---
name: testing_coverage_and_performance
type: project
created: 2026-06-07
updated: 2026-06-14
source: consolidation
source_id: memory_consolidation_2026_06_13
confidence: high
title: Testing Coverage and Performance
---

OpenVibely has a large Go test suite whose measured coverage is limited more by breadth gaps and generated templ output than by lack of test count.

Coverage baseline and decisions:
- The 2026-06-07 audit found about 2,956 test functions, 247 test files, 47.4% overall unfiltered coverage, and 2,103 functions with 0% coverage.
- The coverage/test-count mismatch is primarily breadth bias: many granular tests exercise happy paths in already-tested files, while large adjacent files and error/pagination/webhook/retry paths remain unexecuted.
- `internal/service` and `internal/handler` were the largest durable coverage drains by function count.
- Major service gaps included `service/llm_service.go` at 0% measured coverage and large uncovered areas in Telegram/workflow/memory/worktree/chat action/routing/worker lifecycle/agent files/project services.
- Major handler gaps included schedule, project, workflow, collision, analytics, trend, autonomous, backlog, insights, attachment, SSE, and thread-input handlers.
- Entire or near-entire low/zero package areas included `cmd/server`, `internal/llm/ollama`, `internal/llm/workflow`, `internal/database/migrations`, generated `web/templates/*_templ.go`, `pkg/openai_client`, `internal/agentplugins`, `internal/models`, `internal/llm/anthropic`, and `internal/llm/openai`.
- Follow-up work added focused tests for previously untested handler/service areas, including schedule, project, workflow, collision, analytics, trend, autonomous, backlog, insights, attachment, SSE, thread-input handlers, and `internal/service/memory_service.go`.
- A real `AnalyzeTaskComplexity` nil-task crash in `workflow_handler.go` was fixed by checking `task == nil` after repository lookup.
- Total test functions reached about 3,176 after handler-test additions.
- Generated templ `*_templ.go` files are excluded from coverage summaries while template tests still run. Generated templ files significantly drag unfiltered coverage, so `Makefile` and GitHub Actions coverage summaries filter them before reporting.
- Filtered coverage after later handler-test additions was about 61.4%.

Durable coverage priorities:
- Highest-ROI remaining coverage target is still `service/llm_service.go`, a large core file with 0% measured coverage; it requires careful LLM caller mocking to avoid flaky tests.
- Expand existing `workflow_service_test.go` and `telegram_service_test.go` beyond happy paths, especially error, pagination, webhook, and retry paths.
- Add sparse LLM adapter tests for `internal/llm/anthropic`, `internal/llm/openai`, `internal/llm/ollama`, and `internal/llm/workflow`.
- Existing tests are mostly not wrong; the durable issue is narrow breadth, not excessive count. Avoid blanket `t.Parallel()` changes around shared DB setup.

Runtime and validation facts:
- Slowest packages in the 2026-06-07 audit were `internal/handler`, `internal/service`, `pkg/openai_client`, and `pkg/anthropic_client`.
- Test cost came from many fixed sleeps, repeated `NewTestContext()`/`NewTestDB()` setup, streaming parser/chunk reassembly tests, and DB/git/worktree-heavy service tests.
- Fixed-delay waits in several handler tests were replaced with readiness polling or `require.Eventually`; some timing-sensitive tests gained `testing.Short()` guards.
- `pkg/openai_client` and `pkg/anthropic_client` retry logic have a package-level `clockAfter = time.After` seam so tests can bypass real retry sleeps without changing production behavior.
- `TestRealtimeDiffUpdates` and several worker dispatch/project-limit tests have `testing.Short()` guards because they intentionally use sleep-based goroutine/ticker timing.
- A shared handler `TestMain` DB was deliberately not implemented because many handler tests mutate/query global/default/list state; `NewTestDB` already caches migrations and a shared DB would need transaction rollback isolation to be safe.
- Raw full-suite runs with a 60s timeout can fail from `internal/handler` and `internal/service` timing out under load rather than assertion failures. Prefer `make test`, `make test-cover`, or `go test ./... -count=1 -timeout 120s` for authoritative full validation.
- Full-suite failures may include unrelated/environmental desktop/config PATH issues or occasional SQLite-lock failures; distinguish repeatable touched-scope regressions from existing/environmental failure modes before attributing them to recent changes.
- As of 2026-06-14 after the test-suite stabilization pass, the former baseline failures in `TestHandler_Chat_PlanCompletionPrompt_ModeRestoreReevaluatesOnHydration`, `TestScheduleContent_DisabledScheduleRenderedWithGreyedStyle`, `TestMigration082_SkipsWhenLocalDevDBAlreadyApplied082`, and `TestMigration091_LocalDevAlreadyAppliedUsageChainStillMigrates` were fixed and should no longer be treated as expected failures.
- Current database migration tests expect the checkout to migrate through goose version `96` (`096_task_completed_at.sql`), not `95`.
- Chat mode selector tests should reflect the custom portal select implementation: hidden form input updates are driven by `chat-select-change` custom events carrying `e.detail.value`, not native `change` events or `this.value`.
- `internal/service` tests can intentionally emit logs such as `invalid character 'o' in literal null (expecting 'u')` when exercising malformed-JSON error paths; treat them as expected unless paired with a failing assertion/package result.
- On macOS, full-suite validation can emit Wails-related linker warnings while still passing; treat them as non-fatal unless packages fail or assertions regress.
- Handler `NewTestContext` now calls `h.SetLocalRepoPathEnabled(true)`, matching older `setupHandlerTest` default. Tests needing local paths disabled should explicitly call `tc.handler.SetLocalRepoPathEnabled(false)` after creating context.
