package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/openvibely/openvibely/internal/models"
)

// MemoryRepo persists per-project memory subsystem state: settings, run
// records, and the system-managed consolidation schedule.
type MemoryRepo struct {
	db *sql.DB
}

func NewMemoryRepo(db *sql.DB) *MemoryRepo {
	return &MemoryRepo{db: db}
}

// ---------- settings ----------

// GetSettings returns the settings row for projectID. When no row exists, a
// row with Enabled=true is returned (default-on); callers may upsert later.
func (r *MemoryRepo) GetSettings(ctx context.Context, projectID string) (models.MemorySettings, error) {
	var s models.MemorySettings
	var enabled int
	err := r.db.QueryRowContext(ctx,
		`SELECT project_id, enabled, created_at, updated_at FROM project_memory_settings WHERE project_id = ?`,
		projectID,
	).Scan(&s.ProjectID, &enabled, &s.CreatedAt, &s.UpdatedAt)
	if err == sql.ErrNoRows {
		return models.MemorySettings{ProjectID: projectID, Enabled: true}, nil
	}
	if err != nil {
		return s, fmt.Errorf("get memory settings: %w", err)
	}
	s.Enabled = enabled != 0
	return s, nil
}

// UpsertSettings creates or updates the settings row for a project.
func (r *MemoryRepo) UpsertSettings(ctx context.Context, projectID string, enabled bool) error {
	v := 0
	if enabled {
		v = 1
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO project_memory_settings (project_id, enabled, created_at, updated_at)
		 VALUES (?, ?, datetime('now'), datetime('now'))
		 ON CONFLICT(project_id) DO UPDATE SET enabled = excluded.enabled, updated_at = datetime('now')`,
		projectID, v)
	if err != nil {
		return fmt.Errorf("upsert memory settings: %w", err)
	}
	return nil
}

// ---------- extraction runs ----------

// CreateExtractionRun inserts a new run row in status=running and returns its id.
func (r *MemoryRepo) CreateExtractionRun(ctx context.Context, projectID, sourceKind, sourceID string) (string, error) {
	var id string
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO memory_extraction_runs (project_id, source_kind, source_id, status, started_at)
		 VALUES (?, ?, ?, 'running', datetime('now')) RETURNING id`,
		projectID, sourceKind, sourceID,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("create extraction run: %w", err)
	}
	return id, nil
}

// FinishExtractionRun records the outcome of a run.
func (r *MemoryRepo) FinishExtractionRun(ctx context.Context, id, status, reason, errMsg string, touchedPaths []string) error {
	if touchedPaths == nil {
		touchedPaths = []string{}
	}
	pathsJSON, err := json.Marshal(touchedPaths)
	if err != nil {
		return fmt.Errorf("marshal touched paths: %w", err)
	}
	_, err = r.db.ExecContext(ctx,
		`UPDATE memory_extraction_runs SET status = ?, reason = ?, error_message = ?, touched_paths = ?, completed_at = datetime('now')
		 WHERE id = ?`,
		status, reason, errMsg, string(pathsJSON), id)
	if err != nil {
		return fmt.Errorf("finish extraction run: %w", err)
	}
	return nil
}

// ListRecentExtractionRuns returns the most recent N extraction runs for a project.
func (r *MemoryRepo) ListRecentExtractionRuns(ctx context.Context, projectID string, limit int) ([]models.MemoryExtractionRun, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, project_id, source_kind, source_id, status, reason, error_message, touched_paths, started_at, completed_at
		 FROM memory_extraction_runs WHERE project_id = ? ORDER BY started_at DESC LIMIT ?`,
		projectID, limit)
	if err != nil {
		return nil, fmt.Errorf("list extraction runs: %w", err)
	}
	defer rows.Close()
	var out []models.MemoryExtractionRun
	for rows.Next() {
		var run models.MemoryExtractionRun
		var pathsJSON string
		if err := rows.Scan(&run.ID, &run.ProjectID, &run.SourceKind, &run.SourceID, &run.Status,
			&run.Reason, &run.ErrorMessage, &pathsJSON, &run.StartedAt, &run.CompletedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(pathsJSON), &run.TouchedPaths)
		out = append(out, run)
	}
	return out, rows.Err()
}

// ---------- consolidation runs ----------

func (r *MemoryRepo) CreateConsolidationRun(ctx context.Context, projectID string) (string, error) {
	var id string
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO memory_consolidation_runs (project_id, status, started_at)
		 VALUES (?, 'running', datetime('now')) RETURNING id`,
		projectID).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("create consolidation run: %w", err)
	}
	return id, nil
}

// FinishConsolidationRun records the run outcome.
func (r *MemoryRepo) FinishConsolidationRun(ctx context.Context, id, status, errMsg string, touchedPaths, notes []string) error {
	if touchedPaths == nil {
		touchedPaths = []string{}
	}
	if notes == nil {
		notes = []string{}
	}
	tpJSON, err := json.Marshal(touchedPaths)
	if err != nil {
		return fmt.Errorf("marshal touched paths: %w", err)
	}
	notesJSON, err := json.Marshal(notes)
	if err != nil {
		return fmt.Errorf("marshal notes: %w", err)
	}
	_, err = r.db.ExecContext(ctx,
		`UPDATE memory_consolidation_runs
			 SET status = ?, error_message = ?, touched_paths = ?, notes = ?, completed_at = datetime('now')
			 WHERE id = ?`,
		status, errMsg, string(tpJSON), string(notesJSON), id)
	if err != nil {
		return fmt.Errorf("finish consolidation run: %w", err)
	}
	return nil
}

func (r *MemoryRepo) GetLatestConsolidationRun(ctx context.Context, projectID string) (*models.MemoryConsolidationRun, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, project_id, status, error_message, touched_paths, notes, started_at, completed_at
		 FROM memory_consolidation_runs WHERE project_id = ? ORDER BY started_at DESC LIMIT 1`,
		projectID)
	var run models.MemoryConsolidationRun
	var tpJSON, notesJSON string
	err := row.Scan(&run.ID, &run.ProjectID, &run.Status, &run.ErrorMessage,
		&tpJSON, &notesJSON, &run.StartedAt, &run.CompletedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("latest consolidation run: %w", err)
	}
	_ = json.Unmarshal([]byte(tpJSON), &run.TouchedPaths)
	_ = json.Unmarshal([]byte(notesJSON), &run.Notes)
	return &run, nil
}

func (r *MemoryRepo) EnsureSettings(ctx context.Context, projectID string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO project_memory_settings (project_id) VALUES (?)
		 ON CONFLICT(project_id) DO NOTHING`, projectID)
	if err != nil {
		return fmt.Errorf("ensure memory settings: %w", err)
	}
	return nil
}
