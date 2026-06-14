---
name: provider_architecture
type: project
created: 2026-05-09
updated: 2026-06-14
source: consolidation
source_id: memory_consolidation_2026_06_14
confidence: high
title: Provider Architecture
---

Provider logic is isolated in adapter packages under `internal/llm`: OpenAI, Anthropic, Ollama, and OpenAI-compatible. Provider routing goes through `internal/service/provider_adapter.go` and shared contracts/normalization/streaming packages.

Durable architecture direction:
- OpenVibely is converging model-call behavior around a normalized provider request assembly pipeline.
- Lifecycle preparation precedes provider request construction for turns that need it.
- Selected memories, selected skills, task metadata, goals, follow-up metadata, attachments, and runtime tools are intended to become one normalized `AgentRequest`.
- Provider adapters consume that normalized contract instead of reinterpreting task/chat/follow-up state in ways that can drop context or tools.
- Provider routing should key off required request features such as chat history, `ChatSystemContext`, streaming, runtime tools, and task execution rather than ad hoc flags such as `!Followup`.

Current model-call shape:
- OpenVibely has a partial normalized `AgentRequest`, but some entry-specific code still decides where context lives before the request is built.
- Initial worker tasks run lifecycle and typically carry selected-memory handle context through `ProjectInstructions`.
- Chat/API chat and task-thread follow-ups share `processStreamingResponse` and carry follow-up selected-memory context through `ChatSystemContext`.
- Queued task-thread inputs promote back into the follow-up path.
- Lifecycle-selected memories/skills and runtime tools are carried through `context.Context` and extra-instruction helpers before `LLMService.callLLMDetailed` or `CallAgentDirectStreamingDetailed` constructs an `AgentRequest`.
- Provider adapters then make a second routing decision; this boundary previously allowed OpenAI task-thread follow-ups to drop `ChatSystemContext`.
- Direct utility services such as architect, backlog, collision, insights, trend, and upcoming use direct-style model calls and generally do not run task/chat lifecycle memory routing unless deliberately redesigned as user/task turns.

Provider/model selection facts:
- Provider/model selection is based on the selected `models.LLMConfig`, especially `Provider`, `Model`, and `AuthMethod`; the model string alone does not choose the provider.
- Normal task runs select model config in this order: `Task.AgentID`, then project `DefaultAgentConfigID`, then global default `agent_configs.is_default = 1`.
- Interactive chat selection differs: explicit `agent_id` uses that model config, `agent_id=default` uses the global default, and empty/`auto` triggers complexity/vision-based model selection.
- `Task.AgentDefinitionID` selects persona/system prompt/skills, not the provider/model.
- OpenAI supports Responses API, Completions API, and Codex CLI fallback. OpenAI Responses `SendAgentic` does Codex-style client-side history compaction for API key and OAuth flows.
- Anthropic uses `ProviderAnthropic`; OAuth/API key path uses `pkg/anthropicclient`; CLI path uses subprocess. Helpers live in `models/llm_config.go`.
- Ollama uses `/api/chat`, `ollama_base_url` migration 056, defaulting to `http://localhost:11434`.
- CLI-backed provider support for Anthropic and OpenAI/Codex remains a backend compatibility path, but CLI auth/options should not be exposed in the user-facing Models setup dialog.
- Fine-grained mid-run steering is supported only where OpenVibely owns the provider/tool loop, including OpenAI Responses agentic and Anthropic API/OAuth agentic paths. CLI/fallback transports such as Anthropic CLI, Codex CLI, Ollama, and OpenAI fallback paths remain coarse-grained.
- Provider adapter retries include a steering retry-reset hook so rows claimed during a failed retryable attempt are restored to guarded steering before the next attempt.
- OpenAI and Anthropic task/chat requests use the shared base system prompt plus provider-neutral worktree/project/lifecycle instructions; removed OpenAI OAuth-specific `working_with_user` prompt files should not be reintroduced.

