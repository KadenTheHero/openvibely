package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/openvibely/openvibely/internal/agentplugins"
	"github.com/openvibely/openvibely/internal/applog"
	llmanthropic "github.com/openvibely/openvibely/internal/llm/anthropic"
	llmcontracts "github.com/openvibely/openvibely/internal/llm/contracts"
	llmollama "github.com/openvibely/openvibely/internal/llm/ollama"
	llmopenai "github.com/openvibely/openvibely/internal/llm/openai"
	llmopenai_compatible "github.com/openvibely/openvibely/internal/llm/openai_compatible"
	llmretry "github.com/openvibely/openvibely/internal/llm/retry"
	llmusage "github.com/openvibely/openvibely/internal/llm/usage"
	"github.com/openvibely/openvibely/internal/models"
)

// ProviderAdapter isolates provider-specific call routing from core orchestration.
// Implementations can choose API key, OAuth, or CLI transports per provider.
type ProviderAdapter interface {
	Call(req llmcontracts.AgentRequest) (llmcontracts.AgentResult, error)
}

var resolvePluginRuntimeBundleFn = agentplugins.ResolveRuntimeBundle

func (s *LLMService) initProviderAdapters() {
	anthropicAdapter := llmanthropic.New(s.llmConfigRepo, s.execRepo)
	openaiAdapter := llmopenai.New(s.llmConfigRepo, s.execRepo)
	openaiCompatibleAdapter := llmopenai_compatible.New(s.execRepo)
	ollamaAdapter := llmollama.New(s.execRepo)
	s.providerAdapters = map[models.LLMProvider]ProviderAdapter{
		models.ProviderAnthropic:        &anthropicProviderAdapter{svc: s, adapter: anthropicAdapter},
		models.ProviderOpenAI:           &openAIProviderAdapter{svc: s, adapter: openaiAdapter},
		models.ProviderOpenAICompatible: &openAICompatibleProviderAdapter{svc: s, adapter: openaiCompatibleAdapter},
		models.ProviderOllama:           &ollamaProviderAdapter{svc: s, adapter: ollamaAdapter},
		models.ProviderTest:             &testProviderAdapter{svc: s},
	}
}

func (s *LLMService) adapterFor(provider models.LLMProvider) (ProviderAdapter, bool) {
	if s.providerAdapters == nil {
		s.initProviderAdapters()
	}
	adapter, ok := s.providerAdapters[provider]
	return adapter, ok
}

func requestUsesChatStreaming(req llmcontracts.AgentRequest) bool {
	if req.Operation != llmcontracts.OperationStreaming {
		return false
	}
	mode := strings.TrimSpace(string(req.ChatMode))
	if !req.Followup {
		return mode == string(models.ChatModeOrchestrate) || mode == string(models.ChatModePlan)
	}
	return len(req.ChatHistory) > 0 || strings.TrimSpace(req.ChatSystemContext) != ""
}

func canonicalResult(output, textOnly string, usage llmcontracts.Usage, err error) (llmcontracts.AgentResult, error) {
	if textOnly == "" {
		textOnly = output
	}
	res := llmcontracts.AgentResult{
		Output:         output,
		TextOnlyOutput: textOnly,
		Usage:          usage,
	}
	// Detect max_tokens errors from any provider adapter. Each provider package
	// has its own errMaxTokens sentinel, so match on the error message prefix.
	if err != nil && strings.HasPrefix(err.Error(), "response truncated: max") {
		res.StopReason = "max_tokens"
	}
	return res, err
}

func callWithRetry(req llmcontracts.AgentRequest, fn func() (llmcontracts.AgentResult, error)) (llmcontracts.AgentResult, error) {
	policy := llmretry.DefaultPolicy()
	ctx := req.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return llmretry.DoWithBeforeRetry(ctx, policy, func() (llmcontracts.AgentResult, error) {
		res, err := fn()
		if err != nil && llmretry.IsRetryable(err) {
			applog.Infof("[agent-svc] provider adapter retryable error operation=%s provider=%s model=%s err=%v", req.Operation, req.Agent.Provider, req.Agent.Model, err)
		}
		return res, err
	}, func(retryCtx context.Context) error {
		if reset := llmcontracts.SteeringRetryResetCallbackFromContext(retryCtx); reset != nil {
			return reset(retryCtx)
		}
		return nil
	})
}

