---
name: realtime_and_frontend_patterns
type: project
created: 2026-05-09
updated: 2026-05-30
source: after_complete
source_id: 25cb5b146d8d578c0885e4e26761bb74
confidence: high
title: Realtime and Frontend Patterns
---

OpenVibely uses server-rendered HTMX/templ UI with shared SSE-style live updates. Prefer one shared per-tab sidebar-managed SSE stream with in-browser event fan-out rather than opening separate long-lived EventSources for each surface.

Realtime and diff updates:
- Real-time file changes stream to the Changes tab during task execution via SSE using `GetWorktreeDiffWithUncommitted` to show committed branch changes and uncommitted work without auto-committing.
- Task-thread follow-up executions should run the same periodic diff snapshot broadcast path, persisting `executions.diff_output` and publishing `diff_snapshot` events.
- Treat live diff snapshot UI indicators as runtime feedback, not durable task artifacts; they may disappear after app restart, while persisted execution diff/task changes are the source of durable review state.
- Changes-tab scroll preservation: SSE diff updates should fetch offscreen DOM, compare fingerprints, and skip live DOM mutation when unchanged. When content changes, save/restore window scroll and active diff mode via `requestAnimationFrame`; preserve `window._diffFileState` file expand/collapse state.
- Avoid `htmx.ajax()` live swaps for frequent diff refreshes when a fingerprint gate is needed; it can remount DOM before the no-op check.

Task Changes rendering safety:
- Task detail lazily loads Changes tab content unless `tab=changes` is active, so Thread/Details do not pre-render heavy hidden diff DOM.
- Diff viewer uses GitHub-style load envelopes and renders oversized files as explicit `Load diff` or non-loadable placeholders rather than eagerly mounting all diff DOM. Placeholder/non-rendered file entries such as large-file and hard-blocked diffs should still reuse the shared file-card header/body collapse contract so chevrons, `data-diff-toggle`, body IDs, and `window._diffFileState` restoration behave like normal rendered diffs without mounting heavy tables. Keep padding/background/content spacing on an inner placeholder wrapper, not the collapsible `diff-file-body`, so `max-height: 0` fully collapses non-rendered entries without residual visible padding.
- Diff parsing should synthesize a fallback hunk when diff content lines exist without an explicit `@@` header.
- Task Changes file-header addition/deletion counts should visually align with the diff card add/delete palette, not raw DaisyUI `oklch()` semantic tokens or a separate hardcoded palette. Keep diff card fills at the user-specified colors: dark mode add `#1E3A38` and delete `#3D2C34`, light mode add `#DDEDE0` and delete `#FAE3E1`. Header `+/-` numbers should be text-only with no chip/background styling and use stronger-but-muted foregrounds: dark add `#559B70`, dark delete `#BD7076`, light add `#317A4A`, light delete `#A65353`. Keep desktop-WebView-safe plain color/RGBA variables for shared diff rendering.
- Deleted files in task diffs render as normal file cards with a `Deleted` status badge; textual deletions show removed-line hunks, while deleted binary/empty files without hunks show centered summary text.
- Live diff refreshes are gated by active-tab checks. Task-detail file-change listeners/SSE handlers are explicitly rebound with cleanup so HTMX swaps do not accumulate stale listeners or leave SSE running after navigation.
- Task completion on the detail page should update in place through live update/HTMX/SSE mechanisms; avoid hard/full browser refreshes after completion so active tab, scroll position, and context remain stable.
- Task-thread SSE completion should finish dynamically in place: force the final streamed render, clear streaming indicators/state, and avoid post-stream reconciliation refreshes of either `#task-thread-view` or the whole task shell.
- Thread tab content is lazy-loaded via `GET /tasks/:id/thread`; heavy execution transcripts should not be pre-rendered in hidden tabs.

