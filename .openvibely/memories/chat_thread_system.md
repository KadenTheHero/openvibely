---
name: chat_thread_system
type: project
created: 2026-05-09
updated: 2026-06-01
source: after_complete
source_id: f076cd4c16ee53c0a0e05418c388f12f
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
- Worker-run task completion through `LLMService.ExecuteTaskWithAgent` must invoke the same task-thread queued-input promoter after terminal execution/task state is committed; queued follow-ups behind normal worker tasks should not depend only on handler-managed streaming finalization. As of 2026-06-01, the previously missed `llm_service.go` “project repo path missing” failed-completion branch was fixed to invoke promotion after committing failed execution/task state. Startup recovery should also re-drive pending queued task-thread inputs whose guarded execution is already terminal and whose task has no active execution, using the same shared promoter path; this prevents already-stranded rows from remaining stuck forever after a missed completion callback. Current recovery is capped at 100 task IDs per process start, so large accumulated backlogs require a looped recovery pass or repeated restarts.
- Queued Chat, task-thread, API, Slack, and Telegram inputs may preserve `attachment_session_id`; promoted rows must process saved attachments so text context and image payloads reach the model before first live fragments render.
- Steering rows target the active execution id with an `expected_turn_id` guard. Steering consumption is two-phase: prepare pending steering into the next provider request, send the realtime UI-removal/applied event when preparation starts, mark the DB row applied only after that provider call succeeds, and requeue as pending queued input on provider failure/cancellation.
- Steering recovery must handle prepared and unprepared pending steering rows idempotently across provider retries, cancellations, commit/finalization failures, and terminal cleanup. Recovery/finalization paths should use non-cancelled cleanup contexts so hidden steering rows are not stranded and executions/tasks become terminal.
- During streaming, steering is checked before each provider call, after successful calls, and during a short final grace period before completion. Guarded completion must continue the model loop instead of closing the turn if late pending steering appears.
- When steering is consumed after assistant output has streamed, the active prompt plus assistant delta since the steering cursor is added to provider history as a synthetic completed exchange, and the raw steering content becomes the next provider message. If steering arrives before assistant output, combine the active prompt with the raw steering content without explanatory wrapper text.
- Provider-facing steering should pass through only the user's raw steering content. Do not wrap steers in explanatory “latest instruction” prose.
- Steering attachments are previewed from pending upload storage for the consuming provider call, then moved/recorded only after success. Failed/cancelled consumption should leave attachments retryable. Attachment-bearing steers should not be claimed by the text-only tool-boundary callback until provider-loop attachment injection exists.
- Thread history for future calls is rebuilt from `executions.PromptSent` and cleaned `executions.Output` as plain user/assistant turns, not replayed as provider-native structured `tool_use`/`tool_result` messages. This is an intentional provider-portability tradeoff.
- If a queued row is converted to steering while other queued rows exist, the steered row must execute in the current active turn before remaining FIFO queued rows promote. Conversion after the active execution is no longer running should fail and leave the row queued.
- When a queued input promotes and creates the next active execution, remaining pending queued rows on the same Chat/task-thread surface must be atomically retargeted from the prior active execution guard to the new execution so their `Steer` actions remain valid.
- Stale `running` task executions whose owning task is terminal/inactive or has no real active worker must be repaired or ignored before active-turn decisions. Startup recovery must run after orphaned running tasks are reset, then terminalize impossible leftover runs including active/pending crash leftovers so stale executions cannot trap follow-ups. Completed/cancelled task-thread follow-ups should start a new follow-up execution instead of queueing behind such stale rows.
- Manual Start, move-to-active, and `TaskService.RunTask` entrypoints should promote the oldest pending task-thread queued input rather than silently rerunning the original prompt. If a queued input exists but promotion fails, do not fall back to the original prompt as though promotion succeeded.
- As of task `f076cd4c16ee53c0a0e05418c388f12f` on 2026-06-01, repeated audits of the stale task-thread `running` execution lifecycle fix established these invariants: direct follow-up starts must serialize with an in-transaction active-execution guard and create the execution atomically with moving the task to `active/queued`; concurrent send races should become normal queued follow-ups across web task threads, chat `send_to_task`, Slack, and Telegram; startup workerless task-execution recovery must terminalize persisted task-thread `running` rows after restart, including `active/queued` crash leftovers, and must not strand older pending queued follow-ups; manual Start/move-to-active must check for real active executions before promoting queued follow-ups, preserving FIFO and avoiding parallel executions.
- Final follow-up fixes for task `f076cd4c16ee53c0a0e05418c388f12f` on 2026-06-01 resolved the remaining queued task-thread blockers: `ClaimQueuedForTaskExecution` now uses a `BEGIN IMMEDIATE` transaction with an in-transaction active-execution guard; startup re-drives eligible pending task-thread queued inputs after stale recovery clears dead execution guards; Slack/Telegram `send_to_task` trigger task-thread promotion when appending behind an idle pending follow-up.
- Direct steering endpoints (`/chat/steer` and `/tasks/:id/thread/steer`) still exist even though the composer no longer exposes them. Their insert path should use the same atomic active-turn guard as queued-row-to-steering conversion and return no-active-turn instead of inserting stranded steering.
- Clearing Chat history should cancel project-scoped pending Chat inputs, because Chat pending rows are not task-scoped and could otherwise redraw or promote after visible history is deleted.
- Pending queued/steering rows must redraw from database state on Chat/task-thread refresh, not only immediate HTMX responses. Promoted queued Chat turns broadcast `chat_new_message` with `pending_input_id`; API polling by original queued id follows `run_execution_id` after application.

