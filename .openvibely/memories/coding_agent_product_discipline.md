---
name: coding_agent_product_discipline
type: feedback
created: 2026-05-11
updated: 2026-06-24
source: consolidation
source_id: memory_consolidation_2026_06_22
confidence: high
title: Coding Agent Product Discipline
---

This memory stores durable user preferences and product-discipline decisions for coding agents working on OpenVibely. Full execution runbooks belong in project skills.

User interaction preferences:
- For design, behavior, or feasibility questions, answer directly without making implementation changes unless explicitly requested.
- If a prior response made an unsolicited code change, acknowledge it and revert or ask before proceeding.
- Prefer plain, direct explanations over jargon-heavy phrasing.
- Bug explanations should include the causal chain, concrete failure mode, and exact affected path when the user asks for detail.
- Summaries should be concrete: cite specific files, symbols, handlers, tests, behavior affected, verification performed, and whether a real git diff exists when that context is available.
- If a user challenges why there is no diff after a claimed coding change, inspect branch pointers, status, reflog, and file contents, then plainly correct any prior summary that was based on non-persisted or stale execution output.
- Broad reviews should actively look for mistakes, unintended diff, dead code, and verification gaps.
- If repeated reviews find one issue at a time, the user may request audit-only mode: return a consolidated ranked problem list before making fixes. Edits during audit-only review turn it into implementation/fix work.
- For Goal Agent behavior, preserve the generic model-evaluator design and avoid deterministic or objective-keyword completion logic; detailed implementation boundaries live in `agent_lifecycle_and_skills.md`.
- When multiple findings are variants of one bug class, fix or audit the whole analogous class instead of narrowly addressing one instance.
- Draft reusable skills that the user has not approved for publication should stay local/ignored by default. If the user wants in-app testing before release, keep the skill indexed and ensure the package body exists in the checkout the app loads; do not remove the index as a hiding mechanism.

Model-facing prompt preferences:
- Use direct role/capability wording over low-value internal/product labels unless the label affects authorization, routing, or correctness.
- Avoid backend provenance/category labels that do not help the LLM, including “generated skill(s),” “protected/system agents,” “non-protected agents,” and “manually assigned agent.” Prefer behavior terms like “standalone skill(s),” “protected agents,” “user-managed agents,” and “assigned agent.”
- For protected or scheduled agents, `System:` may remain in storage/UI names when it is a real identity, but model-facing prompt bodies and hook inputs should avoid `System:` headings or prefixes unless they affect behavior.
- Do not inject the product/project name into prompts merely to make them sound project-specific.
- Prefer long model prompts as readable const templates with dynamic context interpolated, rather than chains of `WriteString` calls.
- Reusable skills/runbooks should avoid naming specific current-release features as examples; encode generic decision rules and feature-neutral examples instead.

Documentation, logging, and validation preferences:
- Preserve useful README content and commented multi-line command examples unless there is a specific reason to trim them.
- Preserve liked README/docs structure while folding in stronger positioning/selling points.
- Keep root `docs/` in sync with README/docs-site positioning when overlapping product concepts change.
- When syncing in-repo docs with the docs website repo, audit recent docs-site content changes and propagate overlapping product-concept updates across affected guides, not only a narrow README/environment pass.
- Root README should stay succinct and high-level, point to `https://docs.openvibely.ai` plus the docs source repo at `/Users/dubee/go/src/github.com/openvibely/openvibely-doc`, and keep detailed environment-variable reference in `docs/environment.md`.
- Published docs links in README/project-facing docs should use new-tab HTML anchors where supported; local relative links stay normal Markdown.
- Very high-frequency or low-value debug traces should be commented `applog.Debugf` examples instead of active gated calls when method-call overhead could accumulate, especially per-LLM-chunk, per-SSE-delta, per-HTMX-poll, per-diff-broadcast-tick, or per-action-routing-check logs.
- Full validation should prefer project Makefile targets or `go test ./... -count=1 -timeout 120s`; detailed test-state and timeout caveats live in `testing_coverage_and_performance.md`.
- Release workflow must include a documentation update pass for new or meaningfully changed features before publishing/tagging; keep in-repo `docs/*.md` and `/Users/dubee/go/src/github.com/openvibely/openvibely-doc` aligned when overlapping concepts change.
- Release agents should install missing required local tools such as `gh` when feasible instead of treating them as immediate blockers; hand back only if installation/authentication fails or requires unavailable credentials/permissions.
- Docker image publishing remains documented as a manual/pending release step unless explicit Docker credentials/tooling are present.

