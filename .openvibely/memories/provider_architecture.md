---
name: provider_architecture
type: project
created: 2026-05-09
updated: 2026-06-05
source: consolidation
source_id: memory_consolidation_2026_06_05
confidence: high
title: Provider Architecture
---

Provider logic is isolated in adapter packages under `internal/llm`: OpenAI, Anthropic, and Ollama. Provider routing goes through `internal/service/provider_adapter.go` and shared contracts/normalization/streaming packages.

Provider paths:
- OpenAI supports Responses API, Completions API, and Codex CLI fallback. Responses `SendAgentic` does Codex-style client-side history compaction for API key and OAuth flows.
- OpenAI compaction has pre-turn transcript-size estimation and mid-turn session-token ledger behavior using the latest observed turn footprint plus estimated locally appended items. If `context_length_exceeded` happens before threshold compaction, force-compact and retry that turn once.
- OpenAI compaction uses a dedicated compact prompt (`openAICompactionInstructions` by default, optional override) rather than the full task system prompt. Preserve the compacted output from `/responses/compact` rather than re-summarizing/trimming it client-side.
- When trimming for OpenAI compaction requests, preserve both the opening task objective and newest context; tail-only trimming loses the actual task.
- OpenAI stream parsing should surface reasoning summary/content deltas plus fallbacks from output-item/completed blocks as `[Thinking]` stream blocks. OpenAI agentic requests set `reasoning.summary="auto"`.
- OpenAI OAuth API calls append extra embedded `Working with the user` system-prompt guidance sourced from `runbooks/codex/prompt-base-gpt-5.4.md`; this is OAuth-specific and should not leak into all providers.
- Anthropic uses `ProviderAnthropic` only; the old `ProviderClaudeMax` migration was merged. OAuth/API key path uses `pkg/anthropicclient`; CLI path uses subprocess. Helpers live in `models/llm_config.go`.
- Ollama uses `/api/chat`, `ollama_base_url` migration 056, defaulting to `http://localhost:11434`.

Provider-native tools:
- Provider-native web search is executed by providers, not local web tooling. OpenAI sends `{"type":"web_search"}`; legacy `web_search_preview` is compatibility mapping only.
- OpenAI web-search output items should round-trip without local execution. UI callbacks should emit useful status/source detail, not raw URL/query blobs.
- Anthropic sends direct versioned tool types such as `web_search_20250305` and `web_fetch_20250910` with names `web_search`/`web_fetch` through mixed raw-tools JSON; do not wrap these as `server_tool`.
- Anthropic provider-managed result block types match generically as `*_tool_result` and must be carried forward in assistant history between `pause_turn` continuations.
- Anthropic provider tool-result blocks must preserve the original JSON shape of `content`; replaying object payloads as strings can trigger provider invalid-shape errors.
- Anthropic provider tool callbacks should fire even on `stop_reason="end_turn"` and be emitted in stream order at `content_block_stop`; delayed post-turn callback emission can render tool cards after summary text.
- Do not append explicit provider web retrieval guidance prose to Anthropic system prompts. Enforce tool behavior in runtime/tool-loop logic to avoid user-visible implementation leakage.

Runtime tools and memory:
- Runtime tools are request-scoped, provider-generic, and carried through the LLM service/provider adapter path.
- Anthropic and OpenAI Responses agentic loops support a tool-boundary steering callback: after locally executed tool results are appended and before the next internal model request, the provider loop may ask the owning execution path for pending text-only steering and insert it as a user instruction.
- The owning execution path still owns durable steering prepare/commit/requeue state. Attachment-bearing steers remain on the outer continuation path until provider-loop attachment injection exists, and retry logic must not commit a claimed steer as applied unless the successful attempt also received it. Product-level queueing/steering rules live in `chat_thread_system.md`.
- When adding or fixing runtime tool profiles such as managed memory, verify both OpenAI and Anthropic API/OAuth adapter paths. A fix that only wires OpenAI can leave scheduled memory consolidation failing for Anthropic-backed agents even when the agent/tool profile is correct.
- Anthropic orchestrate chat with runtime action tools should send runtime tools without default local coding tools to avoid wasted tool-not-allowed turns. Task follow-up and plan modes keep mode-appropriate tools.
- Provider adapters should not make memory tools globally available; memory tool exposure is a request/tool-profile decision.
