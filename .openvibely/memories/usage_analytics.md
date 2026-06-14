---
name: usage_analytics
type: project
created: 2026-06-03
updated: 2026-06-13
source: consolidation
source_id: memory_consolidation_2026_06_13
confidence: high
title: Usage Analytics
---

OpenVibely usage analytics are local operational analytics, not third-party product telemetry. No PostHog, Segment, Mixpanel, or Google Analytics-style frontend tracking was found in source as of 2026-06-06.

Durable analytics model:
- Persistent model-usage analytics exist for Anthropic and OpenAI OAuth/API-key paths, plus OpenAI-compatible Chat Completions API-key configs.
- OpenAI-compatible Chat Completions usage is parsed by the shared client/adapter into canonical input/output/total/cached/reasoning fields and persisted through `RecordUsageFromResult` for `ProviderOpenAICompatible` API-key configs. Analytics displays these rows as provider `openai_compatible` with the exact configured model ID in totals, usage-rate series, and model breakdowns.
- Skill analytics events are stored locally in `skill_analytics_events`.
- Usage rows are stored locally in `llm_usage_events`.
- OAuth account-limit snapshots are stored in `account_usage_snapshots`, with extra/model-specific account limits stored in `account_usage_extra_limits`.
- Usage capture is one final row per completed provider model call at the owning execution/call-site boundary, not per streamed chunk.
- Skill analytics event semantics distinguish selection, consumption, and skill changes: `selected` records routed/available skills, `loaded` records successful full-body `skill_view` loads, `viewed` records successful `skill_view` access, `created` records skill creation, and `edited` records app-managed skill updates/mutations.
- Skill create/edit telemetry is separated: create/import UI paths record `created` only for new skills, overwrites record `edited`, mutation tools record `created` for create actions, and other successful mutations record `edited`.
- Provider cost fields are stored only when provider data exists; OpenVibely does not silently estimate provider costs.

Provider normalization facts:
- Anthropic API/OAuth usage preserves input/output tokens plus cache creation/read token fields when returned. Anthropic thinking/reasoning is not currently available as separate `reasoning_output_tokens` and is treated as output tokens.
- OpenAI API/OAuth usage preserves input/output tokens plus cached/reasoning token fields from Responses API usage.
- OpenAI and Anthropic token totals are not directly comparable without normalization: OpenAI `input_tokens` includes cached tokens with cached as a subcount, while Anthropic `input_tokens` excludes cache reads and reports cache creation/read separately.
- For normalized analysis, OpenAI uncached input is approximately `input_tokens - cached_input_tokens`, while Anthropic raw context touched is approximately `input_tokens + cache_creation_input_tokens + cache_read_input_tokens`.
- Historical backfilled rows from `executions.tokens_used` are total-only and do not invent input/output/cache/cost breakdowns.
- Usage analytics group by stored provider/model strings rather than a hardcoded model allowlist. Claude Fable/Mythos usage appears as Anthropic rows when Anthropic returns counters; provider access failures with no counters produce no usage row.
- HTTP 200 Anthropic refusals can still be recorded as failed usage when the response includes counters.

Analytics surface facts:
- `/analytics` includes local task/execution/productivity analytics, LLM usage/account-limit views, and Skill Curator analytics.
- `/api/analytics/usage` backs the Analytics page usage section; `/api/analytics/skills` backs Skill Curator Analytics.
- The Analytics page usage fetch includes selected `project_id` so model usage charts/tables reflect the current project.
- Direct/background model calls infer `project_id` from workdir paths by matching exact project repo paths, paths inside repos, and conventional task worktrees under `.worktrees/task_*`; nested repos choose the most specific project match.
- Task commit-summary LLM calls pass isolated task worktree paths as `workDir` and are expected to resolve back to the owning project.
- Analytics date/hour buckets and the built-in `month` range use app/local timezone semantics matching Schedules.
- Provider account cards appear before usage charts/tables.
- Skill Curator Analytics appears immediately above the Failed Task Patterns card.
- The usage chart label is `Token Usage`; token-count breakdowns are `Model Breakdown by Tokens`; execution-count breakdowns are `Model Breakdown by Executions`.
- The Token Usage chart card's model selector must stay within the card on narrow/mobile widths: avoid an unconditional fixed minimum width on the select, keep the header/select wrapper shrink-safe with `min-w-0`, let the select use `w-full max-w-full` on narrow screens, and apply wider minimums only at larger breakpoints such as `sm:min-w-48`.
- Account-limit cards use provider as the heading with normalized plan/subscription metadata underneath; raw plan types, account IDs, config IDs, emails, and provider identity fields are not public card labels.
- Account-limit horizontal usage bars intentionally use explicit shared-theme track/fill meter markup instead of native/DaisyUI `<progress>` rendering because packaged macOS desktop light-mode WebView made native bars invisible.

Skill Curator analytics UI facts:
- Trend chart label is `Skill Activity Over Time` and should show exactly three always-visible trend lines with no dropdown/series selector: `Used` (`selected + loaded + viewed`), `Created`, and `Edited`.
- The dashboard visually matches the existing Analytics page chart/card style and uses standalone cards, not one combined mega-card.
- Ordering: Skill Activity Over Time next to Top Skills; Follow-through/Selected Outcomes next to Top Agent/Skill Pairs; Least Active Enabled Skills as its own full-width horizontal card.
- Top Skills/Overview, Follow-through/Selected Outcomes, and Top Agent/Skill Pairs use consistent standard graph styling, not pie charts or extra table-heavy UI. Least Active Enabled Skills remains table-only.
- Skill-specific filters such as skill surface, scope, and agent are not shown by default; dashboard defaults those filters to all.
- Agent heatmap/cell activity counts `selected + loaded + viewed`; `edited` belongs in drilldown/tooltip context.
- Shared Analytics date-range selector preserves the 365-day/Last Year option for non-skill usage analytics while skill analytics may map supported ranges separately.

OAuth account facts:
- OpenAI ChatGPT/Codex account usage targets `https://chatgpt.com/backend-api/wham/usage`.
- OpenAI account grouping uses real `OAuthAccountID`; configs with the same account collapse to one card.
- Anthropic account usage and profile behavior is split: usage payloads provide windows/utilization, while profile metadata drives public subscription labels when available.
- Anthropic configs group by stable organization/account identity when profile lookup succeeds; otherwise per-config display is safer than guessing.
- API-key accounts contribute local usage but do not get subscription/account-limit snapshots or fake billing cards.
- OAuth access tokens, refresh tokens, JWTs, decoded claims, auth headers, local config IDs, raw account IDs, fingerprints, and provider identity fields are backend-only and absent from frontend, templates, logs, model tools, and tool results.
- Provider account-limit endpoints are fragile; refresh failures affect live account cards, not local token usage.

Operational guidance belongs in `openvibely_model_usage_tracking_workflow`.
