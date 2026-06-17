---
name: realtime_and_frontend_patterns
type: project
created: 2026-05-09
updated: 2026-06-17
source: after_complete_task
source_id: b648ce2d3cb23ea713498049cc242e40
confidence: high
title: Realtime and Frontend Patterns
---

OpenVibely uses server-rendered HTMX/templ UI with shared SSE-style live updates. The broad UI contract is: SSE announces that something changed, and HTMX/server-rendered templ fragments provide authoritative state.

Realtime and diff facts:
- The shared `/events/live` stream multiplexes task, chat, and file-change events.
- Sidebar-managed client code fans shared live events into browser `CustomEvent`s such as `sse-task-event`, `sse-chat-live-event`, `sse-file-change-event`, and `sse-live-connected`.
- `window._tabVisibility` owns broad realtime connection visibility behavior and pauses polling while hidden.
- Per-execution `/events/chat/:exec_id` streams are the token-style output path for high-frequency execution output.
- Streamed output is persistence-first so reconnect/refresh can resume from execution rows.
- Real-time file changes stream to the Changes tab during task execution via SSE using worktree diff snapshots that include committed branch changes and uncommitted work. Live diff snapshot UI indicators are runtime feedback, not durable task artifacts.
- Task detail lazily loads Changes tab content unless `tab=changes` is active. Direct `?tab=changes` renders remain equivalent to lazy route behavior for stateful Changes features such as worktree merge controls.
- Diff viewer uses GitHub-style load envelopes and oversized-file placeholders rather than eagerly mounting all diff DOM.
- Deleted files render as normal file cards with deletion summaries where needed.
- Renamed/copied file headers render old and new paths in a constrained multi-line from/to layout; normal single-path file headers remain compact/truncated.

Chat/thread rendering facts:
- Chat and task-thread streaming batch DOM rendering and force a final flush on completion.
- Queued and steering-pending Chat/task-thread messages render as compact composer/input-box rows rather than transcript bubbles. Queued rows with a durable `attachment_session_id` show an “Attachments queued” indicator; pending sessions are only finalized into execution attachments when the queued input is promoted.
- Promoted queued task-thread runs are discovered through live events that remove stale pending rows, append promoted execution fragments, and attach execution streams. The queued promotion path publishes both `task_thread_execution_started` and `task_thread_input_applied`; either event should recover the UI if the other is missed.
- Initial task Run from the task detail page also publishes `task_thread_execution_started` after the execution row exists. Because Run-button navigation, lazy Thread-tab loading, and shared `/events/live` can race, the task detail page should force-refresh the authoritative `/tasks/:id/thread` fragment when opening the Thread tab after Run and on current-task start/status/input-applied/live-reconnect signals; active `pending` task-thread views poll so they recover while waiting for the worker to create an execution row. The visible Thread tab uses the internal `chat` tab key, but direct task-detail URLs with `?tab=thread` should normalize to the same Thread tab behavior. Forced task-thread fragment refreshes must close any tracked per-execution `/events/chat/:exec_id` EventSources before replacing `#thread-content`, `#task-thread-view`, `#task-detail-content`, or `#main-content` so running streams are not duplicated.
- Per-execution `/events/chat/:exec_id` stream setup should retry brief early failures, including explicit `execution not found` SSE errors, because live promotion events can reach the browser before the promoted execution stream is queryable.
- Prepared/in-flight steering rows should disappear from pending/composer UI on live events and page refreshes even if durable `thread_inputs` status remains `pending`. Chat and task-thread surfaces reconcile stale pending/steering rows on shared live reconnect via authoritative pending-input fragments (`/chat/pending-inputs` and `/tasks/:taskId/thread/pending-inputs`), which matters because hidden-tab SSE gaps can miss input-applied events.
- Long Chat and Task Thread histories remain complete in the database but are server-windowed in the UI with scroll-top pagination; initial and older windows default to 30 execution/turn rows and request `limit` is capped at 100.
- Browser-memory protection depends on removing old transcript execution DOM nodes from the visible window, not hiding them with CSS. Earlier-page loading uses bounded `limit`/`before` routes, prepends with scroll-anchor preservation, and should be gesture-latched/request-aware.
- Active chat/task-thread streaming uses smart autoscroll semantics: pinned viewers follow growth, while upward user movement is intent to read.
- Chat/thread markdown rendering escapes raw HTML-like tags outside fenced/inline code before markdown parsing.
- Chat and Task Thread composers share sent-message history navigation in the common composer template. History persists in localStorage, is capped at 50 entries, uses separate global-chat and per-task keys, and restores the pre-navigation draft when navigating past newest or pressing Escape.
- Chat/task-thread tool result output should remain full-fidelity: canonical stream `tool_result` markers are not display-truncated, and shared rendering uses bounded responsive scroll containers that avoid trapping page/thread scrolling.
- Plan-mode read-only repo exploration tool cards remain visible during live streams and refreshes.

