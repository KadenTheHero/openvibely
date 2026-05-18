package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	llmcontracts "github.com/openvibely/openvibely/internal/llm/contracts"
	"github.com/openvibely/openvibely/internal/models"
)

// AgentCaller is the subset of the LLM service the LLM-backed HookInvoker
// uses. Defined as an interface so the invoker can be unit-tested without
// pulling in the full LLM service and so callers can shim provider-specific
// behavior.
type AgentCaller interface {
	// CallAgentDirect dispatches the supplied prompt against the configured LLM
	// provider for the agent and returns the raw text reply. Request-scoped
	// runtime tools may be attached to ctx and are available to learning hooks.
	CallAgentDirect(
		ctx context.Context,
		message string,
		attachments []models.Attachment,
		agent models.LLMConfig,
		workDir string,
	) (string, int, error)
}

type AgentDefinitionCaller interface {
	CallAgentDirectWithDefinition(
		ctx context.Context,
		message string,
		attachments []models.Attachment,
		agent models.LLMConfig,
		workDir string,
		agentDef *models.Agent,
	) (string, int, error)
}

// AgentLookup resolves a persisted agent by ID so the invoker can pick the
// correct system prompt and tool grants for the hook. Optional: when nil the
// invoker emits a default-shaped LLMConfig populated only from the model
// preference on the hook.
type AgentLookup interface {
	GetByID(ctx context.Context, id string) (*models.Agent, error)
}

// LLMConfigLookup resolves the model-routing config used by the agent. The
// runbook (§Auto-Routing) talks about per-agent model defaults; this lookup
// applies them. Optional: a nil lookup falls back to "inherit".
type LLMConfigLookup interface {
	GetDefault(ctx context.Context) (*models.LLMConfig, error)
}

// LLMHookInvoker dispatches a lifecycle hook to the LLM service. The hook
// input is rendered as a prompt block (skill body + prompt override + serialized
// previous outputs); the model is expected to return a JSON payload matching
// the configured output_contract.
//
// Wiring example (in server.go):
//
//	invoker := lifecycle.NewLLMHookInvoker(llmSvc, agentRepo, llmConfigRepo)
//	runner := lifecycle.NewRunner(lifecycleRepo, invoker, skillResolver)
type LLMHookInvoker struct {
	caller AgentCaller
	agents AgentLookup
	models LLMConfigLookup
}

// NewLLMHookInvoker constructs the production invoker. caller is required;
// agents/models are optional but recommended.
func NewLLMHookInvoker(caller AgentCaller, agents AgentLookup, modelCfg LLMConfigLookup) *LLMHookInvoker {
	return &LLMHookInvoker{caller: caller, agents: agents, models: modelCfg}
}

// Invoke renders the prompt and asks the LLM for a structured reply. The
// returned bytes are passed to ValidateOutput by the runner. If the model
// returns a fenced ```json block, we extract its body so the runner does not
// have to handle Markdown formatting.
func (i *LLMHookInvoker) Invoke(ctx context.Context, hook models.AgentLifecycleHook, input HookInput) (json.RawMessage, error) {
	if i == nil || i.caller == nil {
		return nil, errors.New("lifecycle: LLMHookInvoker not configured")
	}
	prompt, err := renderHookPrompt(hook, input)
	if err != nil {
		return nil, err
	}
	cfg, agentDef, err := i.resolveLLMConfig(ctx, hook)
	if err != nil {
		return nil, err
	}
	callCtx := contextWithAgentRuntimeTools(ctx, agentDef)
	RecordTraceEvent(callCtx, "llm_prompt", map[string]any{
		"agent_id":        hook.AgentID,
		"agent_key":       agentKeyForTrace(agentDef),
		"skill_key":       hook.SkillKey,
		"output_contract": string(hook.OutputContract),
		"work_dir":        input.WorkDir,
		"prompt":          prompt,
	})
	RecordTraceEvent(callCtx, "available_tools", map[string]any{"tools": runtimeToolNamesForTrace(callCtx)})
	var reply string
	if defCaller, ok := i.caller.(AgentDefinitionCaller); ok && agentDef != nil {
		reply, _, err = defCaller.CallAgentDirectWithDefinition(callCtx, prompt, nil, cfg, input.WorkDir, agentDef)
	} else {
		reply, _, err = i.caller.CallAgentDirect(callCtx, prompt, nil, cfg, input.WorkDir)
	}
	if err != nil {
		RecordTraceEvent(callCtx, "llm_error", map[string]any{"error": err.Error()})
		return nil, fmt.Errorf("lifecycle: LLM call failed: %w", err)
	}
	RecordTraceEvent(callCtx, "llm_raw_reply", map[string]any{"reply": reply})
	extracted := extractJSONPayload(reply)
	RecordTraceEvent(callCtx, "llm_extracted_json", map[string]any{"json": extracted})
	return json.RawMessage(extracted), nil
}

