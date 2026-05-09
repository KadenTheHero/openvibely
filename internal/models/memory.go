package models

import "time"

// MemorySettings stores per-project auto-memory configuration. Memory data
// itself lives outside the DB in the selected project repo under
// .openvibely/memory (see internal/memory).
type MemorySettings struct {
	ProjectID string    `json:"project_id"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// MemoryExtractionRun records a single attempt to extract memory from a
// completed task/chat/thread interaction.
type MemoryExtractionRun struct {
	ID           string     `json:"id"`
	ProjectID    string     `json:"project_id"`
	SourceKind   string     `json:"source_kind"`
	SourceID     string     `json:"source_id"`
	Status       string     `json:"status"` // running | ok | nothing | error
	Reason       string     `json:"reason"`
	ErrorMessage string     `json:"error_message"`
	TouchedPaths []string   `json:"touched_paths"`
	StartedAt    time.Time  `json:"started_at"`
	CompletedAt  *time.Time `json:"completed_at"`
}

// MemoryConsolidationRun records a single consolidation pass.
type MemoryConsolidationRun struct {
	ID           string     `json:"id"`
	ProjectID    string     `json:"project_id"`
	Status       string     `json:"status"` // running | ok | error
	ErrorMessage string     `json:"error_message"`
	TouchedPaths []string   `json:"touched_paths"`
	Notes        []string   `json:"notes"`
	StartedAt    time.Time  `json:"started_at"`
	CompletedAt  *time.Time `json:"completed_at"`
}
