---
name: managed_memory
type: project
created: 2026-05-09
updated: 2026-05-14
source: consolidation
source_id: memory_consolidation_2026_05_14
confidence: high
title: Managed Memory
---

OpenVibely managed memory is model-backed, tool-driven, project-scoped, and stored under the selected project's repo-local `.openvibely/memory/` directory. The directory is flat: `MEMORY.md` is a compact index and top-level focused topic markdown files hold durable context. Do not recreate `user/`, `feedback/`, `project/`, or `runs/` subdirectories.

Do not use repo-root `MEMORY.md` as active task/chat memory. Durable architecture decisions, user preferences, implementation feedback, repeated pitfalls, and task/chat lessons should go into OpenVibely managed memory through `MemoryService`. Repo-root `MEMORY.md` has been removed; do not recreate a root compatibility pointer. If old branches contain durable architecture or workflow content in root `MEMORY.md`, migrate the durable parts into focused `.openvibely/memory/*.md` files instead of restoring the root file. Keep `AGENTS.md`, `guardrails.md`, and `PRACTICES.md` as static operating instructions, not feature-history or task-summary stores.

Memory operations require a configured selected project with a valid local `repo_path`. Do not use an app-owned fallback such as `memory/projects/<project_id>/` or `OPENVIBELY_MEMORY_ROOT`. Each repository decides whether to check in `.openvibely/memory/*.md` or ignore it.

`MemoryService` wraps path resolution, file storage, context building, extraction, consolidation, and DB metadata. Memory features should use OpenVibely's existing SQLite task/chat execution history as the transcript source; do not add JSONL transcript storage or migration scaffolding for unreleased local-only layouts unless explicitly requested.

Filesystem access must be scoped to the managed memory directory. Tool executors must reject traversal, absolute paths, and symlink escapes. Tests should focus on this sandbox boundary and small helpers, not on pretending Go heuristics perform consolidation.

Extraction and consolidation should save distilled reusable guidance, decisions, pitfalls, product direction, workflow constraints, and user preferences. Do not store raw complaint text, assistant-outcome boilerplate, one-off test prompts, transient logs, raw transcripts, secrets, provider-internal product terminology, or task-by-task summaries.

The model owns memory extraction/consolidation decisions. Go code should not replace that with hardcoded rule extractors, deterministic topic classifiers, or JSON proposal workflows when provider tool execution is available. Use neutral OpenVibely-owned product terminology such as model/tool-driven managed memory.

Normal task, thread, and chat executions consume selected managed memory as injected read-only prompt context after runtime model-backed retrieval/selection and synthesis. Do not inject all memory files by default, and do not rely on the main coding model opening `MEMORY.md` with file tools.

As of the 2026-05-09 recall implementation, normal memory recall uses two direct LLM calls before the main task/chat call: a selection call that asks for JSON filenames from the memory index/manifest, then a synthesis call that condenses selected topic files into brief assistant-facing context. `MEMORY.md` is the index and should not be returned as a selected memory file. The synthesized injection should include actionable context directly rather than relying on unresolved trailing filename citations. These calls use the project/default memory agent/provider via `CallAgentDirect` without runtime memory tools attached; verify current code before relying on exact parameters.

Direct managed-memory write tools should usually be reserved for memory-specific extraction/consolidation passes triggered by completion or scheduled runs. Scoped memory file tools are for flows explicitly updating the managed memory directory, not global provider behavior.

Memory Consolidation should run as a normal scheduled task assigned to the built-in Memory Consolidator agent. Each project should get a real system-created scheduled task, visible on the Schedule page like other scheduled tasks, while remaining hidden from the normal Tasks board. Avoid hidden-run behavior, separate memory-specific schedule cards, parallel schedule tables, bespoke schedule status/run-history paths, or prompt-display special cases when existing scheduler and task-thread features can represent the behavior.

Bootstrap may create or repair the Memory Consolidator agent and schedule, but runtime execution should use the generic scheduled-agent path. `agents.system_kind = "memory_consolidator"` may remain for bootstrap/UI identity, but it must not trigger special prompt swapping, special tool grants, or post-run hooks in `LLMService`.

The durable consolidation task is identified by the system Memory Consolidator agent/profile and the system task title, then uses a normal `schedules` row. Its prompt and agent system prompt should instruct the agent to list the scoped memory directory, read `MEMORY.md`, inspect relevant topic files, merge/prune/split/rewrite durable memory, maintain index references, avoid transient logs/secrets/raw transcripts, and summarize changes.

The Memory Consolidator should get filesystem access through generic `ScopedFiles` tool config, not a memory-specific tool/profile: directory `.openvibely/memory`, read/write/delete permissions, and default tools skipped. Use normal task/execution status, output, error, and alert mechanisms for scheduled consolidation results. Do not add a separate scheduled-consolidation runtime branch or duplicate status/error/notes/touched-path metadata unless a clear product/UI reason appears later.

The scoped file executor should stay generic: enforce only hard capability boundaries such as project-relative scope resolution, explicit read/write/delete permissions, absolute-path rejection, traversal rejection, and symlink escape rejection. Memory-specific policy belongs in prompts/config, not in generic tool code.

Troubleshooting: when a completed chat/task produces no new memory file, first inspect the memory run tables. If `memory_extraction_runs` has no row for the interaction, the completion hook did not reach `RecordInteraction`; this differs from a successful extraction that decided there was nothing durable to save.