Responsive and shared UI contracts:
- Chat/task-thread message panes, bubbles, composers, and tool/code output must not create whole-page horizontal scrolling on mobile. Keep roots/cards width-bounded with `min-w-0`/`max-w-full`, wrap long content, and use inner scroll/overflow boundaries for long code/tool output.
- Avoid hard `overflow-x-hidden` on immediate chat/thread roots, message scrollports, or the rounded composer shell when it clips composer shadows, chat-bubble shadows, or rounded bevels. Use gutters/inner containment so shadows can paint while content stays viewport-safe.
- Chat/task-thread message bubbles and the shared composer/input box should align visually with Agents/Models card width while preserving native in-pane scrolling and unclipped shadows/tails. The current contract uses a small left-shift gutter on message scrollports/composer gutters, not root-level negative margins or right-side narrowing hacks.
- Chat/task-thread composer bottom controls must remain contained on mobile without clipping the rounded input shell or leaving artificial side gaps. Keep selector areas shrinkable/truncated, keep the action cluster non-shrinking, and share the same chat-surface shadow token as message bubbles.
- Chat/task-thread composer model selectors and the chat mode selector intentionally use custom portal-style dropdown panels appended to `document.body`, with hidden form inputs carrying values. Avoid native `<select>` behavior for these controls.
- Do not define `scrollbar-gutter: stable` or WebKit `::-webkit-scrollbar` width/track/thumb rules for chat/task-thread transcript panes when native macOS overlay behavior is desired; Firefox-compatible `scrollbar-width`/`scrollbar-color` is acceptable.
- Task detail Details tab keeps Prompt, Goal, and Git Worktree in that order with matching card containers. Its main seven-tab row must remain horizontally scrollable/no-wrap on narrow mobile viewports.
- Task detail completion UI state is split across independently refreshed fragments: status/metrics polling updates the badge, while state-dependent action controls live in `#task-detail-actions` and are refreshed through `/tasks/:taskId/detail-actions`. Browser `sse-task-event` task status changes for the current task should trigger an immediate action refresh on terminal statuses.
- Task Changes/diff surfaces must stay viewport-contained on 320px-class screens. Use wrapping flex rows, shrink-safe long paths/branch labels, and separate overflow boundaries for Changed Files and Worktree Changes lists. Current known gap from the 2026-06-14 audit: long filenames in diff-viewer Changed Files badge lists and long branch/target labels in the Worktree Changes header still need stronger containment.
- Tasks page uses server-rendered kanban board/task-card templ components. Responsive contract: one-column stacked board/cards on phones, two columns around tablet widths, three columns on desktop, no phantom fourth column, no global fixed one-third column width, independent mobile dropzone scrolling, no page-level horizontal overflow, mobile-safe wrapping, at least 44px touch targets, compact desktop card density, and the `+ Add Task` header action colocated beside the title like Models `+ Add Model`.
- In the Completed column, Date newest/oldest sorting is completion-time sorting via nullable `tasks.completed_at`; Backlog date sorting remains creation-time sorting. UI/tests should keep backlog and completed sort semantics distinct.
- Responsive card pages such as Models, Agents, Alerts, Channels, and Personality should keep page roots, grids, cards, badges, and inner content shrink-safe with `max-w-full`/`min-w-0` containment. Long badge values need truncation within the badge row rather than expanding the card/grid.
- Goal edit controls belong in the task edit dialog; verified-state Git worktree actions can remain on the details surface.
- Schedule UI surfaces should clearly distinguish disabled schedules, and dynamic task-loop wakeups should remain visually distinct from fixed schedules.
- Chat/thread/task-result links share global link token `--ov-link-color: #7480ff`.
- Left sidebar navigation preserves hover-only highlight behavior unless the product intentionally redesigns selected nav state.
- Mobile sidebar navigation uses DaisyUI drawer checkbox `#sidebar-toggle`; selecting a nav option should close the drawer only after HTMX has accepted/sent the request. The mobile drawer overlay and panel must layer above sticky page content, with the panel above the overlay.
- `/models` uses `LLMConfig`/`agent_configs`; `/agents` is plugin-first and has no `color` field.
- Project-scoped settings pages preserve active project via the `project_id` URL query. Models create/edit forms should keep `project_id` on HTMX and native fallback submit URLs.
- `Managed Memory` as a tool/profile is presented as a scoped memory-file capability, not broad repo read/write access.
- Toast rendering accounts for native dialog top-layer behavior.
- Native DaisyUI dialogs should become full-screen on mobile via shared base CSS, including landscape phone dimensions; dimension-based rules are more reliable than pointer/hover heuristics in emulators and WebViews.
- Destructive deletes for projects, tasks, models, agents, skills, schedules, and channel integrations use the shared native DaisyUI `<dialog>` confirmation pattern with explicit Cancel and destructive confirm controls; avoid reverting to browser `confirm()` or HTMX `hx-confirm` immediate-delete wiring.

Operational guidance belongs in `openvibely_htmx_templ_ui_workflow`.
