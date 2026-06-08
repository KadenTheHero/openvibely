---
name: realtime_and_frontend_patterns
type: project
created: 2026-05-09
updated: 2026-06-08
source: task
source_id: e321a14e8e7ab207878350bb9e2f4068
confidence: high
title: Realtime and Frontend Patterns
---

OpenVibely uses server-rendered HTMX/templ UI with shared SSE-style live updates. The broad UI contract is: SSE announces that something changed, and HTMX/server-rendered templ fragments provide authoritative state.

Realtime facts:
- The shared `/events/live` stream multiplexes task, chat, and file-change events.
- Sidebar-managed client code fans shared live events into browser `CustomEvent`s such as `sse-task-event`, `sse-chat-live-event`, `sse-file-change-event`, and `sse-live-connected`.
- `window._tabVisibility` owns broad realtime connection visibility behavior and pauses polling while hidden.
- Per-execution `/events/chat/:exec_id` streams are the token-style output path for high-frequency execution output.
- Streamed output is persistence-first so reconnect/refresh can resume from execution rows.
- Real-time file changes stream to the Changes tab during task execution via SSE using worktree diff snapshots that include committed branch changes and uncommitted work.
- Live diff snapshot UI indicators are runtime feedback, not durable task artifacts.

Task Changes and diff facts:
- Task detail lazily loads Changes tab content unless `tab=changes` is active.
- Diff viewer uses GitHub-style load envelopes and oversized-file placeholders rather than eagerly mounting all diff DOM.
- Deleted files render as normal file cards with deletion summaries where needed.
- Renamed/copied file headers render old and new paths in a constrained multi-line from/to layout so long nested paths wrap inside the diff card; normal single-path file headers remain compact/truncated.
- Direct `?tab=changes` renders remain equivalent to lazy route behavior for stateful Changes features such as worktree merge controls.

Chat/thread rendering facts:
- Chat and task-thread streaming batch DOM rendering and force a final flush on completion.
- Queued and steering-pending Chat/task-thread messages render as compact composer/input-box rows rather than transcript bubbles.
- Promoted queued task-thread runs are discovered through live events that remove stale pending rows, append promoted execution fragments, and attach execution streams.
- Prepared/in-flight steering rows should disappear from pending/composer UI on live events and page refreshes even if their durable `thread_inputs` status is still `pending`.
- Long Chat and Task Thread histories remain complete in the database but are server-windowed in the UI with scroll-top pagination.
- Chat and Task Thread default to a visible window of 30 executions/interactions, with older pages also loading up to 30 by default and request `limit` capped at 100; the count is by execution/turn, not by individual rendered bubbles.
- Existing long task threads are backwards compatible with the windowing strategy: no DB migration or cleanup is required, and old histories become windowed retroactively once the task thread/chat content is re-rendered through the new handlers/templates; already-open tabs that loaded the old full DOM may need refresh or an HTMX reload to benefit.
- Browser-memory protection depends on removing old transcript execution DOM nodes from the visible window, not hiding them with CSS; whole execution pairs use removable wrappers so live pages do not grow indefinitely.
- Earlier Chat/Task Thread pages are fetched from the server with bounded `limit`/`before` routes and prepended with scroll-anchor preservation; the UI should not auto-fetch older history on initial render just because the latest window is short. The accepted prepend behavior preserves the user's previous viewport by recording bottom distance/scroll metrics before the older page inserts and restoring after HTMX settle, so newly loaded older messages appear above where the user was reading and can be scrolled through before the next top-load cycle. Because the earlier-page loader is swapped with `hx-swap="outerHTML show:none"`, prepend anchor metadata must live on the stable messages container and lifecycle handling should be bound from stable HTMX events such as `document.body` listeners rather than relying only on the replaced loader element.
- Chat initial render, task-thread initial render, and HTMX swap paths should bind `initChatEarlierLoader`; the scroll-top loader should respond to real top-of-container user intent via scroll, wheel-up, touch-drag-down, and global keyboard navigation without eager-fetching older history on initialization.
- Earlier-page loading should be gesture-latched and request/swap-aware: one command-arrow/scroll/wheel/touch/key gesture at the top loads at most one older page, then requires a new gesture before loading another page. The latch separates consumed-gesture state from HTMX request-in-flight state so programmatic scroll anchoring after a prepend cannot cascade additional older-page loads while the viewport remains pinned at the top. Wheel gestures require an idle gap before counting as a new gesture, touch resets on a new `touchstart`, repeated keyboard events from a held key are ignored, and anchor-restoration scroll events after prepends should not unlock or trigger the next page.
- The earlier-page sentinel should show idle copy by default, show the “Loading earlier messages...” spinner/copy only while a request is active, and return to idle after HTMX completion/failure so users do not see a perpetual loading state after one page prepends. If a stale request-in-flight flag leaves an idle sentinel visible but inert, a real subsequent wheel/touch/key top gesture should recover that stale idle state rather than staying stuck or auto-cascading.
- Chat/Task Thread execution-pair wrappers should preserve equal visible vertical spacing between adjacent user input and assistant response bubbles and between one assistant response and the next user input while still grouping the pair for pruning; the accepted fix used the same vertical gap inside execution pairs as the parent message stack.
- Active chat/task-thread streaming uses smart autoscroll semantics: pinned viewers follow growth, while upward user movement is intent to read.
- Chat/thread markdown rendering escapes raw HTML-like tags outside fenced/inline code before markdown parsing.
- Plan-mode read-only repo exploration tool cards remain visible during live streams and refreshes.

Task detail and shared UI facts:
- Task detail Details tab keeps Prompt, Goal, and Git Worktree in that order with matching card containers.
- Goal edit controls belong in the task edit dialog; verified-state Git worktree actions can remain on the details surface.
- Schedule UI surfaces should clearly distinguish disabled schedules: task detail schedule cards expose a Paused/Resume action and disabled badge/state, while the Schedule page renders disabled items as paused/non-draggable greyed cards with legend support.
- Chat/thread/task-result links share global link token `--ov-link-color: #7480ff`.
- Left sidebar navigation preserves hover-only highlight behavior unless the product intentionally redesigns selected nav state.
- `/models` uses `LLMConfig`/`agent_configs`; `/agents` is plugin-first and has no `color` field.
- `Managed Memory` as a tool/profile is presented as a scoped memory-file capability, not broad repo read/write access.
- Toast rendering accounts for native dialog top-layer behavior.

Operational implementation guidance for HTMX/templ, SSE, streaming DOM updates, diff rendering, task-detail layout, Skills UI, and frontend regressions belongs in `.openvibely/skills/openvibely_htmx_templ_ui_workflow/SKILL.md`.
