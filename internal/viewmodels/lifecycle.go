package viewmodels

import "time"

// LifecycleExecutionView is the prompt-safe UI shape for lifecycle hook output.
// It intentionally excludes raw input/output JSON and prompt text.
type LifecycleExecutionView struct {
	ID             string     `json:"id"`
	When           string     `json:"when"`
	AgentID        string     `json:"agent_id"`
	SkillKey       string     `json:"skill_key"`
	Status         string     `json:"status"`
	OutputContract string     `json:"output_contract"`
	Summary        string     `json:"summary,omitempty"`
	Error          string     `json:"error,omitempty"`
	StartedAt      time.Time  `json:"started_at"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
}

// LifecycleExecutionEventView is the prompt-safe trace shape returned by the
// lifecycle trace API. Payload is already sanitized/truncated before storage.
type LifecycleExecutionEventView struct {
	ID        string         `json:"id"`
	Seq       int            `json:"seq"`
	EventType string         `json:"event_type"`
	Payload   map[string]any `json:"payload"`
	CreatedAt time.Time      `json:"created_at"`
}
