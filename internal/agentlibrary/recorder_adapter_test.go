package agentlibrary

import (
	"context"
	"errors"
	"testing"

	"github.com/openvibely/openvibely/internal/models"
)

type fakeMutationRepo struct {
	rows []models.AgentConfigMutation
}

func (f *fakeMutationRepo) Create(_ context.Context, m *models.AgentConfigMutation) error {
	m.ID = "row-mock"
	f.rows = append(f.rows, *m)
	return nil
}

func TestRepoRecorder_AppliedStatus(t *testing.T) {
	repo := &fakeMutationRepo{}
	rec := NewRepoRecorder(repo, MutationActor{LifecycleExecutionID: "exec-1", TaskID: "task-1", ProjectID: "proj-1"})
	res := &ImportResult{Applied: true, ChangedPaths: []string{"/a/b/SKILL.md"}, ImportedConfigChange: []string{"agent:backend"}}
	if err := rec.Record(context.Background(), "create", "skill", "backend/verify", []byte("{}"), res, nil); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if len(repo.rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(repo.rows))
	}
	row := repo.rows[0]
	if row.ValidationStatus != models.MutationStatusApplied {
		t.Fatalf("status = %v", row.ValidationStatus)
	}
	if row.LifecycleExecutionID != "exec-1" || row.TaskID != "task-1" || row.ProjectID != "proj-1" {
		t.Fatalf("actor fields not copied: %+v", row)
	}
	if row.TargetType != models.MutationTargetSkill || row.TargetKey != "backend/verify" {
		t.Fatalf("target fields: %+v", row)
	}
	if row.ChangedPathsJSON == "" || row.ImportedChangesJSON == "" {
		t.Fatalf("path/import JSON empty: %+v", row)
	}
}

func TestRepoRecorder_BlockedStatus(t *testing.T) {
	repo := &fakeMutationRepo{}
	rec := NewRepoRecorder(repo, MutationActor{})
	res := &ImportResult{Applied: false, Blocked: []string{"backend/verify"}}
	cause := errors.New("protected: bundled")
	if err := rec.Record(context.Background(), "patch", "skill", "backend/verify", []byte(`{"x":1}`), res, cause); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if repo.rows[0].ValidationStatus != models.MutationStatusBlocked {
		t.Fatalf("expected blocked, got %v", repo.rows[0].ValidationStatus)
	}
	if repo.rows[0].ValidationErrorsJSON == "" {
		t.Fatalf("validation_errors_json must capture cause")
	}
}

func TestRepoRecorder_NoOpStatus(t *testing.T) {
	repo := &fakeMutationRepo{}
	rec := NewRepoRecorder(repo, MutationActor{})
	res := &ImportResult{Applied: false}
	if err := rec.Record(context.Background(), "archive", "agent", "backend", nil, res, nil); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if repo.rows[0].ValidationStatus != models.MutationStatusNoOp {
		t.Fatalf("expected no_op, got %v", repo.rows[0].ValidationStatus)
	}
}

func TestRepoRecorder_AppliedWithCause(t *testing.T) {
	// Edge case: backend succeeded enough to write file but applier returned
	// an error afterward. We mark applied=true so the audit reflects the
	// filesystem state, and capture the error for traceability.
	repo := &fakeMutationRepo{}
	rec := NewRepoRecorder(repo, MutationActor{})
	res := &ImportResult{Applied: true}
	cause := errors.New("applier post-error")
	if err := rec.Record(context.Background(), "create", "skill", "x/y", []byte("{}"), res, cause); err != nil {
		t.Fatalf("Record: %v", err)
	}
	row := repo.rows[0]
	if row.ValidationStatus != models.MutationStatusApplied {
		t.Fatalf("status should be applied because file was written, got %v", row.ValidationStatus)
	}
	if row.ValidationErrorsJSON == "" {
		t.Fatalf("cause must still be captured for debugging")
	}
}

func TestRepoRecorder_NilRepo(t *testing.T) {
	rec := NewRepoRecorder(nil, MutationActor{})
	if rec != nil {
		t.Fatalf("nil repo should give nil recorder")
	}
}

func TestRepoRecorder_NilSafe(t *testing.T) {
	var rec *RepoRecorder
	if err := rec.Record(context.Background(), "x", "skill", "k", nil, nil, nil); err != nil {
		t.Fatalf("nil receiver should be a no-op, got %v", err)
	}
}