Release-note preferences:
- Release notes are AI-synthesized from structured unreleased commit context because commit subjects are often terse.
- `Highlights` must summarize what is new in the target release from the changelog/commit range, not repeat static product feature bullets from previous releases; `What's Changed` is the detailed changelog section.
- Describe user-facing capability by what it does, not by incidental UI controls or generic labels.
- For the 0.2.0 skill work, describe the Skill Curator as a recursive self-improvement loop that creates/patches skills from task learnings, not as a generic “Skills overhaul.”
- Do not call out minor model-selector additions as standalone release-note features unless the user explicitly asks or the model integration is itself a major release theme.
- High-level release notes should omit CI/test infrastructure, terminal log verbosity, low-level bug/reliability patches, and minor UI polish unless the audience explicitly needs those details or the fix affects a core workflow.

Current release boundary:
- Latest public release boundary is `v0.3.0`, published on 2026-06-24 at `https://github.com/openvibely/openvibely/releases/tag/v0.3.0`.
- The canonical `v0.3.0` annotated tag object is `0b46d34` and targets commit `bfcc37d` (`Document schedule pause and resume behavior`); `main`/`upstream/main` also pointed at `bfcc37d` after release.
- The `v0.3.0` public range was `v0.2.0..bfcc37d` with 161 commits. Major release themes were OpenAI-compatible provider presets/discovery/usage, safer queued task follow-ups across web/Slack/Telegram and worker-capacity races, skill analytics, scheduled-task pause/resume/lifecycle controls, mobile/responsive task and chat UX, secret-key handling, and worktree/rebase workflow improvements.
- The `v0.3.0` GitHub release published eight assets: two macOS desktop `.app.zip` bundles, four darwin/linux server tarballs, one Windows server zip, and `SHA256SUMS`. No Windows desktop-cli asset was published because `mingw-w64` / `x86_64-w64-mingw32-gcc` was unavailable on the release host.
- Docker image `openvibely/openvibely:0.3.0` was not published during the GitHub release because Docker Hub credentials were unavailable; the release body marks Docker publishing as pending.
- Release readiness for `v0.3.0` included a docs commit, `bfcc37d`, updating `docs/schedule-user-guide.md` to document schedule pause/resume behavior.
- Previous release boundary `v0.2.0` was published on 2026-06-07. Its canonical tag was force-moved to commit `13189db` after publication so the macOS app-bundle zip fix was included; live `v0.2.0` macOS zips extract to `OpenVibely.app/`.
- Known release-build pitfalls: `release-build.sh` dry-run mode can still leak macOS bundle filesystem operations outside the dry-run wrapper; Windows desktop-cli release artifacts require `mingw-w64`/`x86_64-w64-mingw32-gcc`; macOS desktop release zips must preserve the app bundle directory name exactly as `OpenVibely.app`; running release builds from git worktrees under or near OpenVibely-managed worktree cleanup paths can be unsafe because test/runtime cleanup may remove them, so use an isolated plain clone or otherwise protected checkout for real release builds.
- When running `release-build.sh` step-by-step, prefer the script default or an absolute dist directory; a relative dist path can break packaging after the script changes into staging directories.

Operational guidance belongs in project skills such as `openvibely_project_guidance`, `openvibely_validation_workflow`, `openvibely_release_workflow`, `openvibely_docs_editing_workflow`, `openvibely_go_maintenance_workflow`, and `openvibely_audit_review_workflow`.
