---
name: chat_thread_system
type: project
created: 2026-05-09
updated: 2026-06-06
source: after_complete
source_id: a21a336579af2b853222744d738f51f0
confidence: high
title: Chat and Task-Thread Behavior
---

Interactive chat bypasses worker capacity limits. Task-thread follow-ups respect worker limits and use `processStreamingResponse` with `IsTaskFollowup=true`.

Queueing and steering:
- Queueing and steering are distinct product behaviors. Normal Send during an active Chat or task-thread response queues the message for the next turn. Steering is an explicit action that amends the in-progress response only at OpenVibely-owned provider-call/model-step boundaries, including supported text-only local-tool result boundaries after a tool result is appended and before the next internal model request.
- Steering does not interrupt an already-running provider request or local tool execution. Tool-boundary steering must be wired for all active execution paths that own provider loops, including chat/task-thread streaming and main task runs through `LLMService.ExecuteTaskWithAgent`.
- Queueing and steering use persistent `thread_inputs`, not queued execution rows or interrupt-and-replace runs. `executions` remain actual model runs and should not carry queued-turn status or queue metadata.
- Pending-input statuses are only `pending`, `applied`, and `cancelled`; failed/error state belongs on the resulting execution/task. Queued ordering uses durable queue positions with created-at/rowid fallback, and allocation/promotion must be transactionally guarded.
- Queued input promotion atomically claims the pending input while creating the next execution/task, validates the exact surface/thread active state inside the transaction, and treats stale/missing/already-applied actions as conflicts rather than silent success.
- Worker-run task completion through `LLMService.ExecuteTaskWithAgent` must invoke the same task-thread queued-input promoter after terminal execution/task state is committed, including failed-completion branches. Startup recovery should re-drive pending queued task-thread inputs whose guarded execution is already terminal and whose task has no active execution, using the shared promoter path.
- Queued Chat, task-thread, API, Slack, and Telegram inputs may preserve `attachment_session_id`; promoted rows must process saved attachments so text context and image payloads reach the model before first live fragments render.
- Steering rows target the active execution id with an `expected_turn_id` guard. Steering consumption is two-phase: prepare pending steering into the next provider request, send the realtime UI-removal/applied event when preparation starts, mark the DB row applied only after that provider call succeeds, and requeue as pending queued input on provider failure/cancellation.
- Steering recovery must handle prepared and unprepared pending steering rows idempotently across provider retries, cancellations, commit/finalization failures, and terminal cleanup. Recovery/finalization paths should use non-cancelled cleanup contexts so hidden steering rows are not stranded and executions/tasks become terminal.
- During streaming, steering is checked before each provider call, after successful calls, and during a short final grace period before completion. Guarded completion must continue the model loop instead of closing the turn if late pending steering appears.
- Provider-facing steering should pass through only the user's raw steering content. Do not wrap steers in explanatory “latest instruction” prose.
- Steering attachments are previewed from pending upload storage for the consuming provider call, then moved/recorded only after success. Failed/cancelled consumption should leave attachments retryable. Attachment-bearing steers should not be claimed by the text-only tool-boundary callback until provider-loop attachment injection exists.
- Thread history for future calls is rebuilt from `executions.PromptSent` and cleaned `executions.Output` as plain user/assistant turns, not replayed as provider-native structured `tool_use`/`tool_result` messages. This is an intentional provider-portability tradeoff.
- If a queued row is converted to steering while other queued rows exist, the steered row must execute in the current active turn before remaining FIFO queued rows promote. Conversion after the active execution is no longer running should fail and leave the row queued.
- When a queued input promotes and creates the next active execution, remaining pending queued rows on the same Chat/task-thread surface must be atomically retargeted from the prior active execution guard to the new execution so their `Steer` actions remain valid.
- Stale `running` task executions whose owning task is terminal/inactive or has no real active worker must be repaired or ignored before active-turn decisions. Startup recovery must run after orphaned running tasks are reset, then terminalize impossible leftover runs so stale executions cannot trap follow-ups.
- Manual Start, move-to-active, and `TaskService.RunTask` entrypoints should promote the oldest pending task-thread queued input rather than silently rerunning the original prompt. If promotion fails, do not fall back to the original prompt as though promotion succeeded.
- Direct steering endpoints (`/chat/steer` and `/tasks/:id/thread/steer`) still exist even though the composer no longer exposes them. Their insert path should use the same atomic active-turn guard as queued-row-to-steering conversion and return no-active-turn instead of inserting stranded steering.
- Clearing Chat history should cancel project-scoped pending Chat inputs, because Chat pending rows are not task-scoped and could otherwise redraw or promote after visible history is deleted.
- Pending queued/steering rows must redraw from database state on Chat/task-thread refresh, not only immediate HTMX responses. Promoted queued Chat turns broadcast `chat_new_message` with `pending_input_id`; API polling by original queued id follows `run_execution_id` after application.

