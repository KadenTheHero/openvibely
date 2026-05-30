---
name: managed_memory
type: project
created: 2026-05-09
updated: 2026-05-30
source: after_complete
source_id: 55b32f4969c8f131654ec6e6dd4e607f
confidence: high
title: Managed Memory
---

OpenVibely managed memory is model-backed, tool-driven, project-scoped, and stored under the selected project's repo-local `.openvibely/memories/` directory. The directory is flat: `MEMORIES.md` is the compact index and top-level focused topic markdown files hold durable context. Do not recreate old `user/`, `feedback/`, `project/`, or `runs/` subdirectories.

Storage and access boundaries:
- When using scoped memory file tools, the tool root may already be `.openvibely/memories`; if `.openvibely/memories` appears missing, list `.` before assuming memory is absent or creating nested paths.
- Memory operations require a configured selected project with a valid local `repo_path`. Do not use an app-owned fallback such as `memory/projects/<project_id>/` or `OPENVIBELY_MEMORY_ROOT`.
- `MemoryService` owns path resolution, file storage, context building, extraction, consolidation, and DB metadata. Use existing SQLite task/chat execution history as transcript source; do not add JSONL transcript storage or migration scaffolding for unreleased local-only layouts unless explicitly requested.
- Managed memory initialization should be idempotent in web/server and Wails desktop modes. Startup/runtime paths may create `.openvibely/memories/` and `MEMORIES.md` when missing; topic files should be created only by explicit durable-memory writes.
- Filesystem access for managed-memory tools must be scoped to the memory directory. Tool executors must reject traversal, absolute paths, and symlink escapes. Keep memory-specific policy in prompts/config rather than generic scoped-file enforcement.

Content boundaries:
- Extraction and consolidation should save distilled reusable guidance, decisions, pitfalls, product direction, workflow constraints, user preferences, current-state facts, and incidents.
- Do not store raw complaint text, assistant boilerplate, one-off test prompts, transient logs, raw transcripts, secrets, provider-internal product terminology, task-by-task summaries, Chat page prompts, Orchestrate/Plan mode-control text, or procedure-only runbooks better represented as agents/skills.
- Prompt wording for storage boundaries should stay abstract and product-neutral: describe durable contextual notes versus operational procedures without repeatedly naming or contrasting internal subsystems.
- Do not use repo-root `MEMORIES.md` as active task/chat memory. Keep `AGENTS.md`, `guardrails.md`, and `PRACTICES.md` as static operating instructions, not feature-history or task-summary stores.

Lifecycle behavior:
- Memory lifecycle work is owned by the built-in system Memory Curator agent through `recall_memory`, `update_memory`, and `consolidate_memory` lifecycle skills. Do not add separate memory-maintainer/consolidator agents, duplicate memory-maintainer skills, or seed migrations for a separate memory-maintainer path unless explicitly requested.
- The model owns memory extraction/consolidation decisions. Go code should not replace that with hardcoded rule extractors, deterministic topic classifiers, or JSON proposal workflows when provider tool execution is available.
- Normal task, thread, and chat executions consume selected managed memory as injected read-only prompt context after runtime model-backed retrieval/selection and synthesis. For interactive chat, recalled memory is model-facing through `ChatSystemContext` rather than appended to the user prompt. Do not inject all memory files by default or rely on the main coding model opening `MEMORIES.md` with file tools.
- Recall should use `MEMORIES.md` as an index, select topic filenames, then synthesize brief assistant-facing context from selected files; the injection should be directly actionable rather than unresolved filename citations.
- The `before_run` `recall_memory` lifecycle output should expose compact selected-memory debug metadata, such as root-relative file/topic identifiers plus brief summaries/snippets, so the Lifecycle tab can show which memories were selected and injected without dumping raw unrelated memory content.
- Direct managed-memory write tools should usually be reserved for memory-specific extraction/consolidation passes triggered by completion or scheduled runs, not global provider behavior.

Scheduled consolidation:
- Memory consolidation should run as a normal scheduled task assigned to the built-in Memory Curator agent with the `consolidate_memory` skill. Each project should get a real system-created scheduled task visible on the Schedule page like other scheduled tasks, while hidden from the normal Tasks board.
- Avoid hidden-run behavior, separate memory-specific schedule cards, parallel schedule tables, bespoke schedule status/run-history paths, prompt-display special cases, or special `LLMService` prompt/tool behavior when existing scheduler and task-thread features can represent the workflow.
- Bootstrap may create or repair the Memory Curator agent and schedule, but runtime execution should use the generic scheduled-agent path and generic `ScopedFiles` tool config for `.openvibely/memories` with read/write/delete permissions and default tools skipped.
- Use normal task/execution status, output, error, and alert mechanisms for scheduled consolidation results.
- Troubleshooting: when a completed chat/task produces no new memory file, first inspect memory run tables. If `memory_extraction_runs` has no row for the interaction, the completion hook did not reach `RecordInteraction`; this differs from a successful extraction that decided there was nothing durable to save.
