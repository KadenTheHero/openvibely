---
name: chat_thread_system
type: project
created: 2026-05-09
updated: 2026-06-13
source: consolidation
source_id: memory_consolidation_2026_06_13
confidence: high
title: Chat and Task-Thread Behavior
---

Interactive Chat bypasses worker capacity limits. Task-thread follow-ups respect worker limits and use `processStreamingResponse` with `IsTaskFollowup=true`.

Queueing and steering facts:
- Queueing and steering are distinct product behaviors. Normal Send during an active Chat or task-thread response queues a next-turn input; steering is an explicit in-progress action applied only at OpenVibely-owned provider-call/model-step boundaries.
- Queueing and steering use durable `thread_inputs`; `executions` represent real model runs, not parked composer state.
- Queued input promotion claims the pending input while creating the next execution/task and keeps FIFO ordering durable.
- Queued messages with attachments store the pending upload `attachment_session_id` on the durable `thread_inputs` row; queued composer rows show an attachment indicator when that session ID is present, and final execution attachment records are created when the queued input is promoted and the attachment session is processed.
- Task-thread sends made while a task execution is running, including before the first assistant output or while initial lifecycle routing/hooks are still running, queue as next-turn input rather than running inline or duplicating messages.
- Steering rows target an active execution with an `expected_turn_id` guard and use two-phase consumption so failed/cancelled provider steps can recover input.
- Current merged steering instruction formatting passes through the user's steering text trimmed, without wrapping it in additional “latest user instruction” wording.
- Prepared/in-flight steering clears `expected_turn_id` while the row can still have `input_status='pending'`; pending-list queries and UI should exclude those rows until committed, restored, or requeued.
- Provider failure requeues prepared steering; retry restore returns it to guarded steering. Durable cleanup/finalization writes use non-cancelled contexts where needed so pending steering and executions are not stranded after request cancellation.
- Chat/task-thread success defers completion when pending steering exists; if follow-on steering preparation fails or finds no claimable steer, pending steering for that execution is requeued before terminal failure.
- Thread history for future calls is rebuilt from `executions.PromptSent` and cleaned `executions.Output` as plain turns rather than provider-native tool-use messages.
- Failed task executions remain visible terminal history rows. When no replayable assistant output exists, replay preserves the prompt with explicit failure metadata rather than leaving a trailing user-only turn.
- Initial task executions that report final `[STATUS: FAILED | ...]` persist assistant failure transcript/output while marking execution and task failed, so follow-up context can replay the failed turn chronologically.
- Task failure finalization does not clear execution diff output; diff capture/update is a separate post-processing path.

Routes, modes, and runtime actions:
- `/chat` is the global/project-level orchestrator; the task detail Thread tab is task-specific.
- `/chat` supports Orchestrate and Plan modes. Plan is read-only and disables marker execution.
- Canonical chat capabilities live in `internal/chatcontrol/registry.go`.
- Runtime tools and marker processing are mutually exclusive for a request; injected runtime tools set `ProcessMarkers=false`.
- Status-marker parsing treats `[STATUS: FAILED | ...]`, `[STATUS: COMPLETE | ...]`, and related markers as terminal control markers only when they are the final standalone non-empty line. Literal marker text in prose, code spans, code fences, bullets, quotes, examples, or lines with trailing explanatory text must not classify an execution as failed/completed.
- Chat output cleaning preserves status/tool marker text inside inline backtick code spans while stripping real standalone control markers outside inline code.
- Failed-task history replay and terminal-marker fixes are provider-neutral shared-layer behavior, not Anthropic- or Codex-specific fixes.
- Task creation can happen through marker compatibility, runtime action surfaces, or local app APIs depending on active runtime.
- Chat task creation distinguishes Agent definitions from model configs: `agent` names a selectable/enabled Agent, while `agent_id` is internal model-config selection.
- Chat-control task/schedule automation should respect schema semantics: task `priority` uses `1=Low`, `2=Normal`, `3=High`, `4=Urgent`; scheduled task `days` values are short weekday keys; `edit_task` needs a real task ID; `schedule_task` moves the target task into `scheduled`, so bootstrap flows needing immediate work should schedule then explicitly execute when appropriate.

