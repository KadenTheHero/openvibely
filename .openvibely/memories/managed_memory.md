---
name: managed_memory
type: project
created: 2026-05-09
updated: 2026-06-07
source: consolidation
source_id: memory_consolidation_2026_06_07
confidence: high
title: Managed Memory
---

OpenVibely managed memory is model-backed, tool-driven, project-scoped, and stored under the selected project's repo-local `.openvibely/memories/` directory. The directory stays flat: `MEMORIES.md` is the compact index and focused top-level topic files hold durable context. The old `user/`, `feedback/`, `project/`, and `runs/` subdirectory model is obsolete.

Durable storage facts:
- Managed memory requires a selected project with a valid local `repo_path`.
- `MemoryService` owns path resolution, file storage, context building, extraction, consolidation, and DB metadata.
- Existing SQLite task/chat execution history is the transcript source; JSONL transcript storage and app-owned memory roots are not part of the current design.
- Startup/runtime paths may create `.openvibely/memories/` and `MEMORIES.md` when missing. Topic files are created by explicit durable-memory writes.
- Memory schedule seeding is separate from repo-local memory directory initialization: the default project can receive the visible Memory Consolidation scheduled task even when it has no `repo_path`, while actual memory file operations still require a valid local repo path.
- Managed-memory tools are scoped to the memory directory and reject traversal, absolute paths, and symlink escapes.

Content boundaries:
- Memory stores durable contextual notes: user preferences, product direction, architectural decisions, workflow constraints, current-state facts, recurring pitfalls, incidents, and repeated feedback.
- Memory excludes raw complaints, assistant boilerplate, one-off prompts, transient logs, raw transcripts, secrets, provider-internal terminology, task-by-task summaries, Chat page prompts, mode-control text, and procedure-only runbooks.
- Static repository operating instructions belong in app-managed skills and selected managed memory, not repo-root `AGENTS.md` or `CLAUDE.md`.

Lifecycle facts:
- Memory lifecycle work is owned by the built-in Memory Curator through `recall_memory`, `update_memory`, and `consolidate_memory`.
- Memory Curator recall is a `route_task` handle-selection step with `selected_memories`, parallel to Skill Curator `selected_skills`.
- Recall receives only the compact index from `MEMORIES.md`; topic memory bodies are not loaded during route selection.
- Normal tasks and task-thread follow-ups consume managed memory through route-selected handles. Interactive Chat uses a recall-only lifecycle preparation path.
- Selected-memory prompt context is handle-only for skill-style parity. Memory bodies are loaded only on demand through the authorized `memory_view` tool.
- `memory_view` is a read-only request-scoped runtime tool authorized only for route-selected indexed handles plus exact indexed handles explicitly requested by the user for that turn. It rejects `MEMORIES.md`, full paths, traversal, unindexed handles, and arbitrary unselected files.
- `memory_view` is also an explicit agent allowed-tool grant surfaced in the agent create/edit dialog.
- Route-generated memory summaries/snippets/topics are debug metadata, not final task/chat model context.

Scheduled consolidation:
- Memory consolidation runs as a normal scheduled task assigned to Memory Curator with the `consolidate_memory` skill.
- Each project has a real system-created scheduled task visible on the Schedule page while hidden from the normal Tasks board.
- Runtime execution uses the generic scheduled-agent path and scoped memory-file tools; separate hidden-run behavior and bespoke memory-specific scheduler surfaces are not part of the intended design.

Operational implementation guidance for managed-memory routing, selected-memory lifecycle hooks, `memory_view` authorization, provider propagation, and memory troubleshooting belongs in `.openvibely/skills/openvibely_skill_lifecycle_workflow/SKILL.md`, `.openvibely/skills/openvibely_lifecycle_hook_workflow/SKILL.md`, and `.openvibely/skills/openvibely_chat_provider_test_workflow/SKILL.md`.
