---
name: chat_thread_system
type: project
created: 2026-05-09
updated: 2026-05-14
source: consolidation
source_id: memory_consolidation_2026_05_14
confidence: high
title: Chat and Task-Thread Behavior
---

Interactive chat bypasses worker capacity limits. Task-thread follow-ups respect worker limits and use `processStreamingResponse` with `IsTaskFollowup=true`.

Definitions and routes:
- Chat = main orchestrator at `/chat` for global/project-level conversation.
- Thread = task-specific conversation on the task detail Thread tab.
- Root route `/` redirects to `/chat`, preserving `project_id` when provided. Dashboard remains at `/dashboard`.

Chat modes and runtime actions:
- `/chat` supports `orchestrate` (default) and `plan` (read-only planning).
- Plan mode enables read-only repo exploration tools (`read_file`, `list_files`, `grep_search`) and blocks mutating tools (`write_file`, `edit_file`, `bash`).
- Plan mode disables marker execution (`ProcessMarkers=false`) so no task/settings mutations run from marker blocks.
- Canonical chat capability registry is `internal/chatcontrol/registry.go`; it defines action names, domain, read/write access, allowed modes, surfaces, confirmation requirements, and sensitivity. Tool definitions, mode gating, and surface availability derive from the registry.
- Web/API chat and channel services should generate runtime action tools from `chatcontrol.ToolDefsForContext(mode, surface, includeThread)` rather than hand-crafted tool lists.
- Runtime tools and marker processing are mutually exclusive per request. When runtime tools are injected, `ProcessMarkers=false` prevents duplicate execution.
- Legacy marker parser helpers remain for compatibility/tests, but normal chat entrypoints should not depend on assistant-emitted marker blocks.
- New/expected registry actions across surfaces include `get_chat_mode`, `set_chat_mode`, `list_capabilities`, `get_alert`, `get_model`, `get_personality`, `get_current_project`, and `switch_project`.

Plan handoff behavior:
- `/chat` shows a post-plan handoff prompt when a completed assistant response contains `<proposed_plan>` while in plan mode.
- Clicking `Switch to Orchestrate` flips mode and auto-submits one task-creation handoff message for the first plan step.
- Plan-mode guidance should be prose-first while still requiring one `<proposed_plan>...</proposed_plan>` output block.
- Rendered chat/thread output strips `<proposed_plan>` wrapper tags while raw stored output keeps them for CTA detection.
- Plan completion prompt evaluation is centralized and requires stream complete, mode `plan`, and the latest completed assistant response containing `<proposed_plan>`. Older plan markers should not trigger it.

Thread/follow-up behavior:
- Task thread interaction from `/chat` uses `[VIEW_TASK_CHAT]`/`[SEND_TO_TASK]` markers where compatibility requires it.
- `view_task_thread` supports `offset`/`limit` pagination. Transcripts are size-budgeted (80KB total, 50KB per message) with explicit continuation hints when truncated.
- Task-thread follow-ups should use chronological execution history, not re-inject the original task prompt, and propagate the task agent definition so plugin skills/MCP tools are active on API provider paths.
- Follow-up completion inspects streaming text-only output for `[STATUS: FAILED | ...]` and `[STATUS: NEEDS_FOLLOWUP | ...]` markers. A missing/new-empty diff should not turn a successful read-only follow-up into failure.
- Failure completion preserves already-streamed `executions.output` when the failed completion call returns empty output so thread history is not reset.
- Retry writer continuity: streaming writer seeds its in-memory buffer from existing `executions.output` for retryable provider retries on the same execution, preventing transient retries from overwriting streamed history.
- Chat bubble cleanup re-renders raw-content bubbles when rendered DOM is missing, even if signatures match, to avoid blank prior messages after failure/rate-limit refreshes.
- Runtime `execute_tasks` filters out completed tasks/statuses by default. Re-running completed tasks requires explicit `include_completed=true`.
- Runtime `execute_tasks` supports exact single-task targeting by `task_id` or `title`; use exact targeting for specific-task requests instead of broad tag/priority filters.

Task execution/scheduling behavior:
- Active tasks auto-submit to the worker pool on creation or when moved to Active category.
- `/tasks/{id}/run` uses an atomic guarded pending update (`status NOT IN ('running','queued')`) and only submits when that update succeeds, so duplicate run requests cannot downgrade running work back to pending.
- Scheduled tasks are triggered by the background scheduler when `next_run <= now`.
- One-time schedules set `next_run = NULL` after running; repeating schedules compute `next_run` from repeat type and interval.
- Tag-based execution allows batch task execution through chat commands.

Model guardrails:
- Task actions that transition to execution and chat send should block when zero models are configured, emit one `openvibelyToast`, and include a direct `Open Models` action.
- First created model auto-defaults when no models exist. Deleting a default model auto-promotes another remaining model; deleting the last model is allowed.
