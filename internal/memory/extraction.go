package memory

import (
	"errors"
	"strings"
)

// SourceKind identifies the surface that produced an interaction.
type SourceKind string

const (
	SourceTask     SourceKind = "task"
	SourceThread   SourceKind = "thread"
	SourceChat     SourceKind = "chat"
	SourceAPIChat  SourceKind = "api_chat"
	SourceTelegram SourceKind = "telegram"
	SourceSlack    SourceKind = "slack"
	SourceWebhook  SourceKind = "webhook"
)

// Interaction is the normalized input handed to the model-backed memory
// extraction prompt. The memory package does not depend on the live DB layer;
// callers assemble Interaction from execution rows, prompts, and outputs.
type Interaction struct {
	ProjectID    string
	SourceKind   SourceKind
	SourceID     string // execution id, message id, etc.
	UserText     string // user-authored prose only, do not include system prompts
	AssistantOut string // optional assistant output for context
	ChangedFiles []string
	Cancelled    bool
	// Title is an optional short label (e.g. task title) included in the prompt.
	Title string
}

// SaveDecisionReason describes why an interaction was skipped. Empty means
// the interaction is eligible for the model-backed extraction pass.
type SaveDecisionReason string

const (
	SkipDisabled        SaveDecisionReason = "memory disabled"
	SkipCancelled       SaveDecisionReason = "interaction cancelled"
	SkipEmpty           SaveDecisionReason = "no meaningful user prose"
	SkipSecretDominated SaveDecisionReason = "interaction dominated by secrets"
)

// ShouldExtract returns the reason an interaction should be skipped, or empty
// when it is eligible for the model-backed extraction pass.
func ShouldExtract(enabled bool, in Interaction) SaveDecisionReason {
	if !enabled {
		return SkipDisabled
	}
	if in.Cancelled {
		return SkipCancelled
	}
	user := strings.TrimSpace(in.UserText)
	if len(user) < 12 {
		return SkipEmpty
	}
	combined := in.UserText + "\n" + in.AssistantOut
	if LooksDominatedBySecrets(combined) {
		return SkipSecretDominated
	}
	return ""
}

// ErrMemoryDisabled is returned when a memory operation is requested for a
// project that has memory disabled.
var ErrMemoryDisabled = errors.New("memory disabled for project")
