---
name: coding_agent_product_discipline
type: feedback
created: 2026-05-11
updated: 2026-05-30
source: consolidation
source_id: memory_consolidation_2026_05_30
confidence: high
title: Coding Agent Product Discipline
---

When the user asks a design, behavior, or feasibility question, answer directly and do not make implementation changes unless explicitly requested. If a prior response made an unsolicited code change, acknowledge it and revert or ask before proceeding.

Implementation discipline:
- When the user asks to implement an entire runbook/spec except an explicitly excluded area, treat every non-excluded section as in scope. Do not defer in-scope pieces without naming the blocker and getting confirmation; re-read the source spec and audit existing implementation before declaring work complete.
- When implementing product behavior, identify the underlying product concept before coding. Do not derive major behavior from incidental implementation shape such as tool lists, default flags, or temporary code structure; model product policy explicitly in configuration, state, or data model when it affects workflow, isolation, data writes, recovery, or review.
- Avoid making generic capabilities carry hidden special-case behavior for one built-in use case. If one built-in agent or workflow needs exceptional behavior, express it through that agent/workflow's explicit configuration while keeping the generic feature predictable.
- For generic environment/path discovery, derive values from authoritative user or system sources instead of hardcoding guessed locations for specific tools or package managers. Tool/runtime PATH loading should stay generic, user-derived, and free of special-case install paths unless explicitly configured.
- Treat similarly named roots as distinct concepts. Before resolving relative paths or write locations, determine whether the intended root is the project root, isolated worktree root, process working directory, durable repo location, running app data root, or tool scope root. Preserve that meaning through storage and runtime resolution, and do not describe unmerged branch behavior as if it applies to the user's currently running app.
- Do not defer configuration correctness to runtime failures. Users should not be able to save invalid settings that will predictably fail later when the invalid state can be detected at configuration time.
- Do not add legacy compatibility, aliases, migrations, or helper wrappers for bad abstractions unless there is actual shipped or persisted legacy state to support. Intermediate implementation attempts during the same feature are not legacy product states; fix the abstraction instead.
- Prefer product-correct defaults over mechanically convenient defaults. For UI and workflow changes, optimize for clarity, reversibility, user intent, and useful behavior rather than merely making tests pass.
- Long model prompts should be readable const templates with dynamic context interpolated, not assembled through chains of `WriteString` calls.

Communication and review behavior:
- When drafting reusable coding-agent context or instructions, favor generalized patterns and broad behavioral rules over narrow examples. The user values useful improvisation, constrained by explicit assumption discipline rather than unsupported product or architecture guesses.
- When editing project-facing documentation, do not over-trim useful README content or remove commented multi-line command examples without a specific reason; the user values those comments because GitHub renders copy buttons for fenced blocks. Prefer targeted enhancements that preserve liked structure and fold in stronger positioning/selling points from source docs. Keep the root `docs/` directory in sync with README/docs-site positioning when documentation changes touch overlapping product concepts. The root README should stay succinct and high-level, point readers to `https://docs.openvibely.ai` plus the docs source repo at `/Users/dubee/go/src/github.com/openvibely/openvibely-doc`, and keep detailed environment-variable reference in `docs/environment.md`. Consolidate duplicate README value/feature sections into a stronger table when possible, and avoid explaining helper-script internals such as `./start.sh` unless specifically requested. For published docs links in README/project-facing docs, prefer markup that requests opening in a new tab (`target="_blank"` with `rel="noopener noreferrer"`) where the renderer supports it, while keeping local relative links as normal Markdown.
- When summarizing code changes or implementation results, be concrete: cite specific files, symbols/handlers/tests changed, behavior affected, and verification performed when that context is available.
- When the user asks for a broad review before completion, actively look for mistakes, unintended diff, dead code, and verification gaps. Check the diff plus relevant build/test/vet/static analysis where practical, and report tool limitations rather than implying unsupported confidence.
- When the user challenges whether prior changes are beneficial, audit each change or commit individually against current code behavior, not only by commit message. Separate unrelated baseline/merge commits from the review, classify changes as keep, partial, redundant, or risky, and recommend cleanup instead of defending all accumulated work.
- When cleaning up an overgrown or suspicious task branch, reduce it to the minimal root-cause fix rather than preserving accumulated incidental changes. Before destructive reset/checkout/rebase, capture or verify the intended patch; after cleanup, review the final diff for bugs, dead code, and unintended behavior before reporting completion.

Maintenance expectations:
- For scheduled Go maintenance tasks, inspect all Go version references (`go.mod`, `toolchain`, Dockerfiles, CI workflows, build scripts, docs, and version-manager files), update consistently only when a newer stable toolchain is available, check compatible Go module upgrades, run `go mod tidy`, verify with `go test -count=1 ./...` plus the project's standard build checks, and fix update-caused failures rather than masking them. Summaries should distinguish updated items, already-current items, and follow-up risks/manual steps.
- When maintaining development tooling commands, avoid duplicating tool versions across scripts and Dockerfiles. Prefer deriving versions from authoritative module metadata such as `go.mod` when practical; `templ` and `swag` previously had drift risk because Dockerfile/Makefile installs could hardcode versions while other scripts resolved from `go.mod`.
- The last public release boundary the user identified was commit `d654a068ee0b4f146bb9e51dd536728eab49a150`. Schema migrations added only after that boundary and not yet pushed publicly can be consolidated before release instead of preserving every local goose iteration; distinguish this from runtime/local database-file moves such as `openvibely.db` storage migration.
