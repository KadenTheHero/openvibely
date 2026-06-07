---
name: chat_thread_system
type: project
created: 2026-05-09
updated: 2026-06-07
source: task
source_id: f0f661ec9d460b5ba72cc21b2c39d038
confidence: high
title: Chat and Task-Thread Behavior
---

Interactive Chat bypasses worker capacity limits. Task-thread follow-ups respect worker limits and use `processStreamingResponse` with `IsTaskFollowup=true`.

Queueing and steering facts:
- Queueing and steering are distinct product behaviors.
- Normal Send during an active Chat or task-thread response queues a next-turn input.
- Steering is an explicit in-progress action applied only at OpenVibely-owned provider-call/model-step boundaries.
- Queueing and steering use durable `thread_inputs`; `executions` represent real model runs, not parked composer state.
- Queued input promotion claims the pending input while creating the next execution/task and keeps FIFO ordering durable.
- Steering rows target an active execution with an `expected_turn_id` guard and use two-phase consumption so failed/cancelled provider steps can recover the input.
- Prepared/in-flight steering clears `expected_turn_id` while the row can still have `input_status='pending'`; pending-list queries must exclude those prepared steering rows until they are committed, restored, or requeued.
- Provider failure requeue makes prepared steering visible again as queued input; retry restore makes it visible again as guarded steering by restoring `expected_turn_id`.
- Thread history for future calls is rebuilt from `executions.PromptSent` and cleaned `executions.Output` as plain turns rather than provider-native tool-use messages.

Routes, modes, and runtime action facts:
- `/chat` is the global/project-level orchestrator; the task detail Thread tab is task-specific.
- `/chat` supports Orchestrate and Plan modes. Plan is read-only and disables marker execution.
- Canonical chat capabilities live in `internal/chatcontrol/registry.go`.
- Runtime tools and marker processing are mutually exclusive for a request; injected runtime tools set `ProcessMarkers=false`.
- Task creation can happen through marker compatibility, runtime action surfaces, or local app APIs depending on the active runtime.
- Chat task creation distinguishes Agent definitions from model configs: `agent` names a selectable/enabled Agent, while `agent_id` is internal model-config selection.

Plan and thread facts:
- Plan handoff is driven by completed Plan-mode responses containing `<proposed_plan>`.
- Task-thread follow-ups use chronological execution history rather than reinjecting the original task prompt.
- Task-thread follow-ups run normal task lifecycle routing and can receive selected skill handles plus selected memory handles before the follow-up model call.
- Interactive Chat uses narrower recall-only memory preparation and does not run Skill Curator routing or expose `skill_view`.
- Lifecycle-agent activity belongs in the task Lifecycle tab rather than the main Thread tab.

Task-goal facts:
- Task goals are durable `task_goals` records managed by `TaskGoalService`.
- Chat orchestration supports explicit and implicit task-goal creation.
- Goal continuation uses normal task-thread follow-ups through `thread_inputs`; it does not start work inline.
- Goal status writes use stale `goal_id` plus active-status guards.
- Goal status tools can be granted to agents beyond the protected Goal Agent, but ungranted/default agents do not see or execute them.
- Manual/user follow-ups on achieved goals reactivate the same goal before prompt context and lifecycle evaluation; Goal Agent/system-agent continuations do not reopen achieved goals.

Task execution and scheduling facts:
- Active tasks auto-submit to the worker pool on creation or when moved to Active.
- Scheduled tasks run when `next_run <= now`; one-time schedules clear `next_run`, while repeating schedules compute the next occurrence.
- Schedules have a durable enabled/disabled state. Disabled schedules are excluded from due-run selection until re-enabled; toggling off preserves `next_run`, while toggling back on recomputes stale/past `next_run` to the next valid future occurrence.
- Chat control-plane schedule modification supports `enabled: bool` and should stay consistent with HTTP/API schedule toggling semantics.
- Promoted queued task-thread follow-ups move the task back to Active before starting.
- Zero-model guardrails block task execution and Chat send surfaces with an `Open Models` action.
- Channel-origin runs follow the same queueing, steering, and task-thread rules. Provider-specific Slack/Telegram/GitHub/webhook facts live in `integrations_and_channels.md`.
- Worker dispatch capacity uses global/project/model counters. Task execution cleanup is centralized in a deferred finalizer after slot acquisition so normal completion, provider errors, context cancellation returns, task claim failures, and provider panics release slots, clear pending state/cancel registration, and trigger redispatch; task-thread follow-up slot acquisition paths already use immediate defers after acquisition. Provider-panic recovery also finalizes any still-running execution rows for the panicked task as failed with completion metadata, preventing stale `ExecRunning` history after worker-level panic recovery.

Operational implementation guidance for queueing, steering, task-thread follow-ups, channel turn promotion, task-goal runtime tools, and regression coverage belongs in `.openvibely/skills/openvibely_chat_thread_turn_workflow/SKILL.md`, `.openvibely/skills/openvibely_channel_integrations_workflow/SKILL.md`, and `.openvibely/skills/openvibely_task_goals_workflow/SKILL.md`.