func resolveAgentRuntime(ctx context.Context, ad *models.Agent) (raw *models.Agent, merged *models.Agent, pluginDirs []string) {
	if ad == nil {
		return nil, nil, nil
	}
	raw = ad
	merged = ad
	if len(ad.Plugins) == 0 {
		return raw, merged, nil
	}
	runtime, err := resolvePluginRuntimeBundleFn(ctx, ad.Plugins)
	if err != nil {
		applog.Infof("[agent-svc] resolveAgentRuntime failed for %s: %v", ad.Name, err)
		return raw, merged, nil
	}
	merged = agentplugins.MergeAgentWithRuntime(ad, runtime)
	return raw, merged, runtime.PluginDirs
}

type anthropicProviderAdapter struct {
	svc     *LLMService
	adapter *llmanthropic.Adapter
}

func anthropicAdapterEnabled(agent models.LLMConfig) bool {
	return agent.IsOAuth() || agent.IsAnthropicAPIKey()
}

func (a *anthropicProviderAdapter) Call(req llmcontracts.AgentRequest) (llmcontracts.AgentResult, error) {
	rawAgentDef, runtimeAgentDef, runtimePluginDirs := resolveAgentRuntime(req.Ctx, req.AgentDefinition)
	if runtimeAgentDef != nil {
		req.AgentDefinition = runtimeAgentDef
		req.PluginDirs = runtimePluginDirs
	}
	return callWithRetry(req, func() (llmcontracts.AgentResult, error) {
		switch req.Operation {
		case llmcontracts.OperationDirect:
			if anthropicAdapterEnabled(req.Agent) {
				return a.adapter.Call(req.Ctx, req, req.WorkDir, nil)
			}
			if req.Agent.IsAnthropicCLI() {
				if req.DisableTools {
					return llmcontracts.AgentResult{}, fmt.Errorf("direct no-tools mode is not supported for Anthropic CLI transport")
				}
				output, tokens, err := a.svc.callClaudeCLISimple(req.Ctx, req.Message, req.Attachments, req.Agent, req.WorkDir, req.DisableTools)
				return canonicalResult(output, output, llmusage.FromTotal(tokens), err)
			}
			output, tokens, err := a.svc.callAnthropic(req.Ctx, req.Message, req.Attachments, req.Agent)
			return canonicalResult(output, output, llmusage.FromTotal(tokens), err)

		case llmcontracts.OperationStreaming:
			if anthropicAdapterEnabled(req.Agent) {
				return a.adapter.Call(req.Ctx, req, req.WorkDir, nil)
			}
			if req.Agent.IsAnthropicCLI() {
				if requestUsesChatStreaming(req) {
					output, tokens, err := a.svc.callClaudeCLIChat(req.Ctx, req.Message, req.Attachments, req.Agent, req.ExecID, req.ChatHistory, req.ChatSystemContext, req.WorkDir, req.Followup, req.ChatMode, req.PluginDirs)
					return canonicalResult(output, output, llmusage.FromTotal(tokens), err)
				}
				output, textOnly, tokens, err := a.svc.callClaudeCLI(req.Ctx, req.Message, req.Attachments, req.Agent, req.ExecID, req.WorkDir, req.ProjectInstructions, req.PluginDirs, rawAgentDef)
				return canonicalResult(output, textOnly, llmusage.FromTotal(tokens), err)
			}
			if requestUsesChatStreaming(req) {
				output, tokens, err := a.svc.callAnthropicChat(req.Ctx, req.Message, req.Attachments, req.Agent, req.ExecID, req.ChatHistory, req.ChatSystemContext, req.Followup, req.ChatMode)
				return canonicalResult(output, output, llmusage.FromTotal(tokens), err)
			}
			output, tokens, err := a.svc.callAnthropic(req.Ctx, req.Message, req.Attachments, req.Agent)
			return canonicalResult(output, output, llmusage.FromTotal(tokens), err)

		case llmcontracts.OperationTask:
			if anthropicAdapterEnabled(req.Agent) {
				return a.adapter.Call(req.Ctx, req, req.WorkDir, nil)
			}
			if req.Agent.IsAnthropicCLI() {
				output, textOnly, tokens, err := a.svc.callClaudeCLI(req.Ctx, req.Message, req.Attachments, req.Agent, req.ExecID, req.WorkDir, req.ProjectInstructions, req.PluginDirs, rawAgentDef)
				return canonicalResult(output, textOnly, llmusage.FromTotal(tokens), err)
			}
			output, tokens, err := a.svc.callAnthropic(req.Ctx, req.Message, req.Attachments, req.Agent)
			return canonicalResult(output, output, llmusage.FromTotal(tokens), err)
		default:
			return llmcontracts.AgentResult{}, fmt.Errorf("unsupported operation: %s", req.Operation)
		}
	})
}