// resolveLLMConfig picks the model the hook should run under. Preference:
//  1. hook's persisted model override (future column; not implemented yet)
//  2. agent's model field
//  3. the configured default LLMConfig
//  4. an empty config (so the caller's defaults apply)
func (i *LLMHookInvoker) resolveLLMConfig(ctx context.Context, hook models.AgentLifecycleHook) (models.LLMConfig, *models.Agent, error) {
	var agentDef *models.Agent
	if i.agents != nil && hook.AgentID != "" {
		if a, err := i.agents.GetByID(ctx, hook.AgentID); err == nil && a != nil {
			agentDef = a
			cfg := models.LLMConfig{Name: a.Name, Model: a.Model}
			if a.Model != "" && a.Model != "inherit" {
				return cfg, agentDef, nil
			}
		}
	}
	if i.models != nil {
		if def, err := i.models.GetDefault(ctx); err == nil && def != nil {
			return *def, agentDef, nil
		}
	}
	return models.LLMConfig{Model: "inherit"}, agentDef, nil
}

// renderHookPrompt builds the prompt the hook sends to the LLM. The skill
// body anchors the procedure; the prompt override (if any) is appended; the
// hook input snapshot lands in a fenced JSON block so the model can read the
// relevant context without ambiguity. observe_task_for_learning receives only
// the retained chat context JSON when it is available.
func renderHookPrompt(hook models.AgentLifecycleHook, input HookInput) (string, error) {
	var b strings.Builder
	if input.SkillBody != "" {
		b.WriteString(strings.TrimSpace(input.SkillBody))
		b.WriteString("\n\n")
	}
	if hook.PromptOverride != "" {
		b.WriteString(strings.TrimSpace(hook.PromptOverride))
		b.WriteString("\n\n")
	}
	if hook.OutputContract != "" {
		b.WriteString("Return one JSON object that matches the `")
		b.WriteString(string(hook.OutputContract))
		b.WriteString("` output contract. Do not wrap the JSON in prose.\n")
		if spec := outputContractPromptSpec(hook.OutputContract); spec != "" {
			b.WriteString(spec)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	snap := any(sanitizedHookInputForPrompt(input))
	label := "Hook input"
	encoded, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return "", fmt.Errorf("render hook prompt: %w", err)
	}
	b.WriteString(label)
	b.WriteString(":\n```json\n")
	b.Write(encoded)
	b.WriteString("\n```\n")
	return b.String(), nil
}

func sanitizedHookInputForPrompt(input HookInput) HookInput {
	input.SkillBody = ""
	return input
}

func contextWithAgentRuntimeTools(ctx context.Context, agentDef *models.Agent) context.Context {
	base := llmcontracts.RuntimeToolsFromContext(ctx)
	filtered := filterRuntimeToolsForAgent(base, agentDef)
	filtered = llmcontracts.TraceRuntimeTools(filtered, TraceRecorderFromContext(ctx))
	if filtered == base {
		return ctx
	}
	return llmcontracts.WithRuntimeTools(ctx, filtered)
}

func filterRuntimeToolsForAgent(rt *llmcontracts.RuntimeTools, agentDef *models.Agent) *llmcontracts.RuntimeTools {
	if rt == nil || agentDef == nil {
		return rt
	}
	allowed := make(map[string]struct{}, len(agentDef.Tools))
	for _, tool := range agentDef.Tools {
		name := strings.ToLower(strings.TrimSpace(tool))
		if name != "" {
			allowed[name] = struct{}{}
		}
	}
	defs := make([]llmcontracts.RuntimeToolDefinition, 0, len(rt.Definitions))
	for _, def := range rt.Definitions {
		if _, ok := allowed[strings.ToLower(strings.TrimSpace(def.Name))]; ok {
			defs = append(defs, def)
		}
	}
	return &llmcontracts.RuntimeTools{
		Definitions:      defs,
		Executor:         filteredRuntimeToolExecutor(rt.Executor, allowed),
		Filter:           filteredRuntimeToolFilter(rt.Filter, allowed),
		Metadata:         rt.Metadata,
		SkipDefaultTools: rt.SkipDefaultTools,
	}
}

func filteredRuntimeToolExecutor(base llmcontracts.RuntimeToolExecutor, allowed map[string]struct{}) llmcontracts.RuntimeToolExecutor {
	if base == nil {
		return nil
	}
	return func(ctx context.Context, name string, input json.RawMessage) (string, bool, bool, error) {
		if _, ok := allowed[strings.ToLower(strings.TrimSpace(name))]; !ok {
			return "", false, false, nil
		}
		return base(ctx, name, input)
	}
}

func filteredRuntimeToolFilter(base llmcontracts.RuntimeToolFilter, allowed map[string]struct{}) llmcontracts.RuntimeToolFilter {
	return func(name string) (bool, bool) {
		key := strings.ToLower(strings.TrimSpace(name))
		if _, ok := allowed[key]; !ok {
			return false, true
		}
		if base != nil {
			allow, handled := base(name)
			if handled {
				return allow, true
			}
		}
		return true, true
	}
}

func runtimeToolNamesForTrace(ctx context.Context) []string {
	rt := llmcontracts.RuntimeToolsFromContext(ctx)
	if rt == nil {
		return nil
	}
	names := make([]string, 0, len(rt.Definitions))
	for _, def := range rt.Definitions {
		name := strings.TrimSpace(def.Name)
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

func agentKeyForTrace(agentDef *models.Agent) string {
	if agentDef == nil {
		return ""
	}
	return agentDef.Key
}

func outputContractPromptSpec(contract models.LifecycleOutputContract) string {
	switch contract {
	case models.OutputContractLearningSummary:
		return "Required JSON shape: {\"summary\":\"string\",\"nothing_to_save\":boolean,\"created_skills\":[\"skill_or_assigned_agent/skill\"],\"updated_skills\":[\"skill_or_assigned_agent/skill\"],\"archived_skills\":[\"skill_or_assigned_agent/skill\"],\"support_files_written\":[\"path\"],\"blocked_changes\":[\"reason\"],\"evidence_refs\":[{\"task_id\":\"id\",\"task_run_id\":\"id\",\"reason\":\"why\"}]}. Agent definition fields must be omitted or empty; agents are user-managed. Agent-owned skill changes are allowed only through server-scoped agent_skill_manage for the task's assigned agent. If there is nothing durable to save, return exactly: {\"summary\":\"No durable learning to save.\",\"nothing_to_save\":true}"
	case models.OutputContractSelectedMode:
		return "Required JSON shape: {\"mode\":\"agent_key\",\"action\":\"continue|switch\",\"confidence\":0.0,\"reason\":\"string\",\"needs_clarification\":false,\"clarifying_question\":\"string\"}."
	case models.OutputContractSelectedSkills:
		return "Required JSON shape: {\"skills\":[\"skill_key\"],\"confidence\":0.0,\"reason\":\"Why these skills fit the task\",\"needs_clarification\":false,\"clarifying_question\":\"string\"}. Choose only handles listed in available_skills for this turn. Return an empty skills array when no listed skill is relevant."
	case models.OutputContractContextBlock:
		return "Required JSON shape: {\"content\":\"string\",\"sources\":[\"source\"],\"confidence\":0.0}."
	case models.OutputContractActivitySummary:
		return "Required JSON shape: {\"summary\":\"string\",\"changed_paths\":[\"path\"],\"created\":[\"id\"],\"updated\":[\"id\"],\"skipped\":false,\"skip_reason\":\"string\"}."
	case models.OutputContractLibraryUpdateSummary:
		return "Required JSON shape: {\"summary\":\"string\",\"created_skills\":[\"agent/skill\"],\"updated_skills\":[\"agent/skill\"],\"archived_skills\":[\"agent/skill\"],\"skill_consolidations\":[{\"from\":\"agent/skill\",\"into\":\"agent/skill\",\"reason\":\"why\"}],\"skill_prunings\":[{\"handle\":\"agent/skill\",\"reason\":\"why\"}],\"blocked_changes\":[\"reason\"]}. Agent arrays/consolidations/prunings must be omitted or empty; agents are user-managed."
	default:
		return ""
	}
}

// extractJSONPayload pulls the first JSON value out of a model reply. If the
// reply is already a JSON object/array, it is returned verbatim. Otherwise the
// function looks for fenced JSON and then for the first balanced inline JSON
// object/array. Falling all the way through returns the trimmed reply so the
// validator can produce the canonical error.
func extractJSONPayload(reply string) string {
	s := strings.TrimSpace(reply)
	if s == "" {
		return s
	}
	if s[0] == '{' || s[0] == '[' {
		return s
	}
	if idx := strings.Index(s, "```json"); idx >= 0 {
		rest := s[idx+len("```json"):]
		if end := strings.Index(rest, "```"); end >= 0 {
			return strings.TrimSpace(rest[:end])
		}
	}
	if idx := strings.Index(s, "```"); idx >= 0 {
		rest := s[idx+3:]
		if end := strings.Index(rest, "```"); end >= 0 {
			return strings.TrimSpace(rest[:end])
		}
	}
	if payload := firstBalancedJSONValue(s); payload != "" {
		return payload
	}
	return s
}

func firstBalancedJSONValue(s string) string {
	start := -1
	for i, r := range s {
		if r == '{' || r == '[' {
			start = i
			break
		}
	}
	if start < 0 {
		return ""
	}

	stack := make([]rune, 0, 4)
	inString := false
	escaped := false
	for i, r := range s[start:] {
		if inString {
			if escaped {
				escaped = false
				continue
			}
			switch r {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}
		switch r {
		case '"':
			inString = true
		case '{', '[':
			stack = append(stack, r)
		case '}', ']':
			if len(stack) == 0 {
				return ""
			}
			open := stack[len(stack)-1]
			if (open == '{' && r != '}') || (open == '[' && r != ']') {
				return ""
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				return strings.TrimSpace(s[start : start+i+1])
			}
		}
	}
	return ""
}