Plan, thread, and goal facts:
- Plan handoff is driven by completed Plan-mode responses containing `<proposed_plan>`.
- Task-thread follow-ups use chronological execution history rather than reinjecting the original task prompt.
- The Task Thread transcript/pagination renders `executions` history; `tasks.prompt` may not appear as the oldest paged thread item unless also recorded as an execution prompt.
- Task-thread follow-ups run normal task lifecycle routing before the follow-up model call; selected skill/memory handle semantics are owned by the lifecycle and managed-memory memories.
- Interactive Chat uses narrower recall-only memory preparation rather than full Skill Curator routing.
- Lifecycle-agent activity belongs in the task Lifecycle tab rather than the main Thread tab.
- Task goals are durable `task_goals` records managed by `TaskGoalService`. Chat orchestration supports explicit and implicit task-goal creation.
- Goal continuation uses normal task-thread follow-ups through `thread_inputs`; it does not start work inline.
- Tasks do not support direct peer-to-peer chat. Coordination is mediated through app state and control-plane mechanisms such as `send_to_task`, `view_task_thread`, chain-created child tasks, schedules, dynamic wakeups, persisted goals, and durable `thread_inputs`.
- A durable “inbox task” pattern is a canonical task thread, not an automatic mailbox.
- Goal status writes use stale `goal_id` plus active-status guards. Goal status tools can be granted to agents beyond Goal Agent, but ungranted/default agents do not see or execute them.
- Manual/user follow-ups on achieved goals reactivate the same goal before prompt context and lifecycle evaluation; Goal Agent/system-agent continuations do not reopen achieved goals.
- Manual stop/cancel signals implicitly pause the active task goal with reason `stopped by user`, preserving the goal for intentional resume. Starting a task again resumes only goals paused with that reason; explicit/manual goal pauses remain explicit-resume-only.
- Direct task-thread cancel and Active-to-Backlog/Completed moves cancel queued/running execution, pause the active goal, and preserve the user's requested target category. `TaskService.UpdateCategory` must use the pre-update category/status for stop semantics.
- Goal Agent `send_to_task` re-checks active goal state and execution freshness before queueing continuations, preventing stale lifecycle hooks from restarting stopped or superseded work.

Task execution and scheduling facts:
- Active tasks auto-submit to the worker pool on creation or when moved to Active.
- Scheduled tasks run when `next_run <= now`; one-time schedules clear `next_run`, while repeating schedules compute the next occurrence.
- Schedules have durable enabled/disabled state. Disabled schedules are excluded from due-run selection until re-enabled; toggling off preserves `next_run`, while toggling on recomputes stale/past `next_run` to the next valid future occurrence.
- Dynamic task-loop wakeups are schedule rows with wakeup metadata and replacement semantics. When due, the scheduler enqueues normal task-thread follow-ups through durable `thread_inputs`; wakeups are blocked for stopped goal states and failures should be visible on the task timeline/event stream.
- Chat control-plane schedule modification supports `enabled: bool` and must stay consistent with HTTP/API toggling semantics.
- Promoted queued task-thread follow-ups move the task back to Active during the queue-claim/promotion transaction before the follow-up execution is exposed.
- Reactivating a failed task with no queued input should retry the latest failed follow-up using its `PromptSent` and chronological history, not blindly resubmit `tasks.prompt`; older failed follow-ups are ignored if a later execution succeeded.
- Zero-model guardrails block task execution and Chat send surfaces with an `Open Models` action.
- Channel-origin runs follow the same queueing, steering, and task-thread rules where supported.
- Worker dispatch capacity uses global/project/model counters. Cleanup after slot acquisition is centralized so completion, provider errors, cancellation, claim failures, and provider panics release slots, clear pending/cancel state, and trigger redispatch.
- Task-thread follow-up slot waiting is not charged against `worker_timeout`; follow-ups wait for project/model slots with a cancellable no-deadline queue context, then start turn-processing timeout after slots are acquired. Capacity-waiting follow-ups are cancellable while `queued`.

Operational implementation guidance belongs in skills such as `openvibely_chat_thread_turn_workflow`, `openvibely_channel_integrations_workflow`, and `openvibely_task_goals_workflow`.
