---
name: provider_architecture
type: project
created: 2026-05-09
updated: 2026-06-07
source: consolidation
source_id: memory_consolidation_2026_06_07
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
- OpenAI supports Responses API, Completions API, and Codex CLI fallback.
- OpenAI Responses `SendAgentic` does Codex-style client-side history compaction for API key and OAuth flows.
- OpenAI compaction uses a dedicated compact prompt and preserves both the opening task objective and newest context.
- OpenAI OAuth API calls append extra embedded `Working with the user` system-prompt guidance sourced from `runbooks/codex/prompt-base-gpt-5.4.md`; this guidance is OAuth-specific rather than shared across all providers.
- Anthropic uses `ProviderAnthropic`; OAuth/API key path uses `pkg/anthropicclient`; CLI path uses subprocess. Helpers live in `models/llm_config.go`.
- CLI-backed provider support for Anthropic and OpenAI/Codex remains a backend compatibility path, but CLI auth/options should not be exposed in the user-facing Models setup dialog; API key/OAuth setup paths remain user-facing where applicable.
- As of 2026-06-07, the Models setup dialog hides Anthropic/OpenAI CLI connection options while preserving backend CLI compatibility; OAuth setup keeps the hidden `auth_method`/connection-method selects enabled so browser submissions include `oauth`, and `resolveProviderAndAuth` defaults OAuth auth-type submissions to `AuthMethodOAuth` when `auth_method` is absent.
- Anthropic OAuth refresh failures with provider error `invalid_grant` are treated as permanent reauthorization failures: the model config is marked with `oauth_needs_reauth`, the Models UI/API surface `needs_reauth`/"Re-auth Required", and successful token refresh clears the flag while atomically persisting rotated access and refresh tokens.
- Ollama uses `/api/chat`, `ollama_base_url` migration 056, defaulting to `http://localhost:11434`.
- Raw LLM streaming token/output content is not intended for terminal logs at info level. Shared streaming output flows through `internal/llm/stream.Writer`, including OpenAI, Anthropic API/OAuth, Anthropic CLI, Codex CLI, and Ollama paths; subprocess stdout remains piped into the streaming path rather than mirrored to `os.Stdout`.
- Provider diagnostics that can produce noisy multi-line output, especially OpenAI/Anthropic rate-limit and provider response header dumps such as `x-openai`/`x-anthropic` headers, are debug-level diagnostics rather than info-level operational logs; retry/error recovery and significant tool/compaction events remain info-level operational logs.

Provider-native tools:
- Provider-native web search is executed by providers, not local web tooling.
- OpenAI sends `{"type":"web_search"}`; legacy `web_search_preview` is compatibility mapping only.
- Anthropic sends direct versioned tool types such as `web_search_20250305` and `web_fetch_20250910` with names `web_search`/`web_fetch` through mixed raw-tools JSON.
- Anthropic provider-managed result block types match generically as `*_tool_result` and are carried forward in assistant history between `pause_turn` continuations.

Runtime tool facts:
- Runtime tools are request-scoped, provider-generic, and carried through the LLM service/provider adapter path.
- Runtime tool definitions carry read/write access classification.
- Memory tool exposure is a request/tool-profile decision, not a global provider-adapter default.
- Route-phase Memory Curator recall stays sanitized/no-tools, while update/consolidation hooks may receive scoped memory file tools.

Operational guidance for implementing or auditing provider behavior lives in the project skill `.openvibely/skills/openvibely_provider_adapter_workflow/SKILL.md`. Chat/memory-specific provider request testing lives in `.openvibely/skills/openvibely_chat_provider_test_workflow/SKILL.md`.
