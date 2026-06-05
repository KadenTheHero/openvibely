package lifecycle

import (
	"strings"
	"testing"
)

func TestValidateSelectedMode_AcceptsContinueOrSwitch(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload string
		ok      bool
	}{
		{"continue", `{"mode":"backend-engineer","action":"continue","confidence":0.7}`, true},
		{"switch", `{"mode":"security-reviewer","action":"switch","confidence":0.9}`, true},
		{"empty-action", `{"mode":"backend-engineer","confidence":0.5}`, false},
		{"bad-action", `{"mode":"x","action":"flip","confidence":0.5}`, false},
		{"empty-mode", `{"mode":"","action":"switch","confidence":0.5}`, false},
		{"bad-confidence", `{"mode":"x","action":"switch","confidence":1.5}`, false},
		{"clarification-ok", `{"needs_clarification":true,"clarifying_question":"what kind?"}`, true},
		{"clarification-missing-question", `{"needs_clarification":true}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateSelectedMode([]byte(tc.payload))
			if tc.ok && err != nil {
				t.Fatalf("expected ok, got %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatalf("expected error, got nil")
			}
		})
	}
}

func TestValidateSelectedSkills_AcceptsStandaloneHandlesAndClarification(t *testing.T) {
	got, err := ValidateSelectedSkills([]byte(`{"skills":["debug_go_tests","debug_go_tests","review_auth"],"confidence":0.7}`))
	if err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
	if strings.Join(got.Skills, ",") != "debug_go_tests,review_auth" {
		t.Fatalf("expected deduped handles, got %#v", got.Skills)
	}
	if _, err := ValidateSelectedSkills([]byte(`{"skills":["agent/skill"],"confidence":0.7}`)); err == nil {
		t.Fatalf("expected agent-prefixed skill handle rejection")
	}
	if _, err := ValidateSelectedSkills([]byte(`{"needs_clarification":true,"clarifying_question":"which area?"}`)); err != nil {
		t.Fatalf("expected clarification ok, got %v", err)
	}
	if _, err := ValidateSelectedSkills([]byte(`{"confidence":2}`)); err == nil {
		t.Fatalf("expected bad confidence rejection")
	}
}

func TestValidateSelectedMemories_AcceptsCompactIndexSelections(t *testing.T) {
	got, err := ValidateSelectedMemories([]byte(`{"memories":[{"file":"provider_architecture.md","topic":"Provider","summary":"Use mode-driven routing."},{"file":"provider_architecture.md","summary":"duplicate"},{"topic":"User preference","snippet":"ignored without file handle"}],"content":"Remembered context only from the index.","confidence":0.7,"reason":"matches prompt"}`))
	if err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
	if len(got.Memories) != 1 {
		t.Fatalf("expected deduped memories, got %#v", got.Memories)
	}
	if got.Content != "Remembered context only from the index." {
		t.Fatalf("expected compact route content to be preserved as metadata, got %q", got.Content)
	}
	if _, err := ValidateSelectedMemories([]byte(`{"memories":[{"file":"../secret.md"}],"confidence":0.7}`)); err == nil {
		t.Fatalf("expected unsafe memory path rejection")
	}
	if _, err := ValidateSelectedMemories([]byte(`{"needs_clarification":true,"clarifying_question":"which project?"}`)); err != nil {
		t.Fatalf("expected clarification ok, got %v", err)
	}
	if _, err := ValidateSelectedMemories([]byte(`{"confidence":2}`)); err == nil {
		t.Fatalf("expected bad confidence rejection")
	}
}

func TestValidateContextBlock_RejectsBadConfidence(t *testing.T) {
	if _, err := ValidateContextBlock([]byte(`{"content":"hi","confidence":0.5}`)); err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
	if _, err := ValidateContextBlock([]byte(`{"content":"hi","confidence":1.5}`)); err == nil {
		t.Fatalf("expected error on out-of-range confidence")
	}
}

func TestValidateActivitySummary_SkippedRequiresReason(t *testing.T) {
	if _, err := ValidateActivitySummary([]byte(`{"summary":"x","skipped":true}`)); err == nil {
		t.Fatalf("expected error when skipped without reason")
	}
	if _, err := ValidateActivitySummary([]byte(`{"summary":"x","skipped":true,"skip_reason":"no signal"}`)); err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
}

func TestValidateActivitySummary_RequiresSummaryUnlessSkipped(t *testing.T) {
	if _, err := ValidateActivitySummary([]byte(`{"task_id":"current"}`)); err == nil {
		t.Fatalf("expected error for tool-argument fragment without summary")
	}
	if _, err := ValidateActivitySummary([]byte(`{"summary":"queued continuation","skipped":false}`)); err != nil {
		t.Fatalf("expected ok with summary, got %v", err)
	}
}

func TestValidateLearningSummary_RequiresSummaryUnlessNothingToSave(t *testing.T) {
	// nothing_to_save=true: summary may be omitted.
	if _, err := ValidateLearningSummary([]byte(`{"nothing_to_save":true}`)); err != nil {
		t.Fatalf("expected ok with nothing_to_save, got %v", err)
	}
	// nothing_to_save=false with empty summary is rejected.
	if _, err := ValidateLearningSummary([]byte(`{"summary":""}`)); err == nil {
		t.Fatalf("expected error: summary required when nothing_to_save=false")
	}
	if _, err := ValidateLearningSummary([]byte(`{"summary":"updated skill","updated_skills":["x/y"]}`)); err != nil {
		t.Fatalf("expected ok with summary, got %v", err)
	}
}

func TestValidateLibraryUpdateSummary_RequiresSummary(t *testing.T) {
	if _, err := ValidateLibraryUpdateSummary([]byte(`{}`)); err == nil || !strings.Contains(err.Error(), "summary required") {
		t.Fatalf("expected summary-required error, got %v", err)
	}
}

func TestValidateLibraryUpdateSummary_RequiresAttribution(t *testing.T) {
	bad := `{"summary":"x","archived_skills":["a/s"]}`
	if _, err := ValidateLibraryUpdateSummary([]byte(bad)); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("expected missing-attribution error, got %v", err)
	}

	good := `{
			"summary":"merged narrow skills",
			"archived_skills":["a/s"],
			"skill_consolidations":[{"from":"a/s","into":"a/main","reason":"merged"}]
		}`
	if _, err := ValidateLibraryUpdateSummary([]byte(good)); err != nil {
		t.Fatalf("expected ok, got %v", err)
	}

	dup := `{
		"summary":"x",
		"archived_skills":["s1"],
		"skill_consolidations":[{"from":"s1","into":"s-main"}],
		"skill_prunings":[{"handle":"s1","reason":"x"}]
	}`
	if _, err := ValidateLibraryUpdateSummary([]byte(dup)); err == nil || !strings.Contains(err.Error(), "multiple") {
		t.Fatalf("expected multiple-list error, got %v", err)
	}
}

func TestValidateLearningSummary_RejectsAgentChanges(t *testing.T) {
	if _, err := ValidateLearningSummary([]byte(`{"summary":"created agent","created_agents":["agent-x"]}`)); err == nil || !strings.Contains(err.Error(), "agent changes are not allowed") {
		t.Fatalf("expected agent-change rejection, got %v", err)
	}
}

func TestValidateLibraryUpdateSummary_RejectsAgentChanges(t *testing.T) {
	payload := `{"summary":"changed agents","archived_agents":["agent-x"],"agent_prunings":[{"key":"agent-x","reason":"old"}]}`
	if _, err := ValidateLibraryUpdateSummary([]byte(payload)); err == nil || !strings.Contains(err.Error(), "agent changes are not allowed") {
		t.Fatalf("expected agent-change rejection, got %v", err)
	}
}
