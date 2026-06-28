---
name: testing_coverage_and_performance
type: project
created: 2026-06-07
updated: 2026-06-28
source: after_complete
source_id: 73909aacfedf6c4f4048bbb5d2f07b8f:fc3d69765fe0c6ab
confidence: high
title: Testing Coverage and Performance
---

OpenVibely has a large Go test suite whose measured coverage is limited more by breadth gaps and generated templ output than by lack of test count.

Coverage baseline and decisions:
- The 2026-06-07 audit found roughly 3k test functions and about 47% unfiltered coverage, with many functions still uncovered despite the high test count.
- The coverage/test-count mismatch is primarily breadth bias: many granular tests exercise happy paths in already-tested files, while large adjacent files and error/pagination/webhook/retry paths remain unexecuted.
- `internal/service` and `internal/handler` were the largest durable coverage drains by function count.
- Major service gaps included `service/llm_service.go` at 0% measured coverage and large uncovered areas in Telegram/workflow/memory/worktree/chat action/routing/worker lifecycle/agent files/project services.
- Major handler gaps included schedule, project, workflow, collision, analytics, trend, autonomous, backlog, insights, attachment, SSE, and thread-input handlers.
- Follow-up work added focused tests for many previously untested handler/service areas and raised filtered coverage to roughly the low 60% range.
- Generated templ `*_templ.go` files are excluded from coverage summaries while template tests still run. Generated templ files significantly drag unfiltered coverage, so `Makefile` and GitHub Actions coverage summaries filter them before reporting.

Durable coverage priorities:
- Highest-ROI remaining coverage target is still `service/llm_service.go`, a large core file with 0% measured coverage; it requires careful LLM caller mocking to avoid flaky tests.
- Expand existing `workflow_service_test.go` and `telegram_service_test.go` beyond happy paths, especially error, pagination, webhook, and retry paths.
- Add sparse LLM adapter tests for `internal/llm/anthropic`, `internal/llm/openai`, `internal/llm/ollama`, and `internal/llm/workflow`.
- Existing tests are mostly not wrong; the durable issue is narrow breadth, not excessive count. Avoid blanket `t.Parallel()` changes around shared DB setup.
- Outbound DM permissions coverage from task `71ecb86c455fdd6b61883ed82f813d56` is complete as of 2026-06-28: the follow-up added handler tests for Discord user-DM target persistence, saved Discord user-DM test-button dispatch, Authorized Users add/delete not mutating outbound targets, and outbound target save/delete not mutating Slack/Discord authorized users. A fresh read-only audit then found no material issues, and affected `internal/service`, `internal/repository`, and `internal/handler` tests passed with `-count=1`.
- Channel project-switch persistence coverage from task `73909aacfedf6c4f4048bbb5d2f07b8f` is complete as of 2026-06-28: Slack `buildSlackActionToolRuntime`/runtime `switch_project` now asserts a `slack_user_projects` write and later active-project resolution; Telegram runtime `switch_project` asserts a `telegram_user_projects` write and later active-project resolution; and web/API `switch_project` is guarded as informational-only with no writes to Discord/Slack/Telegram/Email active-project tables. The task also resolved an Email sender-project migration-number collision by renumbering it to `104_email_sender_projects.sql`. A fresh read-only audit found no material issues, and affected `internal/database`, `internal/repository`, `internal/handler`, and `internal/service` tests passed with `-count=1`. The later outbound-DM permissions follow-up corrected the stale `internal/chatcontrol` registry assertion for the canonical `platform:user:<id>` DM syntax, so that prior chatcontrol failure is no longer current.

Runtime and validation facts:
- Slowest packages in the 2026-06-07 audit were `internal/handler`, `internal/service`, `pkg/openai_client`, and `pkg/anthropic_client`.
- Test cost came from fixed sleeps, repeated `NewTestContext()`/`NewTestDB()` setup, streaming parser/chunk reassembly tests, and DB/git/worktree-heavy service tests.
- Some fixed-delay waits were replaced with readiness polling or `require.Eventually`; timing-sensitive tests may use `testing.Short()` guards.
- `pkg/openai_client` and `pkg/anthropic_client` retry logic have a package-level `clockAfter = time.After` seam so tests can bypass real retry sleeps without changing production behavior.
- A shared handler `TestMain` DB was deliberately not implemented because many handler tests mutate/query global/default/list state; `NewTestDB` already caches migrations and a shared DB would need transaction rollback isolation to be safe.
- Prefer `make test`, `make test-cover`, or `go test ./... -count=1 -timeout 120s` for authoritative full validation. Raw 60s full-suite runs can time out in `internal/handler` or `internal/service` under load.
- Full-suite failures may include unrelated/environmental desktop/config PATH issues, macOS Wails linker warnings, occasional SQLite-lock failures, or date-sensitive handler reschedule tests; distinguish repeatable touched-scope regressions from existing/environmental failure modes before attributing them to recent changes.
- Current database migration tests expect goose version `103` after Mixture of Models added `103_mixture_provider.sql`, which rebuilds `agent_configs` to allow `provider='mixture'` and adds `mixture_config_json`. Migration `100` repairs the local-dev case where an old Discord migration occupied goose version `099` before main added outbound message target migrations; Discord’s channel schema migration is `101_discord_channel.go`, and Discord persisted per-user project selection is `102_discord_user_projects.sql`.
- Handler `NewTestContext` now calls `h.SetLocalRepoPathEnabled(true)`, matching older `setupHandlerTest` default. Tests needing local paths disabled should explicitly call `tc.handler.SetLocalRepoPathEnabled(false)` after creating context.
- Chat mode selector tests should reflect the custom portal select implementation: hidden form input updates are driven by `chat-select-change` custom events carrying `e.detail.value`, not native `change` events or `this.value`.
- `internal/service` tests can intentionally emit malformed-JSON logs when exercising error paths; treat them as expected unless paired with a failing assertion/package result.
