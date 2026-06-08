---
name: testing_coverage_and_performance
type: project
created: 2026-06-07
updated: 2026-06-08
source: consolidation
source_id: memory_consolidation_2026_06_08
confidence: high
title: Testing Coverage and Performance
---

OpenVibely has a large Go test suite whose measured coverage is limited more by breadth gaps and generated templ output than by lack of test count.

Coverage baseline from the 2026-06-07 audit:
- `go test ./... -count=1 -timeout 60s -coverprofile=/tmp/coverage.out` found about 2,956 test functions, 247 test files, 47.4% overall unfiltered coverage, and 2,103 functions with 0% coverage.
- The coverage/test-count mismatch is primarily breadth bias: many granular tests exercise happy paths in already-tested files, while large adjacent files and error/pagination/webhook/retry paths remain unexecuted.
- `internal/service` and `internal/handler` were the largest durable coverage drains by function count.
- Major service gaps included `service/llm_service.go` at 0% measured coverage and large uncovered areas in Telegram/workflow/memory/worktree/chat action/routing/worker lifecycle/agent files/project services.
- Major handler gaps included schedule, project, workflow, collision, analytics, trend, autonomous, backlog, insights, attachment, SSE, and thread-input handlers.
- Entire or near-entire low/zero package areas included `cmd/server`, `internal/llm/ollama`, `internal/llm/workflow`, `internal/database/migrations`, generated `web/templates/*_templ.go`, `pkg/openai_client`, `internal/agentplugins`, `internal/models`, `internal/llm/anthropic`, and `internal/llm/openai`.

Coverage decisions and current state:
- Follow-up work on 2026-06-07 added focused tests for previously untested handler/service areas, including schedule, project, workflow, collision, analytics, trend, autonomous, backlog, insights, attachment, SSE, thread-input handlers, and `internal/service/memory_service.go`.
- A real `AnalyzeTaskComplexity` nil-task crash in `workflow_handler.go` was fixed by checking `task == nil` after repository lookup.
- Total test functions reached about 3,176 after the handler-test additions.
- Generated templ `*_templ.go` files are excluded from coverage summaries while template tests still run. Generated templ files accounted for about 14,715 of 40,714 coverage-profile lines, dragging total reported coverage from 60.9% filtered to 47.7% unfiltered.
- `Makefile` test targets include `test`, `test-short`, and `test-cover`; `test-cover` filters generated templ output from the coverage report.
- GitHub Actions test coverage summary also filters `*_templ.go` entries before reporting coverage, so CI should report the filtered coverage percentage.
- Filtered coverage after later handler-test additions was about 61.4%.

Durable coverage priorities:
- Highest-ROI remaining coverage target is still `service/llm_service.go`, a large core file with 0% measured coverage; it requires careful LLM caller mocking to avoid flaky tests.
- Expand existing `workflow_service_test.go` and `telegram_service_test.go` beyond happy paths, especially error, pagination, webhook, and retry paths.
- Add sparse LLM adapter tests for `internal/llm/anthropic`, `internal/llm/openai`, `internal/llm/ollama`, and `internal/llm/workflow`.
- Existing tests are mostly not wrong; the durable issue is narrow breadth, not excessive count. Avoid blanket `t.Parallel()` changes around shared DB setup.

Runtime and validation facts:
- Slowest packages in the 2026-06-07 audit were `internal/handler` at about 58.7s, `internal/service` at about 51.8s, `pkg/openai_client` at about 25.5s, and `pkg/anthropic_client` at about 18.1s.
- Test cost came from many `time.Sleep` calls, repeated `NewTestContext()`/`NewTestDB()` setup, streaming parser/chunk reassembly tests, and DB/git/worktree-heavy service tests.
- Fixed-delay waits in several handler tests were replaced with readiness polling or `require.Eventually`; some timing-sensitive tests gained `testing.Short()` guards.
- `pkg/openai_client` and `pkg/anthropic_client` retry logic now have a package-level `clockAfter = time.After` seam so tests can bypass real retry sleeps without changing production behavior.
- `TestRealtimeDiffUpdates` and several worker dispatch/project-limit tests have `testing.Short()` guards because they intentionally use sleep-based goroutine/ticker timing.
- A shared handler `TestMain` DB was deliberately not implemented because many handler tests mutate/query global/default/list state; `NewTestDB` already caches migrations and a shared DB would need transaction rollback isolation to be safe.
- Raw full-suite runs with `go test ./... -count=1 -timeout 60s` or `go test ./internal/... -count=1 -timeout 60s` can fail from `internal/handler` timing out under load rather than from assertion failures. Project Makefile targets use a 120s timeout, so prefer `make test`, `make test-cover`, or `go test ./... -count=1 -timeout 120s` for authoritative full validation.
- Handler `NewTestContext` now calls `h.SetLocalRepoPathEnabled(true)`, matching the older `setupHandlerTest` default. Project-handler tests that create local-source projects rely on this; tests needing local paths disabled should explicitly call `tc.handler.SetLocalRepoPathEnabled(false)` after creating the context.
