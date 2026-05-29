package lifecycle

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	llmcontracts "github.com/openvibely/openvibely/internal/llm/contracts"
	"github.com/openvibely/openvibely/internal/models"
)

type fakeCaller struct {
	reply         string
	lastPrompt    string
	lastConfig    models.LLMConfig
	lastWorkDir   string
	lastAgentDef  *models.Agent
	lastRuntime   *llmcontracts.RuntimeTools
	calledDirect  bool
	calledWithDef bool
	returnErr     error
}

func (f *fakeCaller) CallAgentDirect(ctx context.Context, message string, _ []models.Attachment, agent models.LLMConfig, workDir string) (string, int, error) {
	f.calledDirect = true
	f.lastPrompt = message
	f.lastConfig = agent
	f.lastWorkDir = workDir
	f.lastRuntime = llmcontracts.RuntimeToolsFromContext(ctx)
	return f.reply, 0, f.returnErr
}

func (f *fakeCaller) CallAgentDirectWithDefinition(ctx context.Context, message string, _ []models.Attachment, agent models.LLMConfig, workDir string, agentDef *models.Agent) (string, int, error) {
	f.calledWithDef = true
	f.lastPrompt = message
	f.lastConfig = agent
	f.lastWorkDir = workDir
	f.lastAgentDef = agentDef
	f.lastRuntime = llmcontracts.RuntimeToolsFromContext(ctx)
	return f.reply, 0, f.returnErr
}

type fakeAgentLookup struct {
	byID map[string]*models.Agent
}

func (f *fakeAgentLookup) GetByID(_ context.Context, id string) (*models.Agent, error) {
	return f.byID[id], nil
}

type fakeLLMConfig struct{ def *models.LLMConfig }

func (f *fakeLLMConfig) GetDefault(_ context.Context) (*models.LLMConfig, error) { return f.def, nil }

func TestLLMHookInvoker_RenderAndCall(t *testing.T) {
	caller := &fakeCaller{reply: `{"content":"hello","sources":["a"]}`}
	agentDef := &models.Agent{Name: "X", Model: "sonnet"}
	agents := &fakeAgentLookup{byID: map[string]*models.Agent{
		"agent-1": agentDef,
	}}
	inv := NewLLMHookInvoker(caller, agents, nil)
	hook := models.AgentLifecycleHook{
		ID:             "h1",
		AgentID:        "agent-1",
		OutputContract: models.OutputContractContextBlock,
		PromptOverride: "Use compact phrasing.",
	}
	in := HookInput{
		TaskID:    "t1",
		TaskRunID: "r1",
		WorkDir:   "/repo",
		SkillBody: "Recall memory then return context_block.",
	}
	raw, err := inv.Invoke(context.Background(), hook, in)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if string(raw) == "" {
		t.Fatal("expected non-empty raw payload")
	}
	if !strings.Contains(caller.lastPrompt, "Recall memory then return context_block.") {
		t.Fatalf("expected skill body in prompt, got %q", caller.lastPrompt)
	}
	if !strings.Contains(caller.lastPrompt, "Use compact phrasing.") {
		t.Fatalf("expected prompt override in prompt, got %q", caller.lastPrompt)
	}
	if !strings.Contains(caller.lastPrompt, "context_block") {
		t.Fatalf("expected output_contract mention in prompt")
	}
	if caller.lastConfig.Model != "sonnet" {
		t.Fatalf("expected agent model override, got %q", caller.lastConfig.Model)
	}
	if caller.lastAgentDef != agentDef {
		t.Fatalf("expected context_block hook to receive owning agent definition")
	}
	if !caller.calledWithDef || caller.calledDirect {
		t.Fatalf("expected hook to use agent definition caller, calledWithDef=%v calledDirect=%v", caller.calledWithDef, caller.calledDirect)
	}
	if caller.lastWorkDir != "/repo" {
		t.Fatalf("expected hook workDir to be passed to caller, got %q", caller.lastWorkDir)
	}
	// Validate the raw payload against the contract for sanity.
	if err := ValidateOutput(hook.OutputContract, raw); err != nil {
		t.Fatalf("raw payload should pass contract validation: %v", err)
	}
}

