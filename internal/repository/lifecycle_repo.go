package repository

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	"github.com/openvibely/openvibely/internal/models"
)

// LifecycleRepo persists agent lifecycle hooks and lifecycle execution rows.
type LifecycleRepo struct {
	db *sql.DB

	eventSeqMu sync.Mutex
}

func NewLifecycleRepo(db *sql.DB) *LifecycleRepo {
	return &LifecycleRepo{db: db}
}

const hookCols = `id, agent_id, when_slot, skill_key, prompt_override, output_contract,
                  blocking, enabled, permissions_json, run_policy_json, schedule_json,
                  created_at, updated_at`

func scanHook(row interface{ Scan(...any) error }) (*models.AgentLifecycleHook, error) {
	var h models.AgentLifecycleHook
	var scheduleJSON sql.NullString
	var blocking, enabled int
	var when, contract string
	if err := row.Scan(&h.ID, &h.AgentID, &when, &h.SkillKey, &h.PromptOverride, &contract,
		&blocking, &enabled, &h.PermissionsJSON, &h.RunPolicyJSON, &scheduleJSON,
		&h.CreatedAt, &h.UpdatedAt); err != nil {
		return nil, err
	}
	h.When = models.LifecycleWhen(when)
	h.OutputContract = models.LifecycleOutputContract(contract)
	h.Blocking = blocking != 0
	h.Enabled = enabled != 0
	if scheduleJSON.Valid {
		h.ScheduleJSON = scheduleJSON.String
	}
	return &h, nil
}

// CreateHook persists a new lifecycle hook binding.
func (r *LifecycleRepo) CreateHook(ctx context.Context, h *models.AgentLifecycleHook) error {
	blocking := 0
	if h.Blocking {
		blocking = 1
	}
	enabled := 0
	if h.Enabled {
		enabled = 1
	}
	permissions := h.PermissionsJSON
	if permissions == "" {
		permissions = "{}"
	}
	runPolicy := h.RunPolicyJSON
	if runPolicy == "" {
		runPolicy = "{}"
	}
	var schedule any
	if h.ScheduleJSON != "" {
		schedule = h.ScheduleJSON
	}
	err := r.db.QueryRowContext(ctx, `
        INSERT INTO agent_lifecycle_hooks
            (id, agent_id, when_slot, skill_key, prompt_override, output_contract,
             blocking, enabled, permissions_json, run_policy_json, schedule_json)
        VALUES (lower(hex(randomblob(16))), ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        RETURNING id, created_at, updated_at`,
		h.AgentID, string(h.When), h.SkillKey, h.PromptOverride, string(h.OutputContract),
		blocking, enabled, permissions, runPolicy, schedule,
	).Scan(&h.ID, &h.CreatedAt, &h.UpdatedAt)
	if err != nil {
		return fmt.Errorf("creating lifecycle hook: %w", err)
	}
	return nil
}

// UpdateHook applies edits to an existing lifecycle hook.
func (r *LifecycleRepo) UpdateHook(ctx context.Context, h *models.AgentLifecycleHook) error {
	blocking := 0
	if h.Blocking {
		blocking = 1
	}
	enabled := 0
	if h.Enabled {
		enabled = 1
	}
	permissions := h.PermissionsJSON
	if permissions == "" {
		permissions = "{}"
	}
	runPolicy := h.RunPolicyJSON
	if runPolicy == "" {
		runPolicy = "{}"
	}
	var schedule any
	if h.ScheduleJSON != "" {
		schedule = h.ScheduleJSON
	}
	_, err := r.db.ExecContext(ctx, `
        UPDATE agent_lifecycle_hooks
        SET when_slot = ?, skill_key = ?, prompt_override = ?, output_contract = ?,
            blocking = ?, enabled = ?, permissions_json = ?, run_policy_json = ?,
            schedule_json = ?, updated_at = datetime('now')
        WHERE id = ?`,
		string(h.When), h.SkillKey, h.PromptOverride, string(h.OutputContract),
		blocking, enabled, permissions, runPolicy, schedule, h.ID,
	)
	if err != nil {
		return fmt.Errorf("updating lifecycle hook: %w", err)
	}
	return nil
}

