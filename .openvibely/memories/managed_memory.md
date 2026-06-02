---
name: managed_memory
type: project
created: 2026-05-09
updated: 2026-06-02
source: consolidation
source_id: memory_consolidation_2026_06_02
confidence: high
title: Managed Memory
---

OpenVibely managed memory is model-backed, tool-driven, project-scoped, and stored under the selected project's repo-local `.openvibely/memories/` directory. The directory stays flat: `MEMORIES.md` is the compact index and focused top-level topic markdown files hold durable context. Do not recreate old `user/`, `feedback/`, `project/`, or `runs/` subdirectories.

Storage and access boundaries:
- Scoped memory file tools may already be rooted at `.openvibely/memories`; if that path appears missing, list `.` before assuming memory is absent or creating nested paths.
- Memory requires a selected project with a valid local `repo_path`. Do not use app-owned fallbacks such as `memory/projects/<project_id>/` or `OPENVIBELY_MEMORY_ROOT`.
- `MemoryService` owns path resolution, file storage, context building, extraction, consolidation, and DB metadata. Use existing SQLite task/chat execution history as transcript source; do not add JSONL transcript storage or unreleased layout migrations unless explicitly requested.
- Initialization is idempotent in web/server and Wails desktop modes. Startup/runtime paths may create `.openvibely/memories/` and `MEMORIES.md` when missing; topic files should be created only by explicit durable-memory writes.
- Managed-memory tools must be scoped to the memory directory and reject traversal, absolute paths, and symlink escapes. Keep memory-specific policy in prompts/config rather than generic scoped-file enforcement.

Content boundaries:
- Save distilled reusable guidance, decisions, pitfalls, product direction, workflow constraints, user preferences, current-state facts, and incidents.
- Do not store raw complaint text, assistant boilerplate, one-off test prompts, transient logs, raw transcripts, secrets, provider-internal product terminology, task-by-task summaries, Chat page prompts, Orchestrate/Plan mode-control text, or procedure-only runbooks better represented as agents/skills.
- Prompt wording for storage boundaries should stay abstract and product-neutral: describe durable contextual notes versus operational procedures without repeatedly naming or contrasting internal subsystems.
- Do not use repo-root `MEMORIES.md` as active task/chat memory. Keep `AGENTS.md`, `guardrails.md`, and `PRACTICES.md` as static operating instructions, not feature-history or task-summary stores.

Lifecycle behavior:
- Memory lifecycle work is owned by the built-in system Memory Curator agent through `recall_memory`, `update_memory`, and `consolidate_memory` lifecycle skills. Do not add separate memory-maintainer/consolidator agents, duplicate memory-maintainer skills, or seed migrations for a separate path unless explicitly requested.
- The model owns extraction and consolidation decisions. Go code should not replace this with hardcoded rule extractors, deterministic topic classifiers, or JSON proposal workflows when provider tool execution is available.
- Normal task, thread, and chat executions consume selected managed memory as injected read-only prompt context after runtime model-backed retrieval/selection and synthesis. Interactive chat receives recalled memory through `ChatSystemContext`, not by appending all files or filenames to the user prompt.
- Recall should use `MEMORIES.md` as an index, select topic files, then synthesize brief assistant-facing context from those files. The injected context should be directly actionable, not unresolved filename citations.
- The `before_run` `recall_memory` output should expose compact selected-memory debug metadata such as root-relative identifiers plus brief summaries/snippets so the Lifecycle tab can render deduped badge/pill-style memory identifiers without dumping raw unrelated memory content.
- Direct managed-memory write tools should usually be reserved for memory-specific extraction/consolidation passes triggered by completion or scheduled runs, not global provider behavior.

Scheduled consolidation:
- Memory consolidation runs as a normal scheduled task assigned to the built-in Memory Curator agent with the `consolidate_memory` skill. Each project should get a real system-created scheduled task visible on the Schedule page like other scheduled tasks, while hidden from the normal Tasks board.
- Avoid hidden-run behavior, separate memory-specific schedule cards, parallel schedule tables, bespoke schedule status/history paths, prompt-display special cases, or special `LLMService` prompt/tool behavior when existing scheduler and task-thread features can represent the workflow.
- Bootstrap may create or repair the Memory Curator agent and schedule, but runtime execution should use the generic scheduled-agent path and generic `ScopedFiles` tool config for `.openvibely/memories` with read/write/delete permissions and default tools skipped.
- Use normal task/execution status, output, error, and alert mechanisms for scheduled consolidation results.
- Troubleshooting: if a completed chat/task produces no new memory file, first inspect memory run tables. If `memory_extraction_runs` has no row, the completion hook did not reach `RecordInteraction`; this differs from a successful extraction that decided there was nothing durable to save.
