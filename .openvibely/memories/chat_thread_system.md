---
name: chat_thread_system
type: project
created: 2026-05-09
updated: 2026-06-09
source: task
source_id: acf2a4452b2746bfd6e7c418103f4f84
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
- Task-thread queued rows created while a task execution is running are guarded by that active execution, including the initial task turn before any assistant output exists.
- Steering rows target an active execution with an `expected_turn_id` guard and use two-phase consumption so failed/cancelled provider steps can recover the input.
- As of 2026-06-08, current merged steering instruction formatting passes through the user's steering text trimmed (`strings.TrimSpace`) rather than wrapping it in an additional “latest user instruction” / “concise visible answer” prompt; earlier claims about that wrapper being present were corrected after code inspection.
- Prepared/in-flight steering clears `expected_turn_id` while the row can still have `input_status='pending'`; pending-list queries must exclude those prepared steering rows until they are committed, restored, or requeued.
- Provider failure requeue makes prepared steering visible again as queued input; retry restore makes it visible again as guarded steering by restoring `expected_turn_id`.
- As of 2026-06-08, steering retry/finalization hardening treats request-context cancellation as unsafe for durable cleanup writes: terminal steering recovery, execution/task finalization, and deferred-success steering recovery use non-cancelled cleanup/finalization contexts where needed so pending steering is not stranded and executions are not left running.
- Chat/task-thread success defers completion when pending steering exists; if follow-on steering preparation fails or finds no claimable steer, pending steering for that execution is requeued before terminal failure.
- Main-task finalization after provider return uses a non-cancelled finalization context for durable post-provider writes, including completion/failure, task status/category, steering recovery, alerts, notifications, diff/worktree post-processing, schedules, and chaining.
- Thread history for future calls is rebuilt from `executions.PromptSent` and cleaned `executions.Output` as plain turns rather than provider-native tool-use messages.
- Failed task executions remain visible terminal history rows. When a failed execution has no replayable assistant output, including empty output or marker-only output that cleans to empty, task-thread replay preserves its prompt by adding explicit failure metadata instead of leaving a trailing user-only turn that provider history builders may trim before a follow-up.
- As of 2026-06-08, initial task executions that report agent status failed (`[STATUS: FAILED | ...]`) persist the assistant's failure transcript/output while still marking the execution and task failed, so follow-up context can replay the failed turn chronologically.
- Task failure finalization does not clear execution diff output; `ExecutionRepo.Complete` updates terminal status/output/error metadata, while diff capture/update is a separate post-processing path. If a git worktree diff appears cleared after committing/rebasing, the task changes may simply be in the commit rather than uncommitted worktree state.

Routes, modes, and runtime action facts:
- `/chat` is the global/project-level orchestrator; the task detail Thread tab is task-specific.
- `/chat` supports Orchestrate and Plan modes. Plan is read-only and disables marker execution.
- Canonical chat capabilities live in `internal/chatcontrol/registry.go`.
- Runtime tools and marker processing are mutually exclusive for a request; injected runtime tools set `ProcessMarkers=false`.
- As of 2026-06-09, status-marker parsing treats `[STATUS: FAILED | ...]`, `[STATUS: COMPLETE | ...]`, and related markers as terminal control markers only when they appear as the final standalone non-empty line. Literal marker text in prose, code spans, code fences, bullets, quotes, examples, or lines with trailing explanatory text must not classify an agent or streaming follow-up execution as failed/completed.
- As of 2026-06-09, chat output cleaning preserves status/tool marker text when it appears inside inline backtick code spans, while still stripping real standalone control markers outside inline code. This fixed the user-facing symptom where an attempted literal failure marker example rendered as empty backticks (`The failure marker is ``. Here it is:`).
- The failed-task history replay and terminal-marker fixes are provider-neutral shared-layer behavior, not Anthropic-specific or Codex-specific fixes; Anthropic-style role cleanup and Codex-style output were only examples of where shared context replay or marker parsing symptoms surfaced.
- Task creation can happen through marker compatibility, runtime action surfaces, or local app APIs depending on the active runtime.
- Chat task creation distinguishes Agent definitions from model configs: `agent` names a selectable/enabled Agent, while `agent_id` is internal model-config selection.

Plan and thread facts:
- Plan handoff is driven by completed Plan-mode responses containing `<proposed_plan>`.
- Task-thread follow-ups use chronological execution history rather than reinjecting the original task prompt.
- The Task Thread transcript/pagination renders `executions` history; the original task prompt lives on `tasks.prompt` and may not appear as the oldest paged thread item unless it was also recorded as an execution prompt.
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
- Promoted queued task-thread follow-ups move the task back to Active before starting; this reactivation happens during the queue-claim/promotion transaction before the follow-up execution is exposed, and promotion is expected after initial-turn success, failure, or cancellation.
- Dragging or moving a failed task back to Active with no queued input must not blindly resubmit `tasks.prompt` when the latest execution overall is a failed task-thread follow-up. The Active reactivation path should retry that latest failed follow-up using the failed execution's `PromptSent` and chronological execution history; older failed follow-ups must be ignored if a later execution succeeded.
- Zero-model guardrails block task execution and Chat send surfaces with an `Open Models` action.
- Channel-origin runs follow the same queueing, steering, and task-thread rules. Provider-specific Slack/Telegram/GitHub/webhook facts live in `integrations_and_channels.md`.
- Worker dispatch capacity uses global/project/model counters. Task execution cleanup is centralized in a deferred finalizer after slot acquisition so normal completion, provider errors, context cancellation returns, task claim failures, and provider panics release slots, clear pending state/cancel registration, and trigger redispatch; task-thread follow-up slot acquisition paths already use immediate defers after acquisition. Provider-panic recovery also finalizes any still-running execution rows for the panicked task as failed with completion metadata, preventing stale `ExecRunning` history after worker-level panic recovery.
- A 2026-06-08 incident showed that concurrent task-thread follow-ups could produce synthetic failure status: one queued follow-up timed out waiting for a project worker slot, marked the task terminal/inactive, and stale-running recovery then marked a concurrently running successful execution failed with `Recovered stale running execution: owning task is terminal or inactive`.
- As of 2026-06-09, task-thread follow-up slot waiting is intentionally not charged against `worker_timeout`: follow-ups wait for project/model slots with a cancellable no-deadline queue context, then start the turn-processing/`worker_timeout` context only after slots are acquired. `WorkerService` no longer adds a hidden fallback timeout when slot acquisition is called without a deadline; queue wait should not fail the task merely because capacity has not opened yet.
- As of 2026-06-09, capacity-waiting task-thread follow-ups are cancellable while `queued`: `/tasks/:id/cancel` accepts queued tasks, calls the registered wait cancel function, finalizes the waiting execution as cancelled, and prevents it from later running when capacity opens. This fixed the audit finding left after removing queue-wait timeouts.

Operational implementation guidance for queueing, steering, task-thread follow-ups, channel turn promotion, task-goal runtime tools, and regression coverage belongs in `.openvibely/skills/openvibely_chat_thread_turn_workflow/SKILL.md`, `.openvibely/skills/openvibely_channel_integrations_workflow/SKILL.md`, and `.openvibely/skills/openvibely_task_goals_workflow/SKILL.md`.
