---
name: usage_analytics
type: project
created: 2026-06-03
updated: 2026-06-05
source: consolidation
source_id: memory_consolidation_2026_06_05
confidence: high
title: Usage Analytics
---

OpenVibely has persistent model-usage analytics for Anthropic and OpenAI OAuth/API-key paths. Usage is stored locally in `llm_usage_events`; OAuth account-limit snapshots are stored in `account_usage_snapshots`, with extra/model-specific account limits stored as child rows in `account_usage_extra_limits`.

Usage capture boundaries:
- Capture one final usage row per completed provider model call at the owning execution/call-site boundary, not per streamed chunk.
- Task execution, interactive chat, task-thread follow-ups, direct provider calls, failures, cancellations, and max-token/truncation cases can report usage when provider data is available.
- Duplicate protection is based on execution/operation-style idempotency so retries, streaming chunks, steering deferrals, and completion re-entry do not double-count.

Provider usage normalization:
- Anthropic API/OAuth usage preserves input/output tokens plus cache creation/read token fields when returned by `pkg/anthropicclient`; Anthropic thinking/reasoning is not currently available as a separate `reasoning_output_tokens` value and is treated as part of output tokens.
- OpenAI API/OAuth usage preserves input/output tokens plus cached/reasoning token fields from Responses API usage data.
- Cost fields are stored only when provider data exists; OpenVibely should not estimate provider costs silently.
- Provider response IDs, model/provider/account/config identifiers, timestamps, latency, status/failure/truncation/rate-limit metadata, and raw provider usage JSON are part of the analytics model when available.

Analytics surface:
- `/api/analytics/usage` backs the Analytics page usage section.
- The Analytics usage widget loads all local model usage by default rather than constraining the summary to the currently selected project; project-specific filtering remains an API concern.
- Direct/background model calls infer `project_id` from the call `workDir` when it exactly matches a known project repo path, so lifecycle/direct calls can appear in project-specific analytics without risky broad path matching.
- Analytics load/manual refresh attempts live OAuth account usage refreshes and keeps showing local usage plus the latest successful snapshot if live refresh fails; account-limit refresh warnings are separate from local usage visibility.
- The Analytics page should show provider account cards first in a responsive side-by-side grid, then `Token Usage` beside `Model Breakdown by Tokens`, followed by the `Token Usage Breakdown` table. The old `Daily Token Usage` graph should not appear on the page.
- The `Token Usage Breakdown` table card should size to its table, keep `Provider`, `Model`, `Input`, `Output`, `Cache`, `Reasoning`, `Total`, and `Cost` headers, and show an aggregate Total row instead of separate high-level token summary cards.
- The `Token Usage` chart has one independent model dropdown. Dropdown options include all models combined, each model as separate series, and individual provider/model choices; changing the dropdown must redraw only the Token Usage chart and must not refresh the token table or breakdown cards.
- Individual provider/model option values should use a safe printable key such as JSON-encoded `[provider, model]`, not NUL/control-character separators. The API may still expose rate-series fields such as `usage_rate_by_model` even though the UI label is `Token Usage`.
- Token-count breakdowns should be labeled `Model Breakdown by Tokens`, sit in the standard Analytics chart position next to `Token Usage`, use standard chart-card sizing/radius matching `Model Breakdown by Executions`, and render as a full pie chart with no doughnut hole. Execution-count breakdowns should be labeled `Model Breakdown by Executions`.
- Account-limit cards should use the provider as the heading and normalized plan/subscription metadata underneath. Do not title/display cards with raw `plan_type`, account IDs, config IDs, or user emails.
- Account-limit reset timestamps should render as relative countdown text like `Resets: 6 days, 4 hours, 12 minutes`, hiding non-applicable zero units; past or near reset times may show `Resets: 0 minutes`.

Historical/backfill behavior:
- Consolidated migration `091_llm_usage_tracking.go` creates usage analytics tables and backfills historical Anthropic/OpenAI OAuth/API-key `executions.tokens_used` totals into `llm_usage_events`.
- The Go migration normalizes older local unreleased usage schemas that used `request_status` into canonical `status` shape before backfilling, so dev DBs that already applied the removed `087`-`090` chain remain migratable.
- Historical backfilled rows are total-only; OpenVibely does not invent input/output/cache/cost breakdowns for old execution token counts.