Routes, modes, and runtime actions:
- Chat is the main orchestrator at `/chat` for global/project-level conversation. Thread is the task-specific conversation on the task detail Thread tab. Root route `/` redirects to `/chat`, preserving `project_id`; Dashboard remains at `/dashboard`.
- `/chat` supports `orchestrate` (default) and `plan` (read-only planning). Plan mode enables read-only repo exploration tools, blocks mutating tools, and disables marker execution (`ProcessMarkers=false`).
- Canonical chat capability registry is `internal/chatcontrol/registry.go`; it defines action names, domain, read/write access, allowed modes, surfaces, confirmation requirements, and sensitivity. Web/API chat and channel services should derive runtime action tools from registry helpers. `memory_view` belongs in this registry as a read-only `memory` capability for Orchestrate and Plan so chat modes advertise selected-memory access even when default filesystem/shell tools are hidden.
- Runtime tools and marker processing are mutually exclusive per request. When runtime tools are injected, `ProcessMarkers=false` prevents duplicate execution. Legacy marker parser helpers remain for compatibility/tests, but normal chat entrypoints should not depend on assistant-emitted marker blocks.
- Task-thread follow-ups sent through the UI route still need legacy marker fallback for agents/providers that cannot receive OpenVibely runtime tools. The fallback should be gated on absence of injected runtime tools, not on `IsTaskFollowup=false`, so `[CREATE_TASK]` blocks emitted during `/tasks/:id/thread` follow-ups can persist child tasks and return canonical `[TASK_ID:...]` confirmations without double-executing tool-capable turns.
- Expected registry actions across surfaces include chat mode, capabilities, alert, model, personality, current project, and project switching actions.
- Chat orchestrate task creation should distinguish Agent definitions from model configs: `agent` means assign an Agent from the Agents page by exact unique selectable/enabled name, while `agent_id` is internal model config selection. Natural phrasing should set `agent` only when there is a clear Agent-definition match; unassigned prompts must not invent an agent from skills or model config ids.
- Agent-definition prompt context for task creation should advertise only names the backend can resolve safely. If duplicate enabled/selectable Agent definitions share a name, omit or disambiguate them. Normalize or escape user-editable Agent fields before injecting them into orchestration prompt context.

Plan handoff:
- `/chat` shows a post-plan handoff prompt when a completed assistant response contains `<proposed_plan>` while in plan mode.
- Clicking `Switch to Orchestrate` flips mode and auto-submits one task-creation handoff message for the first plan step.
- Plan-mode guidance should be prose-first while still requiring one `<proposed_plan>...</proposed_plan>` output block.
- Rendered chat/thread output strips `<proposed_plan>` wrapper tags while raw stored output keeps them for CTA detection.
- Plan completion prompt evaluation is centralized and requires stream complete, mode `plan`, and the latest completed assistant response containing `<proposed_plan>`. Older plan markers should not trigger it.
- Once an eligible plan handoff card is shown for the latest completed response, client refocus/refresh/hydration evaluations should preserve it. Only explicit dismissal, a newer response, active stream, or intentional mode switch should hide it.