// DeleteHook removes a lifecycle hook configuration.
func (r *LifecycleRepo) DeleteHook(ctx context.Context, id string) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM agent_lifecycle_hooks WHERE id = ?`, id); err != nil {
		return fmt.Errorf("deleting lifecycle hook: %w", err)
	}
	return nil
}

// HooksByAgent returns all hooks configured for one agent, ordered by `when` value.
func (r *LifecycleRepo) HooksByAgent(ctx context.Context, agentID string) ([]models.AgentLifecycleHook, error) {
	rows, err := r.db.QueryContext(ctx, `
        SELECT `+hookCols+`
        FROM agent_lifecycle_hooks
        WHERE agent_id = ?
        ORDER BY when_slot ASC, created_at ASC`, agentID)
	if err != nil {
		return nil, fmt.Errorf("listing hooks: %w", err)
	}
	defer rows.Close()
	var out []models.AgentLifecycleHook
	for rows.Next() {
		h, err := scanHook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *h)
	}
	return out, rows.Err()
}

// HooksForWhen returns enabled hooks across all agents for one `when` value.
// Hooks are returned ordered by agent_id, created_at for deterministic execution.
func (r *LifecycleRepo) HooksForWhen(ctx context.Context, when models.LifecycleWhen) ([]models.AgentLifecycleHook, error) {
	rows, err := r.db.QueryContext(ctx, `
        SELECT `+hookCols+`
        FROM agent_lifecycle_hooks
        WHERE when_slot = ? AND enabled = 1
        ORDER BY agent_id ASC, created_at ASC`, string(when))
	if err != nil {
		return nil, fmt.Errorf("listing hooks for %s: %w", when, err)
	}
	defer rows.Close()
	var out []models.AgentLifecycleHook
	for rows.Next() {
		h, err := scanHook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *h)
	}
	return out, rows.Err()
}

const execCols = `id, task_id, task_run_id, agent_id, when_slot, lifecycle_hook_id,
                  parent_execution_id, skill_key, output_contract, status,
                  input_json, output_json, error, attempt_count, priority,
                  next_retry_at, idempotency_key,
                  started_at, completed_at`

func scanExecution(row interface{ Scan(...any) error }) (*models.LifecycleExecution, error) {
	var e models.LifecycleExecution
	var agentID, hookID, parent sql.NullString
	var taskRunID sql.NullString
	var when, contract, status string
	if err := row.Scan(&e.ID, &e.TaskID, &taskRunID, &agentID, &when, &hookID,
		&parent, &e.SkillKey, &contract, &status,
		&e.InputJSON, &e.OutputJSON, &e.Error, &e.AttemptCount,
		&e.Priority, &e.NextRetryAt,
		&e.IdempotencyKey,
		&e.StartedAt, &e.CompletedAt); err != nil {
		return nil, err
	}
	if taskRunID.Valid {
		e.TaskRunID = taskRunID.String
	}
	if agentID.Valid {
		e.AgentID = agentID.String
	}
	if hookID.Valid {
		v := hookID.String
		e.LifecycleHookID = &v
	}
	if parent.Valid {
		v := parent.String
		e.ParentExecID = &v
	}
	e.When = models.LifecycleWhen(when)
	e.OutputContract = models.LifecycleOutputContract(contract)
	e.Status = models.LifecycleExecutionStatus(status)
	return &e, nil
}

// CreateExecution records the start of a lifecycle hook invocation.
func (r *LifecycleRepo) CreateExecution(ctx context.Context, e *models.LifecycleExecution) error {
	var hookID, parent any
	if e.LifecycleHookID != nil {
		hookID = *e.LifecycleHookID
	}
	if e.ParentExecID != nil {
		parent = *e.ParentExecID
	}
	if e.Status == "" {
		e.Status = models.LifecycleExecPending
	}
	input := e.InputJSON
	if input == "" {
		input = "{}"
	}
	output := e.OutputJSON
	if output == "" {
		output = "{}"
	}
	err := r.db.QueryRowContext(ctx, `
        INSERT INTO lifecycle_executions
            (id, task_id, task_run_id, agent_id, when_slot, lifecycle_hook_id,
             parent_execution_id, skill_key, output_contract, status,
             input_json, output_json, error, attempt_count, priority,
             next_retry_at, idempotency_key)
        VALUES (lower(hex(randomblob(16))), ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        RETURNING id, started_at`,
		e.TaskID, e.TaskRunID, nullIfEmpty(e.AgentID), string(e.When), hookID,
		parent, e.SkillKey, string(e.OutputContract), string(e.Status),
		input, output, e.Error, e.AttemptCount, e.Priority,
		e.NextRetryAt, e.IdempotencyKey,
	).Scan(&e.ID, &e.StartedAt)
	if err != nil {
		return fmt.Errorf("creating lifecycle execution: %w", err)
	}
	return nil
}

// FindExecutionByIdempotencyKey returns an existing execution with the supplied
// idempotency key, or sql.ErrNoRows if none exists. An empty key returns
// sql.ErrNoRows so callers do not collide on unkeyed rows.
func (r *LifecycleRepo) FindExecutionByIdempotencyKey(ctx context.Context, key string) (*models.LifecycleExecution, error) {
	if key == "" {
		return nil, sql.ErrNoRows
	}
	row := r.db.QueryRowContext(ctx, `
        SELECT `+execCols+`
        FROM lifecycle_executions
        WHERE idempotency_key = ?`, key)
	return scanExecution(row)
}

// UpdateExecution applies the terminal status, output, and timing.
func (r *LifecycleRepo) UpdateExecution(ctx context.Context, e *models.LifecycleExecution) error {
	output := e.OutputJSON
	if output == "" {
		output = "{}"
	}
	_, err := r.db.ExecContext(ctx, `
        UPDATE lifecycle_executions
        SET status = ?, output_json = ?, error = ?, attempt_count = ?,
            next_retry_at = ?, completed_at = ?
        WHERE id = ?`,
		string(e.Status), output, e.Error, e.AttemptCount,
		e.NextRetryAt, e.CompletedAt, e.ID,
	)
	if err != nil {
		return fmt.Errorf("updating lifecycle execution: %w", err)
	}
	return nil
}

// ListExecutionsForTask returns lifecycle executions attached to a task,
// ordered by start time so the UI can render them as task activity.
func (r *LifecycleRepo) ListExecutionsForTask(ctx context.Context, taskID string) ([]models.LifecycleExecution, error) {
	rows, err := r.db.QueryContext(ctx, `
        SELECT `+execCols+`
        FROM lifecycle_executions
        WHERE task_id = ?
        ORDER BY started_at ASC`, taskID)
	if err != nil {
		return nil, fmt.Errorf("listing executions for task %s: %w", taskID, err)
	}
	defer rows.Close()
	var out []models.LifecycleExecution
	for rows.Next() {
		e, err := scanExecution(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

// AppendExecutionEvent persists one trace event for a lifecycle execution.
func (r *LifecycleRepo) AppendExecutionEvent(ctx context.Context, event *models.LifecycleExecutionEvent) error {
	if event == nil {
		return fmt.Errorf("appending lifecycle execution event: nil event")
	}
	if event.LifecycleExecutionID == "" {
		return fmt.Errorf("appending lifecycle execution event: missing lifecycle execution id")
	}
	if event.EventType == "" {
		event.EventType = "event"
	}
	payload := event.PayloadJSON
	if payload == "" {
		payload = "{}"
	}

	// SQLite has no portable single-statement sequence allocator that is pleasant
	// across our test/prod driver, so serialize sequence assignment per repo.
	r.eventSeqMu.Lock()
	defer r.eventSeqMu.Unlock()

	err := r.db.QueryRowContext(ctx, `
		INSERT INTO lifecycle_execution_events
		    (lifecycle_execution_id, seq, event_type, payload_json)
		VALUES (?, COALESCE((SELECT MAX(seq) + 1 FROM lifecycle_execution_events WHERE lifecycle_execution_id = ?), 1), ?, ?)
		RETURNING id, seq, created_at`,
		event.LifecycleExecutionID, event.LifecycleExecutionID, event.EventType, payload,
	).Scan(&event.ID, &event.Seq, &event.CreatedAt)
	if err != nil {
		return fmt.Errorf("appending lifecycle execution event: %w", err)
	}
	event.PayloadJSON = payload
	return nil
}

// ListExecutionEvents returns trace events for a lifecycle execution in emitted order.
func (r *LifecycleRepo) ListExecutionEvents(ctx context.Context, lifecycleExecutionID string) ([]models.LifecycleExecutionEvent, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, lifecycle_execution_id, seq, event_type, payload_json, created_at
		FROM lifecycle_execution_events
		WHERE lifecycle_execution_id = ?
		ORDER BY seq ASC`, lifecycleExecutionID)
	if err != nil {
		return nil, fmt.Errorf("listing lifecycle execution events: %w", err)
	}
	defer rows.Close()
	var out []models.LifecycleExecutionEvent
	for rows.Next() {
		var e models.LifecycleExecutionEvent
		if err := rows.Scan(&e.ID, &e.LifecycleExecutionID, &e.Seq, &e.EventType, &e.PayloadJSON, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
