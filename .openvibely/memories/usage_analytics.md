---
name: usage_analytics
type: project
created: 2026-06-03
updated: 2026-06-07
source: task
source_id: 31a88fc61719854001613dab2470b548
confidence: high
title: Usage Analytics
---

OpenVibely usage analytics are local operational analytics, not third-party product telemetry. No PostHog, Segment, Mixpanel, or Google Analytics-style frontend tracking was found in source as of 2026-06-06.

Durable analytics model:
- Persistent model-usage analytics exist for Anthropic and OpenAI OAuth/API-key paths.
- Usage rows are stored locally in `llm_usage_events`.
- OAuth account-limit snapshots are stored in `account_usage_snapshots`.
- Extra/model-specific account limits are stored as child rows in `account_usage_extra_limits`.
- Usage capture is one final row per completed provider model call at the owning execution/call-site boundary, not per streamed chunk.
- Provider cost fields are stored only when provider data exists; OpenVibely does not silently estimate provider costs.

Provider normalization facts:
- Anthropic API/OAuth usage preserves input/output tokens plus cache creation/read token fields when returned.
- Anthropic thinking/reasoning is not currently available as a separate `reasoning_output_tokens` value and is treated as part of output tokens.
- OpenAI API/OAuth usage preserves input/output tokens plus cached/reasoning token fields from Responses API usage data.
- OpenAI and Anthropic token totals are not directly comparable without normalization: OpenAI `input_tokens` includes cached tokens with cached tokens as a subcount, while Anthropic `input_tokens` excludes cache reads and reports cache creation/read separately.
- For normalized analysis, OpenAI uncached input is approximately `input_tokens - cached_input_tokens`, while Anthropic raw context touched is approximately `input_tokens + cache_creation_input_tokens + cache_read_input_tokens`.
- Historical backfilled rows from `executions.tokens_used` are total-only and do not invent input/output/cache/cost breakdowns.

Analytics surface facts:
- `/analytics` includes local task/execution/productivity analytics plus LLM usage/account-limit views.
- `/api/analytics/usage` backs the Analytics page usage section.
- The Analytics page usage fetch includes the selected `project_id` so model usage charts/tables reflect the current project.
- Direct/background model calls can infer `project_id` from exact project repo-path `workDir` matches.
- Analytics date/hour buckets and the built-in `month` range use app/local timezone semantics matching Schedules.
- Provider account cards appear before usage charts/tables.
- The usage chart label is `Token Usage`; token-count breakdowns are labeled `Model Breakdown by Tokens`; execution-count breakdowns are labeled `Model Breakdown by Executions`.
- Account-limit cards use the provider as the heading with normalized plan/subscription metadata underneath; raw plan types, account IDs, config IDs, emails, and provider identity fields are not public card labels.
- Account-limit horizontal usage bars intentionally use explicit shared-theme track/fill meter markup instead of native/DaisyUI `<progress>` rendering, after a 2026-06-07 packaged macOS desktop light-mode WebView issue made the native bars invisible while web and dark modes appeared correct.

OAuth account facts:
- OpenAI ChatGPT/Codex account usage targets `https://chatgpt.com/backend-api/wham/usage`.
- OpenAI account grouping uses real `OAuthAccountID`; configs with the same account collapse to one card.
- Anthropic account usage and profile behavior is split: usage payloads provide windows/utilization, while profile metadata drives public subscription labels where available.
- Anthropic configs group by stable organization/account identity when profile lookup succeeds; otherwise per-config display is safer than guessing.
- API-key accounts contribute local usage but do not get subscription/account-limit snapshots or fake billing cards.
- OAuth access tokens, refresh tokens, JWTs, decoded claims, auth headers, local config IDs, raw account IDs, fingerprints, and provider identity fields are backend-only and absent from frontend, templates, logs, model tools, and tool results.
- Provider account-limit endpoints are fragile; refresh failures affect live account cards, not local token usage.

Operational implementation guidance for usage persistence, provider usage normalization, account-limit refresh, Analytics UI, migrations/backfill, and regression coverage belongs in `.openvibely/skills/openvibely_model_usage_tracking_workflow/SKILL.md`.