Thread and follow-up behavior:
- Task thread interaction from `/chat` uses `[VIEW_TASK_CHAT]`/`[SEND_TO_TASK]` markers where compatibility requires it.
- `view_task_thread` supports pagination. Transcripts are size-budgeted with explicit continuation hints when truncated.
- Task-thread follow-ups should use chronological execution history, not re-inject the original task prompt, and propagate the task agent definition so plugin skills/MCP tools are active on API provider paths.
- User-facing task-thread follow-up bugs must be reproduced through the actual task thread send/queued input path, such as `TaskThreadSend`, `/tasks/:id/thread`, `thread_inputs` promotion, or `startQueuedTaskThreadInput`; directly calling the `send_to_task` runtime tool is a separate chat/action-tool path and is not valid evidence for UI task-thread follow-up behavior.
- Task-thread follow-ups run the normal task lifecycle routing path and can receive selected skill handles plus selected memory handles before the follow-up model call. Interactive Chat uses a narrower recall-only lifecycle preparation path: Memory Curator may select indexed memory handles, but Skill Curator routing/`skill_view` are not part of the normal chat memory recall path. When chat action tools and selected-memory runtime tools are composed, selected-memory execution should take precedence so `memory_view` is handled by the scoped memory executor rather than swallowed by a generic chat action fallback. Detailed `memory_view` exposure and handle-safety rules live in `managed_memory.md`.
- When task execution delegates to separate LLM-backed agents or lifecycle hooks, their outputs should be visible in an appropriate user-facing execution view; lifecycle-agent activity belongs in its dedicated task-detail tab rather than mixed into the main Thread tab.
- Follow-up completion inspects streaming text-only output for `[STATUS: FAILED | ...]` and `[STATUS: NEEDS_FOLLOWUP | ...]` markers. A missing/new-empty diff should not turn a successful read-only follow-up into failure.
- Failure completion preserves already-streamed `executions.output` when the failed completion call returns empty output so thread history is not reset.
- Shared streaming-runner cancellation should update both `executions.status` and the owning task status to cancelled, and should send channel Chat cancellation responses when the cancelled run originated from Slack/Telegram.
- Retry writer continuity seeds its in-memory buffer from existing `executions.output` for retryable provider retries on the same execution, preventing transient retries from overwriting streamed history.
- Chat bubble cleanup re-renders raw-content bubbles when rendered DOM is missing, even if signatures match, to avoid blank prior messages after failure/rate-limit refreshes.
- Runtime `execute_tasks` filters out completed tasks/statuses by default. Re-running completed tasks requires explicit `include_completed=true`; exact single-task targeting should use `task_id` or exact `title`.

