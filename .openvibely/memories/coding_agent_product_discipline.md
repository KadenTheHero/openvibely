---
name: coding_agent_product_discipline
type: feedback
created: 2026-05-11
updated: 2026-06-07
source: consolidation
source_id: memory_consolidation_2026_06_07
confidence: high
title: Coding Agent Product Discipline
---

This memory stores durable user preferences and product-discipline decisions for coding agents working on OpenVibely. Full execution runbooks belong in project skills.

User interaction preferences:
- For design, behavior, or feasibility questions, the user expects a direct answer without implementation changes unless explicitly requested.
- If a prior response made an unsolicited code change, acknowledge it and revert or ask before proceeding.
- The user prefers plain, direct explanations over jargon-heavy phrasing.
- When the user asks to explain a bug in detail, provide the full causal chain, concrete failure mode, and exact affected path rather than a terse summary.
- Summaries are expected to be concrete: cite specific files, symbols, handlers, tests, behavior affected, and verification performed when that context is available.
- When the user asks for a broad review before completion, actively look for mistakes, unintended diff, dead code, and verification gaps.
- If repeated reviews keep finding one issue at a time, the user may ask for audit-only mode: return a consolidated ranked problem list before making fixes.

Model-facing prompt preferences:
- The user prefers direct role/capability wording over low-value internal/product labels such as “built-in system agent,” “system agent configuration,” and “non-system agent,” unless the label affects authorization, routing, or correctness.
- In model-facing prompt/tool text, avoid backend provenance/category labels that do not help the LLM, including “generated skill(s),” “protected/system agents,” “non-protected agents,” and “manually assigned agent.” Prefer behavior terms like “standalone skill(s),” “protected agents,” “user-managed agents,” and “assigned agent.”
- The user dislikes injecting the product/project name into prompts merely to make them sound project-specific.
- The user prefers long model prompts as readable const templates with dynamic context interpolated, rather than chains of `WriteString` calls.

Logging preference:
- The user prefers very high-frequency or low-value debug traces to be left as commented `applog.Debugf` examples instead of active gated calls when method-call overhead could accumulate.
- This especially applies to per-LLM-chunk, per-SSE-delta, per-HTMX-poll, per-diff-broadcast-tick, or per-action-routing-check logs.

Documentation preferences:
- Useful README content and commented multi-line command examples are valuable to preserve unless there is a specific reason to trim them.
- The user values preserving liked README/docs structure while folding in stronger positioning/selling points.
- Keep root `docs/` in sync with README/docs-site positioning when overlapping product concepts change.
- Root README is expected to stay succinct and high-level, point to `https://docs.openvibely.ai` plus the docs source repo at `/Users/dubee/go/src/github.com/openvibely/openvibely-doc`, and keep detailed environment-variable reference in `docs/environment.md`.
- For published docs links in README/project-facing docs, new-tab HTML anchors are preferred where supported; local relative links stay as normal Markdown.

Maintenance and release facts:
- The authoritative full Go test-suite command is `go test ./... -count=1 -timeout 60s`; narrower `./internal/...` runs are validation subsets.
- The last public release boundary the user identified was commit `d654a068ee0b4f146bb9e51dd536728eab49a150`.
- Schema migrations added only after that boundary and not yet pushed publicly can be consolidated before release; this is distinct from runtime/local database-file moves such as `openvibely.db` storage migration.
- OpenVibely has project-scoped release automation skills indexed in `.openvibely/skills/SKILLS.md`.
- Release notes are AI-synthesized from structured unreleased commit context because commit subjects are often terse.
- Docker image publishing remains documented as a manual/pending release step unless explicit Docker credentials/tooling are present.
- As of 2026-06-07, the release workflow's prior Windows desktop-cli blocker was fixed: `.openvibely/skills/openvibely_release_workflow/scripts/release-build.sh` no longer uses `local` at top level in the `WINDOWS_DESKTOP_OK=1` block.

Operational guidance for release execution, release notes, docs editing, validation, Go maintenance, review workflows, and repo-specific coding practice belongs in the matching project skills under `.openvibely/skills/`, especially `openvibely_project_guidance`, `openvibely_validation_workflow`, `openvibely_release_workflow`, `openvibely_docs_editing_workflow`, `openvibely_go_maintenance_workflow`, and `openvibely_audit_review_workflow`.
