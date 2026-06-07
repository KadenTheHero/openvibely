---
name: realtime_and_frontend_patterns
type: project
created: 2026-05-09
updated: 2026-06-07
source: task
source_id: 21a1d5d33de4634395174cd1f74bb002
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
- Direct `?tab=changes` renders remain equivalent to lazy route behavior for stateful Changes features such as worktree merge controls.

Chat/thread rendering facts:
- Chat and task-thread streaming batch DOM rendering and force a final flush on completion.
- Queued and steering-pending Chat/task-thread messages render as compact composer/input-box rows rather than transcript bubbles.
- Promoted queued task-thread runs are discovered through live events that remove stale pending rows, append promoted execution fragments, and attach execution streams.
- Prepared/in-flight steering rows should disappear from pending/composer UI on live events and page refreshes even if their durable `thread_inputs` status is still `pending`.
- Long Chat and Task Thread histories remain complete in the database but are server-windowed in the UI with scroll-top pagination.
- Browser-memory protection depends on removing old transcript execution DOM nodes from the visible window, not hiding them with CSS; whole execution pairs use removable wrappers so live pages do not grow indefinitely.
- Earlier Chat/Task Thread pages are fetched from the server with bounded `limit`/`before` routes and prepended with scroll-anchor preservation; the UI should not auto-fetch older history on initial render just because the latest window is short.
- Chat initial render, task-thread initial render, and HTMX swap paths should bind `initChatEarlierLoader`; the scroll-top loader should respond to real top-of-container user intent via scroll, wheel-up, touch-drag-down, and global keyboard navigation without eager-fetching older history on initialization.
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
