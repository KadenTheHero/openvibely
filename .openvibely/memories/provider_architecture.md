---
name: provider_architecture
type: project
created: 2026-05-09
updated: 2026-06-09
source: task
source_id: 351323f14eeac1e2ebec4226600c5b81
confidence: high
title: Provider Architecture
---

Provider logic is isolated in adapter packages under `internal/llm`: OpenAI, Anthropic, and Ollama. Provider routing goes through `internal/service/provider_adapter.go` and shared contracts/normalization/streaming packages.

Durable architecture direction:
- OpenVibely is converging model-call behavior around a normalized provider request assembly pipeline.
- Lifecycle preparation precedes provider request construction for turns that need it.
- Selected memories, selected skills, task metadata, goals, follow-up metadata, attachments, and runtime tools are intended to become one normalized `AgentRequest`.
- Provider adapters consume that normalized contract instead of reinterpreting task/chat/follow-up state in ways that can drop context or tools.
- Provider routing is intended to key off required request features such as chat history, `ChatSystemContext`, streaming, runtime tools, and task execution rather than ad hoc flags such as `!Followup`.

Current model-call shape as of 2026-06-07:
- OpenVibely has a partial normalized `AgentRequest`, but some entry-specific code still decides where context lives before the request is built.
- Initial worker tasks run lifecycle and typically carry selected-memory handle context through `ProjectInstructions`.
- Chat/API chat and task-thread follow-ups share `processStreamingResponse` and carry follow-up selected-memory context through `ChatSystemContext`.
- Queued task-thread inputs promote back into the follow-up path.
- Lifecycle-selected memories/skills and runtime tools are first carried through `context.Context` and extra-instruction helpers before `LLMService.callLLMDetailed` or `CallAgentDirectStreamingDetailed` constructs an `AgentRequest`.
- Provider adapters then make a second routing decision; this boundary previously allowed OpenAI task-thread follow-ups to drop `ChatSystemContext`.
- Direct utility services such as architect, backlog, collision, insights, trend, and upcoming use direct-style model calls and generally do not run task/chat lifecycle memory routing unless deliberately redesigned as user/task turns.

