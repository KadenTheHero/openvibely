---
name: managed_memory
type: project
created: 2026-05-09
updated: 2026-05-25
source: consolidation
source_id: memory_consolidation_2026_05_23
confidence: high
title: Managed Memory
---

OpenVibely managed memory is model-backed, tool-driven, project-scoped, and stored under the selected project's repo-local `.openvibely/memory/` directory. The directory is flat: `MEMORY.md` is a compact index and top-level focused topic markdown files hold durable context. Do not recreate old `user/`, `feedback/`, `project/`, or `runs/` subdirectories.

When using scoped memory file tools, the tool root may already be `.openvibely/memory`; if `.openvibely/memory` appears missing, list `.` before assuming memory is absent or creating nested paths.

Do not use repo-root `MEMORY.md` as active task/chat memory. Durable architecture decisions, user preferences, implementation feedback, repeated pitfalls, and task/chat lessons should go into OpenVibely managed memory through `MemoryService`. If old branches contain useful root-memory content, migrate only the durable parts into focused `.openvibely/memory/*.md` files instead of restoring a root compatibility pointer. Keep `AGENTS.md`, `guardrails.md`, and `PRACTICES.md` as static operating instructions, not feature-history or task-summary stores.

Memory operations require a configured selected project with a valid local `repo_path`. Do not use an app-owned fallback such as `memory/projects/<project_id>/` or `OPENVIBELY_MEMORY_ROOT`. Each repository decides whether to check in `.openvibely/memory/*.md` or ignore it.

Managed memory initialization should be idempotent in both web/server and Wails desktop modes. For a selected project with a valid local `repo_path`, startup/runtime paths may create `.openvibely/memory/` and `MEMORY.md` when missing; topic files should be created only when explicitly written or safely skipped when absent. New topic files are indexed by the model/tool memory workflow when deemed durable and index-worthy, not automatically by low-level file creation.

`MemoryService` owns path resolution, file storage, context building, extraction, consolidation, and DB metadata. Memory features should use existing SQLite task/chat execution history as transcript source; do not add JSONL transcript storage or migration scaffolding for unreleased local-only layouts unless explicitly requested.

When implementing lifecycle/runbook work, do not add separate built-in memory-maintainer agents, `memory_maintainer` system-kind constants, memory-maintainer skills such as `create_memory`, `recall_memory`, or `consolidate_memory`, or seed migrations for that separate memory-maintainer path unless explicitly requested. OpenVibely already has built-in managed memory; runbook memory portions have been treated as out of scope.

Filesystem access for managed memory tools must be scoped to the memory directory. Tool executors must reject traversal, absolute paths, and symlink escapes. Keep generic scoped-file code focused on hard capability boundaries; memory-specific policy belongs in prompts/config, not generic tool logic.

Extraction and consolidation should save distilled reusable guidance, decisions, pitfalls, product direction, workflow constraints, and user preferences. Do not store raw complaint text, assistant-outcome boilerplate, one-off test prompts, transient logs, raw transcripts, secrets, provider-internal product terminology, task-by-task summaries, Chat page prompts, Orchestrate/Plan mode-control text, or procedural how-to content better represented as an agent/skill.

Prompt wording for managed-memory storage boundaries should stay abstract and product-neutral: describe durable contextual notes versus operational runbooks/procedures without repeatedly naming or contrasting internal subsystems such as "memory" and "skills".

The model owns memory extraction/consolidation decisions. Go code should not replace that with hardcoded rule extractors, deterministic topic classifiers, or JSON proposal workflows when provider tool execution is available. Use neutral OpenVibely-owned product terminology such as model/tool-driven managed memory.

Normal task, thread, and chat executions consume selected managed memory as injected read-only prompt context after runtime model-backed retrieval/selection and synthesis. Do not inject all memory files by default, and do not rely on the main coding model opening `MEMORY.md` with file tools. The recall flow should use `MEMORY.md` as an index, select topic filenames, then synthesize brief assistant-facing context from selected files; the synthesized injection should be directly actionable rather than relying on unresolved filename citations.

Direct managed-memory write tools should usually be reserved for memory-specific extraction/consolidation passes triggered by completion or scheduled runs. Scoped memory file tools are for flows explicitly updating the managed memory directory, not global provider behavior.

Memory Consolidation should run as a normal scheduled task assigned to the built-in Memory Consolidator agent. Each project should get a real system-created scheduled task, visible on the Schedule page like other scheduled tasks, while remaining hidden from the normal Tasks board. Avoid hidden-run behavior, separate memory-specific schedule cards, parallel schedule tables, bespoke schedule status/run-history paths, or prompt-display special cases when existing scheduler and task-thread features can represent the behavior.

Bootstrap may create or repair the Memory Consolidator agent and schedule, but runtime execution should use the generic scheduled-agent path. `agents.system_kind = "memory_consolidator"` may remain for bootstrap/UI identity, but it must not trigger special prompt swapping, special tool grants, or post-run hooks in `LLMService`.

The Memory Consolidator should get filesystem access through generic `ScopedFiles` tool config: directory `.openvibely/memory`, read/write/delete permissions, and default tools skipped. Use normal task/execution status, output, error, and alert mechanisms for scheduled consolidation results.

Troubleshooting: when a completed chat/task produces no new memory file, first inspect the memory run tables. If `memory_extraction_runs` has no row for the interaction, the completion hook did not reach `RecordInteraction`; this differs from a successful extraction that decided there was nothing durable to save.