Task goals:
- Task goals are durable `task_goals` records managed by `TaskGoalService`, visible/editable from task detail HTMX controls, and optionally created atomically with a task.
- Chat orchestration should support both explicit and implicit task-goal creation. Completion-condition phrasing such as “keep going until...” should be inferred as the task goal rather than only copied into the prompt.
- Goal continuation must enqueue normal task-thread follow-ups through `thread_inputs`; it should not start work inline or rely on deterministic auto-enqueue code after every successful follow-up. Goal Agent lifecycle execution details live in `agent_lifecycle_and_skills.md`.
- Goal status mutation tools such as `mark_task_goal_achieved` and `report_task_goal_blocked` should be available to any explicitly granted agent, not protected-Goal-Agent-only. Ungranted/default agents should not see or execute those tools.
- Goal status writes must retain stale `goal_id` plus current `status='active'` guards so paused/cleared/replaced goals reject stale evaluator writes. Stale guarded updates should return a stale-update error rather than reporting success with a null goal.
- Lifecycle hook status-tool execution should authorize against the actual lifecycle hook agent's grants when hook-agent context is present; protected Goal Agent authority comes from `system_kind=goal` hook identity, not caller-supplied runtime-origin fields.
- Task-thread goal prompt/context and `list_capabilities` output should reflect the assigned agent's actual goal status tool grants: ungranted agents are told the protected Goal Agent handles completion/blocker evaluation, while granted agents are told exactly which status tools they may use.
- Task-goal prompt context should be present across task-follow-up entry points, including web direct sends, queued task-thread sends, review submissions, and channel-origin task runs. Review and channel-origin runs should reactivate an achieved goal before prompt construction when appropriate. Supported channel parity details live in `integrations_and_channels.md`.
- Generic runtime `send_to_task` callers must not be able to spoof protected Goal Agent lineage by passing `origin="system_agent"` or `origin_agent="goal"`; only the internal Goal Agent runtime override may persist `source='system_agent'` with `origin_agent='goal'`.
- Goal Agent `send_to_task` should infer the current task in real task-thread runtimes. Durable queued/reloaded Goal Agent follow-ups retain lineage through `thread_inputs.origin_agent` alongside `source='system_agent'`.
- `ResumeGoal` only resumes paused goals, preserves the same `goal_id`, and clears blocked audit state.
- Kanban task cards render a `Goal` badge from a derived `Task.HasGoal` flag populated by the board/list task query for non-cleared goals; future task card/link surfaces need their queries expanded if they rely on the flag.
- When a user/manual task-thread follow-up starts on a task whose goal is `achieved`, OpenVibely reactivates that same goal to `active` before prompt context and lifecycle evaluation. Goal Agent/system-agent continuations are excluded so internal follow-ups do not reopen completed goals.
- Supported channel integrations, including Slack and Telegram, should keep task-goal controls at parity with the web/API chat control plane rather than restricting goal setting by surface.
- Lifecycle hook ordering for queued task messages: `before_run`/routing lifecycle prep for the queued turn must complete before that queued message is sent to the model, but the previous turn's asynchronous `after_complete` hook does not need to finish before queued promotion starts.

Task execution and scheduling:
- Active tasks auto-submit to the worker pool on creation or when moved to Active category.
- `/tasks/{id}/run` uses an atomic guarded pending update and only submits when that update succeeds, so duplicate run requests cannot downgrade running work back to pending.
- Scheduled tasks are triggered by the background scheduler when `next_run <= now`. One-time schedules set `next_run = NULL` after running; repeating schedules compute `next_run` from repeat type and interval.
- Tag-based execution allows batch task execution through chat commands.
- Promoted queued task-thread follow-ups must move the task category back to `active` before starting so active promoted work does not remain under Completed.
- `processChatSendToTask` and `TaskThreadSend` should not mutate task status/category until active checks, agent selection, execution/queued-input creation, and worktree preparation have succeeded. Immediate Chat creation paths should clean up newly-created chat tasks if execution creation fails.

Model guardrails:
- Task actions that transition to execution and chat send should block when zero models are configured, emit one `openvibelyToast`, and include a direct `Open Models` action.
- First created model auto-defaults when no models exist. Deleting a default model auto-promotes another remaining model; deleting the last model is allowed.
- Pending queued inputs whose stored `agent_config_id` is deleted or becomes `NULL` before promotion should resolve to the current default model. If no model is configured, cancel the unstartable queued row so FIFO promotion is not permanently blocked.
- API completed status should report task IDs extracted from processed `[TASK_ID:...]` markers, not all non-chat project tasks.
- Model-specific worker timeouts must be applied before creating the actual streaming context, not only before worker-slot waiting.

Channel-origin runs follow the same queueing/steering and task-thread rules. Provider-specific Slack/Telegram/GitHub/webhook behavior is captured in `integrations_and_channels.md`.
