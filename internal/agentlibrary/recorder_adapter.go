package agentlibrary

import (
	"context"
	"encoding/json"

	"github.com/openvibely/openvibely/internal/models"
)

// MutationRepo is the minimal contract a backing repository must satisfy for
// the agentlibrary recorder adapter. It exists so this package does not import
// internal/repository (avoiding an import cycle with future callers).
type MutationRepo interface {
	Create(ctx context.Context, m *models.AgentConfigMutation) error
}

// RepoRecorder adapts a MutationRepo + MutationActor into the MutationRecorder
// interface the mutation tools call. Every recorded row carries the actor
// context so the audit log can answer who proposed each change and why it was
// or was not applied (runbook §Data Model line 2452 + §Backend Validation
// line 1773).
type RepoRecorder struct {
	repo  MutationRepo
	actor MutationActor
}

// NewRepoRecorder wires a MutationRepo with the contextual actor fields.
// Callers typically construct one RepoRecorder per lifecycle execution so the
// LifecycleExecutionID is correct for every row inserted during that hook.
func NewRepoRecorder(repo MutationRepo, actor MutationActor) *RepoRecorder {
	if repo == nil {
		return nil
	}
	return &RepoRecorder{repo: repo, actor: actor}
}

// Record persists one audit row. Both applied and blocked proposals are
// stored so debugging can answer why no persisted-state change happened.
//
// Mapping rules:
//   - cause != nil and result.Applied  -> applied with the partial-success error captured in validation_errors_json
//   - cause != nil and !result.Applied -> blocked
//   - cause == nil and result.Applied  -> applied
//   - cause == nil and !result.Applied -> no_op
func (r *RepoRecorder) Record(ctx context.Context, action, target, key string, payload []byte, result *ImportResult, cause error) error {
	if r == nil || r.repo == nil {
		return nil
	}
	row := models.AgentConfigMutation{
		LifecycleExecutionID: r.actor.LifecycleExecutionID,
		TaskID:               r.actor.TaskID,
		TaskRunID:            r.actor.TaskRunID,
		ProjectID:            r.actor.ProjectID,
		ActorAgentID:         r.actor.ActorAgentID,
		TargetType:           models.MutationTargetType(target),
		TargetKey:            key,
		Action:               action,
		ProposedPayloadJSON:  string(payload),
	}
	status := models.MutationStatusNoOp
	if result != nil && result.Applied {
		status = models.MutationStatusApplied
	}
	if cause != nil && (result == nil || !result.Applied) {
		status = models.MutationStatusBlocked
	}
	row.ValidationStatus = status

	if cause != nil {
		errsJSON, _ := json.Marshal([]string{cause.Error()})
		row.ValidationErrorsJSON = string(errsJSON)
	}
	if result != nil {
		if len(result.ChangedPaths) > 0 {
			if b, err := json.Marshal(result.ChangedPaths); err == nil {
				row.ChangedPathsJSON = string(b)
			}
		}
		if len(result.ImportedConfigChange) > 0 {
			if b, err := json.Marshal(result.ImportedConfigChange); err == nil {
				row.ImportedChangesJSON = string(b)
			}
		}
		if len(result.EvidenceRefs) > 0 {
			if b, err := json.Marshal(result.EvidenceRefs); err == nil {
				row.EvidenceRefsJSON = string(b)
			}
		}
	}
	return r.repo.Create(ctx, &row)
}