Chat and thread rendering:
- For streaming chat and tool cards, batch DOM rendering with `requestAnimationFrame` and force final flush on completion.
- Active chat/task-thread streaming should use shared smart autoscroll behavior when present: record whether the viewport was pinned before content growth, then only scroll after rendering if it was pinned. Upward user movement is intent to read; do not force-scroll back down until the user returns to the bottom or initiates a new send. For large conversations, avoid clearing scroll intent from programmatic/clamp scroll events during streaming rerenders; derive intent from real user interactions such as wheel/touch/key/pointer and keep pointer/scrollbar drags active until pointerup/cancel/blur. Streaming code should resolve/rebind scroll trackers when HTMX/morph swaps replace or detach message containers so smart scrolling recovers without refresh.
- Chat/thread attachment images can increase message height after HTMX swaps when screenshots load. Render image attachments with a stable marker and bind shared image load/error smart-scroll correction on initial render and relevant swaps, snapshotting pinned state before layout growth and scrolling only when the tracker allows it.
- Task-thread tab/task navigation should keep per-thread scroll state: returning from Details/Changes or another task should restore remembered position or bottom-align only when the prior thread state was pinned, on fresh initial entry, or for new send/active stream activity. Do not solve navigation scroll bugs with hard remounts or full refreshes that reset task context.
- Task-thread streaming must keep its HTMX polling fallback resumable across lifecycle hook/status transitions: reactivated/resumed streams should reset stale inactive markers and preserve a valid `/tasks/:id/thread` poll URL/trigger so live updates continue if the per-execution EventSource closes or errors.
- Avoid expensive full-container reprocessing on polling refreshes; use content signatures and incremental cleaning.
- Chat/thread markdown rendering escapes raw HTML-like tags outside fenced/inline code before `marked.parse` so malformed model outputs do not break DOM.
- Avoid destructive `/chat` `outerHTML` history refreshes on tab refocus or SSE reconnect when chat history is already loaded, including after hard refresh where static chat bubble markup is present.
- Plan-mode read-only repo exploration tool cards should remain visible in assistant bubble rendering during live streams and refreshes.
- Chat mode selector hydration should use hidden input + localStorage restore and mark hydration state before evaluating mode-dependent UI; detailed plan-handoff rules live in `chat_thread_system.md`.

Shared UI/page patterns:
- For HTMX dropdown/menu actions, distinguish user reports of “spinner/toast but then failure” from “click did nothing.” If there is no spinner, toast, or menu close, first suspect that the HTMX request never fired or was not bound. Check rendered attributes, lazy-loaded HTMX processing, and whether menu `<button>` elements are inside a parent `<form>` without `type="button"`.
- Chat/thread markdown links and task-result links share global link token `--ov-link-color: #7480ff`, with hover/focus/active/visited states.
- Left sidebar navigation should preserve the original hover-only highlight behavior; avoid persistent selected-item highlight classes/scripts unless the product intentionally redesigns nav active state.
- Schedule timeline/current-time UI should use shared tokens instead of hardcoded green for light/dark consistency.
- Schedule `Run At` controls should expose click-anywhere picker behavior using `showPicker()` with focus fallback while preserving keyboard entry.
- Schedule repeat controls should stay parity-aligned across `/schedule` create modal and task-detail schedule forms. New Scheduled Task defaults Repeat to Daily and treats missing `repeat_type` in schedule-page create submissions as `daily` server-side.
- `/personality` uses card-list UX; Base personality is pinned first, labelled Base, never active-ring highlighted, and Base kebab behavior differs depending on selection.
- `/workers` uses a single Worker Capacity & Utilization table-style card with global row pinned first. Worker-limit inputs use dirty-state highlighting only while editing and suppress dirty restore after successful Set during immediate swaps.
- `/models` uses `LLMConfig`/`agent_configs`; Default badge uses shared `ov-badge-default` style.
- `/agents` is plugin-first with modal marketplace/install state, generate flow, plugin endpoints, and no `color` field/UI.
- Agent tool-selection UI should avoid ambiguous aggregate labels. If a capability like `Read` includes multiple concrete tools, expose enough detail in labels/help text so users understand the granted operations. If `Managed Memory` appears as a tool/profile, make clear it is a scoped memory-file capability rather than arbitrary normal repo read/write access.
- Toast rendering must account for native dialog top-layer behavior by re-hosting toasts into the active modal dialog when needed.
- Alert/banner inline values such as branch names should inherit the alert text color in dark mode; avoid themed surface backgrounds on inline `<code>` inside colored alerts because contrast can become unreadable.