type openAIProviderAdapter struct {
	svc     *LLMService
	adapter *llmopenai.Adapter
}

func shouldFallbackOpenAI(agent models.LLMConfig, err error) bool {
	// Fallback is only meaningful for OAuth-backed OpenAI configs where the
	// /v1/responses scope/endpoint can be unavailable.
	if !agent.IsOpenAIOAuth() {
		return false
	}
	return llmretry.ShouldFallbackOpenAI(err)
}

func (a *openAIProviderAdapter) Call(req llmcontracts.AgentRequest) (llmcontracts.AgentResult, error) {
	_, runtimeAgentDef, _ := resolveAgentRuntime(req.Ctx, req.AgentDefinition)
	if runtimeAgentDef != nil {
		req.AgentDefinition = runtimeAgentDef
	}
	// Apply agent definition: inject system prompt + skill content
	if req.AgentDefinition != nil {
		req.ChatSystemContext = ApplyAgentToSystemPrompt(req.ChatSystemContext, req.AgentDefinition)
		req.ProjectInstructions = ApplyAgentToSystemPrompt(req.ProjectInstructions, req.AgentDefinition)
		if req.AgentDefinition.Model != "" && req.AgentDefinition.Model != "inherit" {
			req.Agent.Model = req.AgentDefinition.Model
		}
	}
	return callWithRetry(req, func() (llmcontracts.AgentResult, error) {
		switch req.Operation {
		case llmcontracts.OperationDirect:
			if openAIDirectClientEnabled(req.Agent) {
				output, usage, err := a.adapter.CallDirect(req.Ctx, req.Message, req.Attachments, req.Agent, req.WorkDir, req.DisableTools)
				if shouldFallbackOpenAI(req.Agent, err) {
					if req.DisableTools {
						return canonicalResult(output, output, usage, err)
					}
					applog.Infof("[agent-svc] openai direct fallback to codex-cli operation=%s model=%s err=%v", req.Operation, req.Agent.Model, err)
					output, tokens, ferr := a.svc.callCodexCLISimple(req.Ctx, req.Message, req.Attachments, req.Agent, req.WorkDir, req.DisableTools)
					return canonicalResult(output, output, llmusage.FromTotal(tokens), ferr)
				}
				return canonicalResult(output, output, usage, err)
			}
			if req.DisableTools {
				return llmcontracts.AgentResult{}, fmt.Errorf("direct no-tools mode is not supported for OpenAI CLI transport")
			}
			output, tokens, err := a.svc.callCodexCLISimple(req.Ctx, req.Message, req.Attachments, req.Agent, req.WorkDir, req.DisableTools)
			return canonicalResult(output, output, llmusage.FromTotal(tokens), err)

		case llmcontracts.OperationStreaming:
			if openAIDirectClientEnabled(req.Agent) {
				if requestUsesChatStreaming(req) {
					output, usage, err := a.adapter.CallChatStreaming(req.Ctx, req.Message, req.Attachments, req.Agent, req.ExecID, req.ChatHistory, req.ChatSystemContext, req.Followup, req.ChatMode, req.WorkDir, req.AgentDefinition)
					if shouldFallbackOpenAI(req.Agent, err) {
						applog.Infof("[agent-svc] openai chat fallback to completions operation=%s model=%s err=%v", req.Operation, req.Agent.Model, err)
						output, usage, err = a.adapter.CallCompletionsChatStreaming(req.Ctx, req.Message, req.Attachments, req.Agent, req.ExecID, req.ChatHistory, req.ChatSystemContext, req.Followup, req.ChatMode, req.WorkDir, req.AgentDefinition)
						if err != nil {
							applog.Infof("[agent-svc] openai completions fallback to codex-cli operation=%s model=%s err=%v", req.Operation, req.Agent.Model, err)
							output, tokens, ferr := a.svc.callCodexCLIChat(req.Ctx, req.Message, req.Attachments, req.Agent, req.ExecID, req.ChatHistory, req.ChatSystemContext, req.WorkDir, req.Followup, req.ChatMode)
							return canonicalResult(output, output, llmusage.FromTotal(tokens), ferr)
						}
					}
					return canonicalResult(output, output, usage, err)
				}
				output, textOnly, usage, err := a.adapter.CallStreaming(req.Ctx, req.Message, req.Attachments, req.Agent, req.ExecID, req.WorkDir, req.ProjectInstructions, req.AgentDefinition)
				if shouldFallbackOpenAI(req.Agent, err) {
					applog.Infof("[agent-svc] openai streaming fallback to completions operation=%s model=%s err=%v", req.Operation, req.Agent.Model, err)
					output, textOnly, usage, err = a.adapter.CallCompletionsStreaming(req.Ctx, req.Message, req.Attachments, req.Agent, req.ExecID, req.WorkDir, req.ProjectInstructions, req.AgentDefinition)
					if err != nil {
						applog.Infof("[agent-svc] openai completions fallback to codex-cli operation=%s model=%s err=%v", req.Operation, req.Agent.Model, err)
						output, textOnly, tokens, ferr := a.svc.callCodexCLI(req.Ctx, req.Message, req.Attachments, req.Agent, req.ExecID, req.WorkDir)
						return canonicalResult(output, textOnly, llmusage.FromTotal(tokens), ferr)
					}
				}
				return canonicalResult(output, textOnly, usage, err)
			}
			if requestUsesChatStreaming(req) {
				output, tokens, err := a.svc.callCodexCLIChat(req.Ctx, req.Message, req.Attachments, req.Agent, req.ExecID, req.ChatHistory, req.ChatSystemContext, req.WorkDir, req.Followup, req.ChatMode)
				return canonicalResult(output, output, llmusage.FromTotal(tokens), err)
			}
			output, textOnly, tokens, err := a.svc.callCodexCLI(req.Ctx, req.Message, req.Attachments, req.Agent, req.ExecID, req.WorkDir)
			return canonicalResult(output, textOnly, llmusage.FromTotal(tokens), err)

		case llmcontracts.OperationTask:
			if openAIDirectClientEnabled(req.Agent) {
				output, textOnly, usage, err := a.adapter.CallStreaming(req.Ctx, req.Message, req.Attachments, req.Agent, req.ExecID, req.WorkDir, req.ProjectInstructions, req.AgentDefinition)
				if shouldFallbackOpenAI(req.Agent, err) {
					applog.Infof("[agent-svc] openai task fallback to completions operation=%s model=%s err=%v", req.Operation, req.Agent.Model, err)
					output, textOnly, usage, err = a.adapter.CallCompletionsStreaming(req.Ctx, req.Message, req.Attachments, req.Agent, req.ExecID, req.WorkDir, req.ProjectInstructions, req.AgentDefinition)
					if err != nil {
						applog.Infof("[agent-svc] openai completions fallback to codex-cli operation=%s model=%s err=%v", req.Operation, req.Agent.Model, err)
						output, textOnly, tokens, ferr := a.svc.callCodexCLI(req.Ctx, req.Message, req.Attachments, req.Agent, req.ExecID, req.WorkDir)
						return canonicalResult(output, textOnly, llmusage.FromTotal(tokens), ferr)
					}
				}
				return canonicalResult(output, textOnly, usage, err)
			}
			output, textOnly, tokens, err := a.svc.callCodexCLI(req.Ctx, req.Message, req.Attachments, req.Agent, req.ExecID, req.WorkDir)
			return canonicalResult(output, textOnly, llmusage.FromTotal(tokens), err)
		default:
			return llmcontracts.AgentResult{}, fmt.Errorf("unsupported operation: %s", req.Operation)
		}
	})
}

