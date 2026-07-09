---
name: testing_coverage_and_performance
type: project
created: 2026-06-07
updated: 2026-07-09
source: after_complete_memory_update
source_id: 5e2f64df7cb867d549abf9d044450714
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
- Outbound DM permissions coverage is in place for Discord user-DM target persistence, saved Discord user-DM test dispatch, Authorized Users add/delete isolation from outbound targets, and outbound target save/delete isolation from Slack/Discord authorized users.
- Channel project-switch persistence coverage is in place for Slack and Telegram runtime `switch_project` writes plus later active-project resolution. Web/API `switch_project` is guarded as informational-only and must not write Discord/Slack/Telegram/Email active-project tables.
- The canonical outbound direct-message target syntax in chat-control assertions is `platform:user:<id>`.

Runtime and validation facts:
- Slowest packages in the 2026-06-07 audit were `internal/handler`, `internal/service`, `pkg/openai_client`, and `pkg/anthropic_client`.
- Test cost came from fixed sleeps, repeated `NewTestContext()`/`NewTestDB()` setup, streaming parser/chunk reassembly tests, and DB/git/worktree-heavy service tests.
- Some fixed-delay waits were replaced with readiness polling or `require.Eventually`; timing-sensitive tests may use `testing.Short()` guards.
- `pkg/openai_client` and `pkg/anthropic_client` retry logic have a package-level `clockAfter = time.After` seam so tests can bypass real retry sleeps without changing production behavior.
- A shared handler `TestMain` DB was deliberately not implemented because many handler tests mutate/query global/default/list state; `NewTestDB` already caches migrations and a shared DB would need transaction rollback isolation to be safe.
- Prefer `make test`, `make test-cover`, or `go test ./... -count=1 -timeout 120s` for authoritative full validation. Raw 60s full-suite runs can time out in `internal/handler` or `internal/service` under load.
- Full-suite failures may include unrelated/environmental desktop/config PATH issues, macOS Wails linker warnings, occasional SQLite-lock failures, date-sensitive handler reschedule tests, plugin marketplace clone/network timeouts in `internal/agentplugins`/`internal/server`, or the missing `.openvibely/skills/openvibely_github_autonomous_sdlc_bootstrap/SKILL.md` fixture causing `internal/agentskills` failures; distinguish repeatable touched-scope regressions from existing/environmental failure modes before attributing them to recent changes. When default temp-dir runs hang during plugin marketplace cloning, rerunning the full suite with `TMPDIR=/private/tmp` has been the stable validation path.
- Handler `NewTestContext` now calls `h.SetLocalRepoPathEnabled(true)`, matching older `setupHandlerTest` default. Tests needing local paths disabled should explicitly call `tc.handler.SetLocalRepoPathEnabled(false)` after creating context.
- Chat mode selector tests should reflect the custom portal select implementation: hidden form input updates are driven by `chat-select-change` custom events carrying `e.detail.value`, not native `change` events or `this.value`.
- `internal/service` tests can intentionally emit malformed-JSON logs when exercising error paths; treat them as expected unless paired with a failing assertion/package result.
- `TestTelegramServiceConcurrentUpdateTokenIsSerialized` in `internal/service/telegram_service_test.go` (covers concurrent `UpdateToken` lifecycle serialization) was flaky as of 2026-07-01 (`seenReplacementPollers` could remain empty because `UpdateToken` could return before the test consumed the buffered `started`-poller notification, a test-side race, not a production bug). Fixed on 2026-07-02 by having the test track the last-completed token update, assert the service's current bot token matches it, and wait for that specific current-token poller instead of requiring a notification to be consumed before both `UpdateToken` calls return. Confirmed stable with `-count=20` and `-race -count=10` reruns. When writing concurrency regression tests with buffered notification channels, prefer waiting on the final/current observable state rather than asserting a notification was drained by loop end, since goroutine notification and the operation's completion signal can race.
