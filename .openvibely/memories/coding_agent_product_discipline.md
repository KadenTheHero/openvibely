---
name: coding_agent_product_discipline
type: feedback
created: 2026-05-11
updated: 2026-06-08
source: consolidation
source_id: memory_consolidation_2026_06_08
confidence: high
title: Coding Agent Product Discipline
---

This memory stores durable user preferences and product-discipline decisions for coding agents working on OpenVibely. Full execution runbooks belong in project skills.

User interaction preferences:
- For design, behavior, or feasibility questions, answer directly without making implementation changes unless explicitly requested.
- If a prior response made an unsolicited code change, acknowledge it and revert or ask before proceeding.
- Prefer plain, direct explanations over jargon-heavy phrasing.
- Bug explanations should include the causal chain, concrete failure mode, and exact affected path when the user asks for detail.
- Summaries should be concrete: cite specific files, symbols, handlers, tests, behavior affected, and verification performed when that context is available.
- Broad reviews should actively look for mistakes, unintended diff, dead code, and verification gaps.
- If repeated reviews find one issue at a time, the user may request audit-only mode: return a consolidated ranked problem list before making fixes.
- When multiple findings are variants of one bug class, fix or audit the whole analogous class instead of narrowly addressing one instance.

Model-facing prompt preferences:
- Use direct role/capability wording over low-value internal/product labels unless the label affects authorization, routing, or correctness.
- Avoid backend provenance/category labels that do not help the LLM, including “generated skill(s),” “protected/system agents,” “non-protected agents,” and “manually assigned agent.” Prefer behavior terms like “standalone skill(s),” “protected agents,” “user-managed agents,” and “assigned agent.”
- For protected or scheduled agents, `System:` may remain in storage/UI names when it is a real identity, but model-facing prompt bodies and hook inputs should avoid `System:` headings or prefixes unless they affect behavior.
- Do not inject the product/project name into prompts merely to make them sound project-specific.
- Prefer long model prompts as readable const templates with dynamic context interpolated, rather than chains of `WriteString` calls.
- Reusable skills/runbooks should avoid naming specific current-release features as examples; encode generic decision rules and feature-neutral examples instead.

Logging preference:
- Very high-frequency or low-value debug traces should be commented `applog.Debugf` examples instead of active gated calls when method-call overhead could accumulate.
- This applies especially to per-LLM-chunk, per-SSE-delta, per-HTMX-poll, per-diff-broadcast-tick, or per-action-routing-check logs.

Documentation preferences:
- Preserve useful README content and commented multi-line command examples unless there is a specific reason to trim them.
- Preserve liked README/docs structure while folding in stronger positioning/selling points.
- Keep root `docs/` in sync with README/docs-site positioning when overlapping product concepts change.
- When syncing in-repo docs with the docs website repo, audit recent docs-site content changes and propagate overlapping product-concept updates into the in-repo guides; the user expects more than a narrow README/environment pass when the docs-site changed agents, chat, lifecycle, models, skills, workers, tasks, or analytics content.
- Root README should stay succinct and high-level, point to `https://docs.openvibely.ai` plus the docs source repo at `/Users/dubee/go/src/github.com/openvibely/openvibely-doc`, and keep detailed environment-variable reference in `docs/environment.md`.
- Published docs links in README/project-facing docs should use new-tab HTML anchors where supported; local relative links stay normal Markdown.

Maintenance and validation facts:
- The authoritative full Go test-suite command is `go test ./... -count=1 -timeout 60s`, but project Makefile targets use a 120s timeout and are safer for full validation because `internal/handler` can exceed 60s under load.
- OpenVibely has project-scoped release automation, docs editing, validation, Go maintenance, review, and audit skills indexed in `.openvibely/skills/SKILLS.md`.
- Release workflow must include a documentation update pass for new or meaningfully changed features before publishing/tagging; keep in-repo `docs/*.md` and `/Users/dubee/go/src/github.com/openvibely/openvibely-doc` aligned when overlapping product concepts change.
- Release agents should install missing required local tools such as `gh` when feasible instead of treating them as immediate user blockers; only hand back if installation/authentication fails or requires unavailable credentials/permissions.
- Docker image publishing remains documented as a manual/pending release step unless explicit Docker credentials/tooling are present.

Release-note preferences:
- Release notes are AI-synthesized from structured unreleased commit context because commit subjects are often terse.
- `Highlights` must summarize what is new in the target release from the changelog/commit range, not repeat static product feature bullets from previous releases; `What's Changed` is the detailed changelog section.
- Describe user-facing capability by what it does, not by incidental UI controls or generic labels.
- For the 0.2.0 skill work, describe the Skill Curator as a recursive self-improvement loop that creates/patches skills from task learnings, not as a generic “Skills overhaul.”
- Do not call out minor model-selector additions as standalone release-note features unless the user explicitly asks or the model integration is itself a major release theme.
- High-level release notes should omit CI/test infrastructure, terminal log verbosity, low-level bug/reliability patches, and minor UI polish unless the audience explicitly needs those details or the fix affects a core workflow.

Current release boundary:
- Latest public release boundary is `v0.2.0`, published on 2026-06-07.
- The canonical `v0.2.0` tag was force-moved to commit `13189db` after publication so the macOS app-bundle zip fix is included.
- The GitHub release is `https://github.com/openvibely/openvibely/releases/tag/v0.2.0`; live macOS zips now extract to `OpenVibely.app/`.
- The temporary `release/v0.2.0` branch was deleted after the durable release-note skill update was cherry-picked to `main`.
- Schema migrations added only after `v0.2.0` and not yet pushed publicly can be consolidated before release; this is distinct from runtime/local database-file moves such as `openvibely.db` storage migration.

Known release-build pitfalls as of 2026-06-07:
- `release-build.sh` dry-run mode still leaks macOS desktop bundle filesystem operations (`cp`/`chmod`) outside the dry-run wrapper, causing misleading dry-run errors while not blocking a real release.
- `mingw-w64`/`x86_64-w64-mingw32-gcc` was not installed on the local release host during the 0.2.0 release dry run, so the Windows desktop-cli artifact would be skipped unless that toolchain is installed.
- MacOS desktop release zips must preserve the app bundle directory name exactly as `OpenVibely.app`; packaging architecture details live in `openvibely_architecture.md`.

Operational guidance for release execution, release notes, docs editing, validation, Go maintenance, review workflows, and repo-specific coding practice belongs in the matching project skills under `.openvibely/skills/`, especially `openvibely_project_guidance`, `openvibely_validation_workflow`, `openvibely_release_workflow`, `openvibely_docs_editing_workflow`, `openvibely_go_maintenance_workflow`, and `openvibely_audit_review_workflow`.