type openAICompatibleProviderAdapter struct {
	svc     *LLMService
	adapter *llmopenai_compatible.Adapter
}

func (a *openAICompatibleProviderAdapter) Call(req llmcontracts.AgentRequest) (llmcontracts.AgentResult, error) {
	_, runtimeAgentDef, _ := resolveAgentRuntime(req.Ctx, req.AgentDefinition)
	if runtimeAgentDef != nil {
		req.AgentDefinition = runtimeAgentDef
	}
	if req.AgentDefinition != nil {
		req.ChatSystemContext = ApplyAgentToSystemPrompt(req.ChatSystemContext, req.AgentDefinition)
		req.ProjectInstructions = ApplyAgentToSystemPrompt(req.ProjectInstructions, req.AgentDefinition)
		if req.AgentDefinition.Model != "" && req.AgentDefinition.Model != "inherit" {
			req.Agent.Model = req.AgentDefinition.Model
		}
	}
	return callWithRetry(req, func() (llmcontracts.AgentResult, error) {
		return a.adapter.Call(req.Ctx, req, req.WorkDir)
	})
}

type ollamaProviderAdapter struct {
	svc     *LLMService
	adapter *llmollama.Adapter
}

func (a *ollamaProviderAdapter) Call(req llmcontracts.AgentRequest) (llmcontracts.AgentResult, error) {
	_, runtimeAgentDef, _ := resolveAgentRuntime(req.Ctx, req.AgentDefinition)
	if runtimeAgentDef != nil {
		req.AgentDefinition = runtimeAgentDef
	}
	// Apply agent definition: inject system prompt + skill content
	if req.AgentDefinition != nil {
		req.ChatSystemContext = ApplyAgentToSystemPrompt(req.ChatSystemContext, req.AgentDefinition)
		req.ProjectInstructions = ApplyAgentToSystemPrompt(req.ProjectInstructions, req.AgentDefinition)
	}
	return callWithRetry(req, func() (llmcontracts.AgentResult, error) {
		return a.adapter.Call(req.Ctx, req, req.WorkDir, nil)
	})
}

type testProviderAdapter struct {
	svc *LLMService
}

type agentRequestRecorder interface {
	RecordAgentRequest(req llmcontracts.AgentRequest)
}

func (a *testProviderAdapter) Call(req llmcontracts.AgentRequest) (llmcontracts.AgentResult, error) {
	if a.svc.llmCaller == nil {
		return llmcontracts.AgentResult{}, fmt.Errorf("test provider requires LLMCaller to be set via SetLLMCaller()")
	}
	if recorder, ok := a.svc.llmCaller.(agentRequestRecorder); ok {
		recorder.RecordAgentRequest(req)
	}
	output, textOnly, tokens, err := a.svc.llmCaller.CallModel(req.Ctx, req.Message, req.Attachments, req.Agent, req.ExecID, req.WorkDir)
	return canonicalResult(output, textOnly, llmusage.FromTotal(tokens), err)
}