func TestLLMHookInvoker_HookUsesAgentDefinitionCaller(t *testing.T) {
	caller := &fakeCaller{reply: `{"summary":"reviewed","nothing_to_save":true}`}
	agentDef := &models.Agent{Name: "Skill Curator", Model: "sonnet"}
	inv := NewLLMHookInvoker(caller, &fakeAgentLookup{byID: map[string]*models.Agent{"skill-curator": agentDef}}, nil)
	_, err := inv.Invoke(context.Background(), models.AgentLifecycleHook{AgentID: "skill-curator", OutputContract: models.OutputContractLearningSummary}, HookInput{WorkDir: "/repo"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if caller.lastAgentDef != agentDef {
		t.Fatalf("expected hook to receive agent definition")
	}
	if !caller.calledWithDef || caller.calledDirect {
		t.Fatalf("expected hook to use agent definition caller, calledWithDef=%v calledDirect=%v", caller.calledWithDef, caller.calledDirect)
	}
	if caller.lastWorkDir != "/repo" {
		t.Fatalf("expected hook workdir, got %q", caller.lastWorkDir)
	}
}

func TestLLMHookInvoker_FiltersRuntimeToolsByAgentDefinition(t *testing.T) {
	caller := &fakeCaller{reply: `{"summary":"reviewed","nothing_to_save":true}`}
	agentDef := &models.Agent{Name: "Skill Curator", Model: "sonnet", Tools: []string{"skill_view", "skill_manage"}}
	inv := NewLLMHookInvoker(caller, &fakeAgentLookup{byID: map[string]*models.Agent{"skill-curator": agentDef}}, nil)
	baseTools := &llmcontracts.RuntimeTools{
		Definitions: []llmcontracts.RuntimeToolDefinition{{Name: "skill_view"}, {Name: "skills_list"}, {Name: "skill_manage"}},
	}
	ctx := llmcontracts.WithRuntimeTools(context.Background(), baseTools)
	_, err := inv.Invoke(ctx, models.AgentLifecycleHook{AgentID: "skill-curator", OutputContract: models.OutputContractLearningSummary}, HookInput{})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if caller.lastRuntime == nil {
		t.Fatal("expected runtime tools")
	}
	for _, allowed := range []string{"skill_view", "skill_manage"} {
		if !caller.lastRuntime.HasDefinition(allowed) {
			t.Fatalf("expected %s to be available", allowed)
		}
	}
	for _, denied := range []string{"skills_list"} {
		if caller.lastRuntime.HasDefinition(denied) {
			t.Fatalf("did not expect %s to be available", denied)
		}
	}
}

func TestLLMHookInvoker_ScopedFilesGrantAllowsConcreteFileRuntimeTools(t *testing.T) {
	caller := &fakeCaller{reply: `{"summary":"updated scoped files","changed_paths":["state.md"]}`}
	agentDef := &models.Agent{Name: "Custom After-Complete Agent", Model: "sonnet", Tools: []string{models.AgentToolScopedFiles}}
	inv := NewLLMHookInvoker(caller, &fakeAgentLookup{byID: map[string]*models.Agent{"custom-hook-agent": agentDef}}, nil)
	baseTools := &llmcontracts.RuntimeTools{
		Definitions: []llmcontracts.RuntimeToolDefinition{{Name: "read_file"}, {Name: "write_file"}, {Name: "delete_file"}, {Name: "skill_manage"}},
		Executor: func(ctx context.Context, name string, input json.RawMessage) (string, bool, bool, error) {
			return `{"ok":true}`, true, false, nil
		},
	}
	ctx := llmcontracts.WithRuntimeTools(context.Background(), baseTools)
	_, err := inv.Invoke(ctx, models.AgentLifecycleHook{AgentID: "custom-hook-agent", OutputContract: models.OutputContractActivitySummary}, HookInput{})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	for _, allowed := range []string{"read_file", "write_file", "delete_file"} {
		if caller.lastRuntime == nil || !caller.lastRuntime.HasDefinition(allowed) {
			t.Fatalf("expected %s from ScopedFiles grant, got %#v", allowed, caller.lastRuntime)
		}
		if _, handled, isErr, err := caller.lastRuntime.Executor(ctx, allowed, json.RawMessage(`{}`)); !handled || isErr || err != nil {
			t.Fatalf("expected %s execution to pass filter handled=%v isErr=%v err=%v", allowed, handled, isErr, err)
		}
	}
	if caller.lastRuntime.HasDefinition("skill_manage") {
		t.Fatalf("ScopedFiles grant must not expose unrelated runtime tools: %#v", caller.lastRuntime.Definitions)
	}
}

func TestLLMHookInvoker_DoesNotExposeRuntimeToolsWithoutAgentGrants(t *testing.T) {
	caller := &fakeCaller{reply: `{"summary":"reviewed","nothing_to_save":true}`}
	agentDef := &models.Agent{Name: "Skill Curator", Model: "sonnet"}
	inv := NewLLMHookInvoker(caller, &fakeAgentLookup{byID: map[string]*models.Agent{"skill-curator": agentDef}}, nil)
	baseTools := &llmcontracts.RuntimeTools{
		Definitions: []llmcontracts.RuntimeToolDefinition{{Name: "skill_view"}, {Name: "skill_manage"}},
	}
	ctx := llmcontracts.WithRuntimeTools(context.Background(), baseTools)
	_, err := inv.Invoke(ctx, models.AgentLifecycleHook{AgentID: "skill-curator", OutputContract: models.OutputContractLearningSummary}, HookInput{})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if caller.lastRuntime == nil {
		t.Fatal("expected filtered runtime tools object")
	}
	if len(caller.lastRuntime.Definitions) != 0 {
		t.Fatalf("expected no runtime tools without agent grants, got %#v", caller.lastRuntime.Definitions)
	}
}

func TestLLMHookInvoker_RecordsRuntimeToolTrace(t *testing.T) {
	caller := &fakeCaller{reply: `{"summary":"reviewed","nothing_to_save":true}`}
	agentDef := &models.Agent{Name: "Skill Curator", Model: "sonnet", Tools: []string{"skill_view"}}
	inv := NewLLMHookInvoker(caller, &fakeAgentLookup{byID: map[string]*models.Agent{"skill-curator": agentDef}}, nil)
	baseTools := &llmcontracts.RuntimeTools{
		Definitions: []llmcontracts.RuntimeToolDefinition{{Name: "skill_view"}},
		Executor: func(ctx context.Context, name string, input json.RawMessage) (string, bool, bool, error) {
			return `{"ok":true}`, true, false, nil
		},
	}
	store := &memStore{}
	recorder := NewTraceRecorder(store, "exec-1", nil)
	ctx := context.Background()
	ctx = WithTraceRecorder(ctx, recorder)
	ctx = llmcontracts.WithRuntimeToolTraceRecorder(ctx, recorder)
	ctx = llmcontracts.WithRuntimeTools(ctx, baseTools)
	_, err := inv.Invoke(ctx, models.AgentLifecycleHook{AgentID: "skill-curator", OutputContract: models.OutputContractLearningSummary}, HookInput{})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if caller.lastRuntime == nil || caller.lastRuntime.Executor == nil {
		t.Fatal("expected traced runtime executor")
	}
	if _, handled, _, err := caller.lastRuntime.Executor(ctx, "skill_view", json.RawMessage(`{"handle":"agent/skill"}`)); err != nil || !handled {
		t.Fatalf("execute traced tool handled=%v err=%v", handled, err)
	}
	var eventTypes []string
	for _, event := range store.events {
		eventTypes = append(eventTypes, event.EventType)
	}
	for _, want := range []string{"llm_prompt", "available_tools", "llm_raw_reply", "llm_extracted_json", "tool_call", "tool_result"} {
		if !containsString(eventTypes, want) {
			t.Fatalf("expected trace event %q in %v", want, eventTypes)
		}
	}
}

func TestLLMHookInvoker_RendersConversationTranscript(t *testing.T) {
	caller := &fakeCaller{reply: `{"summary":"reviewed","nothing_to_save":true}`}
	inv := NewLLMHookInvoker(caller, nil, nil)
	_, err := inv.Invoke(context.Background(), models.AgentLifecycleHook{OutputContract: models.OutputContractLearningSummary}, HookInput{
		TaskID: "task-1",
		Extras: map[string]any{
			ConversationTranscriptKey: llmcontracts.ChatContext{
				Messages: []llmcontracts.ChatContextMessage{
					{Role: "user", Content: "please fix the templ workflow"},
					{Role: "assistant", Content: "ran make templ and go test"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	for _, want := range []string{"Hook input", "conversation_transcript", "task-1", "please fix the templ workflow", "ran make templ and go test"} {
		if !strings.Contains(caller.lastPrompt, want) {
			t.Fatalf("expected rendered prompt to contain %q, got %q", want, caller.lastPrompt)
		}
	}
}

func TestLLMHookInvoker_LearningSummaryPromptIncludesExactJSONShape(t *testing.T) {
	caller := &fakeCaller{reply: `{"summary":"reviewed","nothing_to_save":true}`}
	inv := NewLLMHookInvoker(caller, nil, nil)
	_, err := inv.Invoke(context.Background(), models.AgentLifecycleHook{OutputContract: models.OutputContractLearningSummary}, HookInput{})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	for _, want := range []string{"nothing_to_save", "created_skills", "agent_skill_manage", "Agent definition fields", `{"summary":"No durable learning to save.","nothing_to_save":true}`} {
		if !strings.Contains(caller.lastPrompt, want) {
			t.Fatalf("expected rendered prompt to contain %q, got %q", want, caller.lastPrompt)
		}
	}
}

func TestLLMHookInvoker_ObserveSkillPromptTreatsMissingCoverageAsCreateSignal(t *testing.T) {
	caller := &fakeCaller{reply: `{"summary":"reviewed","nothing_to_save":true}`}
	inv := NewLLMHookInvoker(caller, nil, nil)
	_, err := inv.Invoke(context.Background(), models.AgentLifecycleHook{OutputContract: models.OutputContractLearningSummary}, HookInput{
		SkillBody: strings.Join([]string{
			"# Observe Task For Learning",
			"Missing coverage is not a no-op reason.",
			"create or patch a selectable primary coding/repository agent",
		}, "\n"),
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	for _, want := range []string{"Missing coverage is not a no-op reason", "selectable primary coding/repository agent"} {
		if !strings.Contains(caller.lastPrompt, want) {
			t.Fatalf("expected rendered observe prompt to contain %q, got %q", want, caller.lastPrompt)
		}
	}
}

func TestLLMHookInvoker_ExtractsInlineJSONAfterProse(t *testing.T) {
	caller := &fakeCaller{reply: "I found no durable learning.\n{\"summary\":\"No durable learning to save.\",\"nothing_to_save\":true}\nThanks."}
	inv := NewLLMHookInvoker(caller, nil, nil)
	raw, err := inv.Invoke(context.Background(), models.AgentLifecycleHook{OutputContract: models.OutputContractLearningSummary}, HookInput{})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if err := ValidateOutput(models.OutputContractLearningSummary, raw); err != nil {
		t.Fatalf("expected extracted JSON to validate, got %q: %v", string(raw), err)
	}
}

func TestLLMHookInvoker_FallsBackToDefault(t *testing.T) {
	caller := &fakeCaller{reply: `{"selected_mode":"x","mode":"x","action":"continue","confidence":0.5}`}
	def := &models.LLMConfig{Name: "default", Model: "haiku"}
	inv := NewLLMHookInvoker(caller, &fakeAgentLookup{byID: map[string]*models.Agent{}}, &fakeLLMConfig{def: def})
	if _, err := inv.Invoke(context.Background(), models.AgentLifecycleHook{AgentID: "missing"}, HookInput{}); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if caller.lastConfig.Model != "haiku" {
		t.Fatalf("expected default config to be used, got %q", caller.lastConfig.Model)
	}
}

func TestLLMHookInvoker_ExtractsFencedJSON(t *testing.T) {
	caller := &fakeCaller{reply: "Here you go:\n```json\n{\"a\":1}\n```\n"}
	inv := NewLLMHookInvoker(caller, nil, nil)
	raw, err := inv.Invoke(context.Background(), models.AgentLifecycleHook{}, HookInput{})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	var v map[string]int
	if err := json.Unmarshal(raw, &v); err != nil || v["a"] != 1 {
		t.Fatalf("expected extracted JSON {a:1}, got %q (err=%v)", string(raw), err)
	}
}

func TestLLMHookInvoker_NilCaller(t *testing.T) {
	var inv *LLMHookInvoker
	if _, err := inv.Invoke(context.Background(), models.AgentLifecycleHook{}, HookInput{}); err == nil {
		t.Fatal("expected error for nil invoker")
	}
	inv2 := &LLMHookInvoker{}
	if _, err := inv2.Invoke(context.Background(), models.AgentLifecycleHook{}, HookInput{}); err == nil {
		t.Fatal("expected error for nil caller")
	}
}
