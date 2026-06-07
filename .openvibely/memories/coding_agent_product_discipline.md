---
name: coding_agent_product_discipline
type: feedback
created: 2026-05-11
updated: 2026-06-07
source: after_complete
source_id: 3f679b74b33ea6ed3957ea82f8f453f2
confidence: high
title: Coding Agent Product Discipline
---

When the user asks a design, behavior, or feasibility question, answer directly and do not make implementation changes unless explicitly requested. If a prior response made an unsolicited code change, acknowledge it and revert or ask before proceeding.

Implementation discipline:
- When the user asks to implement an entire runbook/spec except an explicitly excluded area, treat every non-excluded section as in scope. Do not defer in-scope pieces without naming the blocker and getting confirmation; re-read the source spec and audit existing implementation before declaring work complete.
- Identify the underlying product concept before coding. Do not derive major behavior from incidental implementation shape such as tool lists, default flags, or temporary code structure; model product policy explicitly in configuration, state, or data model when it affects workflow, isolation, data writes, recovery, or review.
- For cross-layer production changes, include regression coverage for the touched wiring/call-site layer as well as lower-level service behavior.
- When the user reports a consistent behavior bug in a specific UI/API/provider/mode path, reproduce that exact path and verify the final provider-bound request/tool payload before declaring the system works. Do not treat lifecycle DB rows, intermediate context objects, direct helper tests, or adjacent tool/API paths such as `send_to_task` as sufficient proof for task-thread UI follow-up behavior.
- Avoid making generic capabilities carry hidden one-off special cases. If a built-in agent/workflow needs exceptional behavior, express it through explicit configuration while keeping the generic feature predictable.
- Derive environment/path values from authoritative user or system sources instead of hardcoding guessed locations for specific tools or package managers.
- Treat similarly named roots as distinct concepts: project root, isolated worktree root, process working directory, durable repo location, running app data root, and tool scope root are not interchangeable.
- Do not defer configuration correctness to runtime failures when invalid state can be detected at configuration time.
- Do not add legacy compatibility, aliases, migrations, or helper wrappers for bad abstractions unless there is actual shipped or persisted legacy state to support.
- Prefer product-correct defaults over mechanically convenient defaults. Optimize UI/workflow changes for clarity, reversibility, user intent, and useful behavior.
- Long model prompts should be readable const templates with dynamic context interpolated, not assembled through chains of `WriteString` calls.

Communication and review behavior:
- Favor generalized reusable instructions over narrow examples. Treat example names as illustrative unless explicitly defined as product fixtures; do not hardcode them into app-facing behavior or prompts.
- Summaries should be concrete: cite specific files/symbols/handlers/tests changed, behavior affected, and verification performed when that context is available.
- The user prefers plain, direct explanations over jargon-heavy phrasing; explain compactly with concrete examples when clarifying concepts such as stale guards, authorization, or lifecycle behavior. When they ask to explain a bug in detail, do not substitute a terse “short version”; give the full causal chain, concrete failure mode, and exact affected path without padding.
- When the user asks for a broad review before completion, actively look for mistakes, unintended diff, dead code, and verification gaps. Check the diff plus relevant build/test/vet/static analysis where practical, and report tool limitations.
- If repeated reviews keep finding one issue at a time, switch to audit-only mode when asked: stop editing, collect a consolidated ranked list of concrete problems with file/symbol references, and let the user choose what to fix afterward.
- When fixing a repeated class of lifecycle/recovery bugs, harden the entire class in one pass rather than patching one branch at a time.
- When challenged on prior changes, audit each change/commit individually against current behavior, separate unrelated baseline/merge commits, classify changes as keep/partial/redundant/risky, and recommend cleanup instead of defending all accumulated work.
- When cleaning up an overgrown or suspicious task branch, reduce it to the minimal root-cause fix. Before destructive reset/checkout/rebase, capture or verify the intended patch; after cleanup, review the final diff for bugs, dead code, and unintended behavior.

Documentation preferences:
- Do not over-trim useful README content or remove commented multi-line command examples without a specific reason; the user values copyable fenced command blocks.
- Preserve liked README/docs structure while folding in stronger positioning/selling points. Keep root `docs/` in sync with README/docs-site positioning when overlapping product concepts change.
- Root README should stay succinct and high-level, point to `https://docs.openvibely.ai` plus the docs source repo at `/Users/dubee/go/src/github.com/openvibely/openvibely-doc`, and keep detailed environment-variable reference in `docs/environment.md`.
- Consolidate duplicate README value/feature sections into a stronger table when possible, and avoid explaining helper-script internals such as `./start.sh` unless specifically requested.
- For published docs links in README/project-facing docs, prefer markup that opens external links in a new tab (`target="_blank"` with `rel="noopener noreferrer"`) where supported; keep local relative links as normal Markdown.

Maintenance expectations:
- The authoritative full Go test-suite command is `go test ./... -count=1 -timeout 60s`; narrower `./internal/...` runs are validation subsets and should not be described as the full suite.
- For scheduled Go maintenance tasks, inspect all Go version references (`go.mod`, `toolchain`, Dockerfiles, CI workflows, build scripts, docs, version-manager files), update consistently only when a newer stable toolchain is available, check compatible module upgrades, run `go mod tidy`, verify with `go test -count=1 ./...` plus standard build checks, and fix update-caused failures rather than masking them.
- Avoid duplicating tool versions across scripts and Dockerfiles. Prefer deriving versions from authoritative module metadata such as `go.mod` when practical.
- The last public release boundary the user identified was commit `d654a068ee0b4f146bb9e51dd536728eab49a150`. Schema migrations added only after that boundary and not yet pushed publicly can be consolidated before release; distinguish this from runtime/local database-file moves such as `openvibely.db` storage migration.
- OpenVibely now has a project-scoped standalone release automation skill, `openvibely_release_workflow`, indexed in `.openvibely/skills/SKILLS.md` with deterministic scripts under `.openvibely/skills/openvibely_release_workflow/scripts/`. Future release work should prefer that skill/script path for semver normalization, preflight checks, release branch/tag naming, artifact naming matching v0.1.0, checksum generation, and GitHub release creation instead of improvising shell steps.
- OpenVibely release notes should be AI-synthesized from structured unreleased commit context because commit subjects are often terse. Deterministic release scripts should collect raw commit context and render reusable/static release-note structure, but the agent should write the high-level user-facing "What's Changed" summary, group related impactful changes, preserve important technical notes, and omit low-impact internal noise instead of publishing regex buckets or raw commit-message lists.
- The release workflow publishes GitHub releases through both `git` and the GitHub CLI: `git` creates/pushes the `release/v<version>` branch and annotated `v<version>` tag, while `gh` performs auth/release-existence checks, creates the GitHub release, uploads artifacts/checksums, and prints the release URL. Docker image publishing is documented as a manual/pending step unless explicit Docker credentials/tooling are present.
- As of the final post-fix audit for task `3f679b74b33ea6ed3957ea82f8f453f2`, the release workflow's Windows desktop-cli blocker was fixed: `.openvibely/skills/openvibely_release_workflow/scripts/release-build.sh` no longer uses `local` at top level in the `WINDOWS_DESKTOP_OK=1` block. The audit found no material release-workflow bugs or regressions, and release validation reported 50/50 tests passing with all release scripts passing `bash -n`.