Provider facts:
- Provider/model selection is based on the selected `models.LLMConfig`, especially its `Provider`, `Model`, and `AuthMethod`; the model string alone does not choose the provider.
- Normal task runs select the concrete model config in this order: `Task.AgentID`, then project `DefaultAgentConfigID`, then the global default `agent_configs.is_default = 1`.
- Interactive chat selection differs: explicit `agent_id` uses that model config, `agent_id=default` uses the global default, and empty/`auto` triggers complexity/vision-based model selection rather than defaulting.
- `Task.AgentDefinitionID` selects persona/system prompt/skills, not the provider/model; provider still comes from the selected `LLMConfig`.
- OpenAI supports Responses API, Completions API, and Codex CLI fallback.
- OpenAI Responses `SendAgentic` does Codex-style client-side history compaction for API key and OAuth flows.
- Fine-grained mid-run steering is supported where OpenVibely owns the provider/tool loop and can inject at model-step boundaries, including OpenAI Responses agentic and Anthropic API/OAuth agentic paths. CLI/fallback transports such as Anthropic CLI, Codex CLI, Ollama, and OpenAI fallback paths remain coarse-grained because OpenVibely sees them as subprocess/provider calls rather than internal tool loops.
- Provider adapter retries include a steering retry-reset hook: rows claimed during a failed retryable attempt are restored to guarded steering state before the next attempt, so a later successful attempt must actually receive the steer before it can be committed.
- OpenAI compaction uses a dedicated compact prompt and preserves both the opening task objective and newest context; it compacts prior history only, so it does not fire on first/simple turns with empty history.
- OpenAI OAuth no longer appends an extra provider-specific `Working with the user` system-prompt file; `internal/llm/prompt/openai_oauth_prompt.go` and `internal/llm/prompt/openai_oauth_working_with_user.txt` were removed, and OpenAI OAuth now uses the shared base system prompt plus provider-neutral worktree/project/lifecycle instructions like Anthropic.
- Anthropic has no equivalent provider-specific `working_with_user` prompt file; Anthropic task/chat requests use the shared base system prompt plus provider-neutral worktree/project/lifecycle instructions.
- Anthropic uses `ProviderAnthropic`; OAuth/API key path uses `pkg/anthropicclient`; CLI path uses subprocess. Helpers live in `models/llm_config.go`.
- CLI-backed provider support for Anthropic and OpenAI/Codex remains a backend compatibility path, but CLI auth/options should not be exposed in the user-facing Models setup dialog; API key/OAuth setup paths remain user-facing where applicable.
- As of 2026-06-07, the Models setup dialog hides Anthropic/OpenAI CLI connection options while preserving backend CLI compatibility; OAuth setup keeps the hidden `auth_method`/connection-method selects enabled so browser submissions include `oauth`, and `resolveProviderAndAuth` defaults OAuth auth-type submissions to `AuthMethodOAuth` when `auth_method` is absent.
- OAuth access/refresh tokens are currently stored per `agent_configs` model config rather than shared per provider account, so two Anthropic model configs can have different OAuth freshness/reauth states even when both use the same Anthropic OAuth account and `ProviderAnthropic`.
- `agent_configs.oauth_account_id` exists, but as of 2026-06-09 it is effectively provider-dependent: OpenAI OAuth populates it from the token response `id_token`/`chatgpt_account_id`, while Anthropic OAuth does not populate an account identifier, so OpenVibely cannot currently determine from stored data whether two Anthropic model configs use the same Anthropic account.
- This per-config OAuth storage is vulnerable to refresh-token rotation divergence: if two model configs start with copied/shared Anthropic refresh token material, the first successful refresh can rotate/replace the token for that config while leaving the other config with an invalidated stale token that fails with HTTP 400/`invalid_grant`.
- Durable architecture direction: OAuth credentials should move toward a provider-account token table, with model configs referencing the shared provider-account credential, so all Anthropic configs for the same OAuth account benefit from one rotated token pair; Anthropic needs an account-identity source such as a userinfo/account endpoint during OAuth connect before this can be reliably keyed by account.
- Anthropic/OpenAI OAuth token refresh is reactive, not background-scheduled: there is no ticker/cron/goroutine that proactively refreshes tokens. OAuth token persistence stores access token, refresh token, and `OAuthExpiresAt` derived from the provider token endpoint's `expires_in`; the hardcoded 1-hour value is only the refresh look-ahead threshold, not the access-token TTL. `EnsureFresh` runs on user-triggered provider use such as prompting and on-demand Analytics account-usage refreshes; the provider refresh endpoint is only called when the stored access token has less than a 1-hour TTL remaining, so with roughly 8-hour access tokens active prompting usually refreshes about every 7 hours per model config. On provider 401 recovery, OpenVibely reloads the model config from the DB and may refresh/persist rotated tokens; it does not reread OAuth token material from disk, keychain, or environment variables. Anthropic does not publish a reliable refresh-token TTL for this integration; treat refresh-token expiry as opaque/server-controlled and potentially inactivity/revocation driven rather than depending on a fixed duration.
- In the local project runtime state as of 2026-06-09, configured Anthropic credentials were OAuth-only entries in `$HOME/.openvibely/openvibely.db` `agent_configs.oauth_access_token`; no `ANTHROPIC_API_KEY` env var or stored Anthropic `agent_configs.api_key` was present. Do not print token values when diagnosing access.
- A 2026-06-09 `claude-fable-5` Messages verification ultimately succeeded with the configured Anthropic OAuth token when the request used OpenVibely's normal Anthropic OAuth/agentic request shape, including the Claude Code billing-attribution system block; earlier hand-built minimal OAuth Messages requests without that block reached Anthropic but returned HTTP 429 `rate_limit_error` even for existing Claude models.
- Claude Fable 5 (`claude-fable-5`) and Claude Mythos 5 (`claude-mythos-5`) are supported Anthropic models in OpenVibely as of 2026-06-09. They default to a 1M context window, support up to 128k output tokens, require adaptive thinking (`thinking: {"type":"adaptive"}`), must never receive fixed `budget_tokens`, do not return raw thinking blocks, and can return HTTP 200 with `stop_reason: "refusal"`, which should be surfaced as an unsuccessful/refusal result rather than silent success.
- Anthropic OAuth refresh failures with provider error `invalid_grant` are treated as permanent reauthorization failures: the model config is marked with `oauth_needs_reauth`, the Models UI/API surface `needs_reauth`/"Re-auth Required", and successful token refresh clears the flag while atomically persisting rotated access and refresh tokens.
- Ollama uses `/api/chat`, `ollama_base_url` migration 056, defaulting to `http://localhost:11434`.
- Raw LLM streaming token/output content is not intended for terminal logs at info level. Shared streaming output flows through `internal/llm/stream.Writer`, including OpenAI, Anthropic API/OAuth, Anthropic CLI, Codex CLI, and Ollama paths; subprocess stdout remains piped into the streaming path rather than mirrored to `os.Stdout`.
- Provider diagnostics that can produce noisy multi-line output, especially OpenAI/Anthropic rate-limit and provider response header dumps such as `x-openai`/`x-anthropic` headers, are debug-level diagnostics rather than info-level operational logs; retry/error recovery and significant tool/compaction events remain info-level operational logs.

Provider-native tools:
- Provider-native web search is executed by providers, not local web tooling.
- OpenAI sends `{"type":"web_search"}`; legacy `web_search_preview` is compatibility mapping only.
- Anthropic sends direct versioned tool types such as `web_search_20250305` and `web_fetch_20250910` with names `web_search`/`web_fetch` through mixed raw-tools JSON.
- Anthropic provider-managed result block types match generically as `*_tool_result` and are carried forward in assistant history between `pause_turn` continuations.
- Anthropic agentic `tool_use` and `server_tool_use` blocks must always carry object-valued `input`; empty, nil, null, or invalid inputs are serialized as `{}`, and streaming history preserves `input` from `content_block_start` when no `input_json_delta` arrives.
- OpenAI Responses agentic `function_call.arguments` is the analogous replay/tool-execution field, but it is a JSON string rather than an object; missing or empty arguments are normalized to `"{}"` before local tool execution and turn-continuation replay.

Runtime tool facts:
- Runtime tools are request-scoped, provider-generic, and carried through the LLM service/provider adapter path.
- Runtime tool definitions carry read/write access classification.
- Memory tool exposure is a request/tool-profile decision, not a global provider-adapter default.
- Route-phase Memory Curator recall stays sanitized/no-tools, while update/consolidation hooks may receive scoped memory file tools.

Operational guidance for implementing or auditing provider behavior lives in the project skill `.openvibely/skills/openvibely_provider_adapter_workflow/SKILL.md`. Chat/memory-specific provider request testing lives in `.openvibely/skills/openvibely_chat_provider_test_workflow/SKILL.md`.