OAuth account snapshot behavior:
- OpenAI OAuth account usage for ChatGPT/Codex bases should target `https://chatgpt.com/backend-api/wham/usage`: ChatGPT backend/codex/root bases normalize to `/backend-api/wham/usage`, not `/api/codex/usage`.
- OpenAI account usage GET should send `Authorization`, `ChatGPT-Account-ID`, `originator: codex_cli_rs`, `Accept: application/json`, and normal user-agent headers when available. If `OAuthAccountID` is missing, decode the ChatGPT account ID from the OAuth JWT claim `https://api.openai.com/auth.chatgpt_account_id`, then persist it through the normal OAuth account update path without exposing the JWT or claims outside the backend.
- OpenAI account usage parsing maps `rate_limit.primary_window` to the 5-hour/session limit and `rate_limit.secondary_window` to the weekly limit. Iterate all `additional_rate_limits[]` entries; store each extra/model-specific limit as a child row keyed by `metered_feature` (`limit_key`) with display label from `limit_name`.
- Preserve non-column OpenAI fields such as `credits`, `spend_control`, `rate_limit_reset_credits`, `last_token_usage`, and `total_token_usage` in sanitized raw JSON, and store `rate_limit_reached_type` when present.
- Normalize OpenAI/Codex raw `plan_type` in the backend for account-card subtitles: `free`, `plus`, `pro`, `team`, `enterprise`, `edu`, and `education` map to `ChatGPT ...`; unknown non-empty values are title-cased; empty values become `OpenAI subscription`.
- OpenAI/Codex account-card credit badges are provider-specific: `credits.has_credits=true` maps to “Usage credits available”, `credits.unlimited=true` to “Unlimited credits”, `credits.overage_limit_reached=true` to “Usage credit limit reached”, and `spend_control.reached=true` to “Spend limit reached”.
- Anthropic OAuth account usage GET should use only account-limit headers: `Authorization: Bearer <access-token>`, the current OAuth beta header such as `anthropic-beta: oauth-2025-04-20`, and `Content-Type: application/json`, with a 5-second timeout. Do not add inference-only billing attribution, `anthropic-version`, `x-app`, or extra Claude Code beta headers to this usage GET.
- Anthropic account usage parsing treats Claude-style snake_case keys as first-class: `five_hour` maps to the 5-hour/session limit and `seven_day` to the weekly limit. Named windows such as `seven_day_opus`/`seven_day_sonnet` become `account_usage_extra_limits` rows keyed by source field name.
- For Anthropic OAuth `/api/oauth/usage`, `utilization` is already percent-like: `1` means 1%, not 100%, so store it directly rather than multiplying fractions.
- Account-limit refresh must use the same repo-backed `internal/llm/oauth.Manager` freshness/recovery path as inference, persist rotated OAuth tokens back to the model config, and retry account usage once after a `401` with the recovered token.
- Account-limit refreshes read OAuth credentials only from existing secret-bearing `LLMConfig` fields. API-key accounts contribute local usage but do not get subscription account-limit snapshots or fake Analytics account-limit cards unless a real provider-specific account/billing API is added later.
- Account-limit cards and live refreshes group by actual provider OAuth account, not by model/config. OpenAI configs with the same `OAuthAccountID` collapse to one card. Anthropic configs should fetch `/api/oauth/profile` and persist/group by `organization.uuid` when available, falling back to `account.uuid`; if profile lookup fails, show per-config cards rather than guessing by token equality or usage payload similarity.
- Anthropic subscription/card metadata should come from `/api/oauth/profile`, not `/api/oauth/usage`: prefer `organization.rate_limit_tier`, then account max/pro flags, then organization type. Use billing/status/extra-usage metadata only as normalized card details, not raw identifiers.
- Do not use `agent_config_id`/config IDs as fake `account_id` values in Analytics account-limit views. In-memory grouping keys/fingerprints must not be stored or exposed.
- Never send OAuth access tokens, refresh tokens, OAuth metadata, raw auth headers, JWTs, decoded claims, in-memory account fingerprints, local config identifiers, or provider identity fields such as OpenAI `email`, `user_id`, and raw `account_id` to frontend, templates, logs, model tools, or tool results.
- Provider account-limit endpoints are fragile and may return denied, invalid-credential, or rate-limit responses; this affects live account-limit cards, not local token usage in `llm_usage_events`.
- Account refresh failures are stored as sanitized failure snapshots, preserving previous successful limit data when available and avoiding raw provider HTML/JSON response bodies in logs or storage.
- Automatic account-refresh backoff is coarse by status: successful snapshots refresh at most hourly; `429` backs off for 30 minutes; `401`/`403` backs off for 24 hours; other refresh failures back off for 6 hours. Manual Analytics refresh (`refresh=true`) bypasses cached-success TTLs and failure cooldowns.