OpenAI-compatible provider facts:
- `ProviderOpenAICompatible` is a separate generic Chat Completions path for inference servers/gateways that expose OpenAI-style `/chat/completions`; provider/model selection still comes from `LLMConfig.Provider`.
- The first cut is API-key oriented with optional missing auth for local servers, base URL/transport/preset config fields, and no OAuth provider-account flow.
- It routes through `internal/llm/openai_compatible` and reusable `pkg/openai_client` chat-completions code rather than first-party OpenAI Responses.
- Runtime behavior supports streaming text, provider tool calls/tool-result replay, usage normalization, and advanced extra headers/body JSON. Extra-body fields must not override protected request ownership such as `model`, `messages`, `stream`, or tool fields.
- Setup includes presets and best-effort `/models` discovery via `/models/openai-compatible/available`; discovery credentials are accepted via `X-OpenAI-Compatible-API-Key`, not query parameters.
- The Models setup UI intentionally has no manual “Discover Models” button; presets are shown directly in the Provider dropdown while submissions normalize to backend provider `openai_compatible` with hidden `preset_slug`.
- Discovery only auto-prefills the model selector when exactly one model is returned, and stale discovery responses must be ignored if the user switches provider/preset or changes base URL before response return.
- Preset catalog includes OpenRouter, NVIDIA NIM, Local vLLM, LM Studio, SGLang, LiteLLM, DeepInfra, Fireworks, Groq, Mistral, Cerebras, Together, Hugging Face Router, DeepSeek, Moonshot, DashScope, DashScope Intl, Alibaba Coding Plan, Z.AI/GLM, NovitaAI, Venice, Qianfan, Kilo Code, Arcee AI, StepFun, StepFun Step Plan, Tencent TokenHub, Tencent TokenHub Intl, Xiaomi MiMo, Inferrs, ds4 Local, GMI Cloud, Chutes, and Custom OpenAI-Compatible.
- Excluded/unverified candidates such as xAI, GitHub Copilot, native Bedrock/Gemini, and MiniMax should not be surfaced as generic presets or auto-normalized without a new explicit decision.
- Backend UI-provider normalization is allowlisted to known preset slugs while preserving canonical `provider=openai_compatible` configs with custom/future `preset_slug` values and saved `base_url` values during edit flows.
- Edit mode preserves saved OpenAI-compatible `preset_slug` and `base_url` values, including known-preset user overrides and unknown/future preset slugs. Preset default base URLs should only be applied when the user actively changes provider/preset, or when creating a new config with an empty base URL.
- Supported provider/model docs in the app repo and docs-site should continue to list Anthropic, OpenAI, Ollama, and OpenAI-compatible Chat Completions providers; hosted/local presets auto-load or probe models through header-only credentials, Custom is manual-entry oriented, API keys are described as header-only rather than URL parameters, and complete preset lists should name distinct variants such as DashScope Intl, StepFun Step Plan, and Tencent TokenHub Intl. Do not present starter model examples as an exhaustive supported-model catalog; compatible endpoints can discover models best-effort and users may enter exact model IDs manually.

OAuth and model-specific facts:
- OAuth access/refresh tokens are currently stored per `agent_configs` model config rather than shared per provider account, so two Anthropic model configs can have different OAuth freshness/reauth states even when both use the same account.
- `agent_configs.oauth_account_id` is provider-dependent: OpenAI OAuth populates it from token response identity, while Anthropic OAuth does not currently provide a reliable account identifier.
- Per-config OAuth storage is vulnerable to refresh-token rotation divergence. Durable direction is a provider-account token table with model configs referencing shared provider-account credentials; Anthropic needs an account-identity source before this can be reliably keyed.
- Anthropic/OpenAI OAuth token refresh is reactive, not background-scheduled. `EnsureFresh` runs on user-triggered provider use and on-demand Analytics account-usage refreshes, refreshing only when the stored access token is below the look-ahead threshold.
- Provider 401 recovery reloads the model config from DB and may refresh/persist rotated tokens; it does not reread OAuth token material from disk, keychain, or environment variables.
- Anthropic refresh-token expiry should be treated as opaque/server-controlled, not a fixed duration.
- Anthropic OAuth refresh failures with provider `invalid_grant` are permanent reauthorization failures: mark model config `oauth_needs_reauth`, surface `needs_reauth`/“Re-auth Required,” and clear the flag after successful refresh.
- Claude Fable 5 (`claude-fable-5`) and Claude Mythos 5 (`claude-mythos-5`) are supported Anthropic model IDs. They default to a 1M context window, support up to 128k output tokens, require adaptive thinking without fixed `budget_tokens`, do not return raw thinking blocks, and can return HTTP 200 refusal responses that should surface as unsuccessful/refusal results.

Provider-native tools and runtime tools:
- Provider-native web search/fetch is executed by providers, not local web tooling.
- OpenAI sends `{"type":"web_search"}`; legacy `web_search_preview` is compatibility mapping only.
- Anthropic sends direct versioned tool types such as `web_search_20250305` and `web_fetch_20250910` with names `web_search`/`web_fetch` through mixed raw-tools JSON.
- Anthropic provider-managed result block types match generically as `*_tool_result` and are carried forward in assistant history between `pause_turn` continuations.
- Anthropic `tool_use` and `server_tool_use` blocks must always carry object-valued `input`; missing/empty/invalid values serialize as `{}` and streaming history preserves start-block input when no JSON delta arrives.
- OpenAI Responses `function_call.arguments` is a JSON string and is normalized to `"{}"` when missing/empty before local tool execution and replay.
- Runtime tools are request-scoped, provider-generic, and carried through the LLM service/provider adapter path. Tool definitions carry read/write access classification.
- Memory tool exposure is a request/tool-profile decision, not a global provider-adapter default. Route-phase Memory Curator recall stays sanitized/no-tools, while update/consolidation hooks may receive scoped memory file tools.

Operational guidance belongs in skills such as `openvibely_provider_adapter_workflow` and `openvibely_chat_provider_test_workflow`.