Routes, modes, and runtime actions:
- Chat is the main orchestrator at `/chat` for global/project-level conversation. Thread is the task-specific conversation on the task detail Thread tab. Root route `/` redirects to `/chat`, preserving `project_id`; Dashboard remains at `/dashboard`.
- `/chat` supports `orchestrate` (default) and `plan` (read-only planning). Plan mode enables read-only repo exploration tools, blocks mutating tools, and disables marker execution (`ProcessMarkers=false`).
- Canonical chat capability registry is `internal/chatcontrol/registry.go`; it defines action names, domain, read/write access, allowed modes, surfaces, confirmation requirements, and sensitivity. Web/API chat and channel services should derive runtime action tools from registry helpers.
- Runtime tools and marker processing are mutually exclusive per request. When runtime tools are injected, `ProcessMarkers=false` prevents duplicate execution. Legacy marker parser helpers remain for compatibility/tests, but normal chat entrypoints should not depend on assistant-emitted marker blocks.
- Expected registry actions across surfaces include chat mode, capabilities, alert, model, personality, current project, and project switching actions.
- Chat orchestrate task creation should distinguish Agent definitions from model configs: `agent` means assign an Agent from the Agents page by exact unique selectable/enabled name, while `agent_id` is internal model config selection. Natural phrasing such as “Have <agent name>…”, “Ask <agent name>…”, or “Use <agent name>…” should set `agent` only when there is a clear Agent-definition match; unassigned prompts must not invent an agent from skills or model config ids.
- Agent-definition prompt context for task creation should advertise only names the backend can actually resolve safely. If duplicate enabled/selectable Agent definitions share a name, omit or disambiguate them. Normalize or escape user-editable Agent fields before injecting them into orchestration prompt context.

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
- When task execution delegates to separate LLM-backed agents or lifecycle hooks, their outputs should be visible in an appropriate user-facing execution view; lifecycle-agent activity belongs in its dedicated task-detail tab rather than mixed into the main Thread tab.
- Follow-up completion inspects streaming text-only output for `[STATUS: FAILED | ...]` and `[STATUS: NEEDS_FOLLOWUP | ...]` markers. A missing/new-empty diff should not turn a successful read-only follow-up into failure.
- Failure completion preserves already-streamed `executions.output` when the failed completion call returns empty output so thread history is not reset.
- Shared streaming-runner cancellation should update both `executions.status` and the owning task status to cancelled, and should send channel Chat cancellation responses when the cancelled run originated from Slack/Telegram.
- Retry writer continuity seeds its in-memory buffer from existing `executions.output` for retryable provider retries on the same execution, preventing transient retries from overwriting streamed history.
- Chat bubble cleanup re-renders raw-content bubbles when rendered DOM is missing, even if signatures match, to avoid blank prior messages after failure/rate-limit refreshes.
- Runtime `execute_tasks` filters out completed tasks/statuses by default. Re-running completed tasks requires explicit `include_completed=true`; exact single-task targeting should use `task_id` or exact `title`.
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
