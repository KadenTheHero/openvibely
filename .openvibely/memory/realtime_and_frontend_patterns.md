---
name: realtime_and_frontend_patterns
type: project
created: 2026-05-09
updated: 2026-05-12
source: manual_conversion
source_id: repo_root_MEMORY_md
confidence: high
title: Realtime and frontend patterns
---

OpenVibely uses server-rendered HTMX/templ UI with shared SSE-style live updates. Prefer one shared per-tab sidebar-managed SSE stream with in-browser event fan-out rather than opening separate long-lived EventSources for each surface.

Realtime/diff updates:
- Real-time file changes stream to the Changes tab during task execution via SSE every ~2 seconds using `GetWorktreeDiffWithUncommitted` to show committed branch changes and uncommitted work without auto-committing.
- Task-thread follow-up executions should run the same periodic diff snapshot broadcast path, persisting `executions.diff_output` and publishing `diff_snapshot` events.
- Changes-tab scroll preservation: SSE diff updates should fetch offscreen DOM, compare fingerprints, and skip live DOM mutation when unchanged. When content changes, save/restore window scroll and active diff mode via `requestAnimationFrame`; preserve `window._diffFileState` file expand/collapse state.
- Avoid `htmx.ajax()` live swaps for frequent diff refreshes when a fingerprint gate is needed; it can remount DOM before the no-op check.

Task Changes rendering safety:
- Task detail lazily loads Changes tab content unless `tab=changes` is active, so Thread/Details do not pre-render heavy hidden diff DOM.
- Diff viewer uses GitHub-style load envelopes: max 300 files considered, max total loadable budget 20,000 lines or 1MB raw diff, max single-file budget 20,000 lines or 500KB raw diff, auto-load threshold 400 lines or 20KB per file.
- Files above auto-load threshold render `Load diff` placeholders when still loadable; beyond single-file/total budgets render non-loadable placeholders with reason text.
- Diff parsing synthesizes a fallback hunk when diff content lines exist without explicit `@@` header.
- Live diff refreshes are gated by active-tab checks. Task-detail file-change listeners/SSE handlers are explicitly rebound with cleanup so HTMX swaps do not accumulate stale listeners or leave SSE running after navigation.
- Task completion on the detail page should update in place through live update/HTMX/SSE mechanisms; avoid hard/full browser refreshes after completion so active tab, scroll position, and context remain stable.
- Task-thread SSE completion should finish dynamically in place: force the final streamed render, clear streaming indicators/state, and avoid post-stream `htmx.ajax()` reconciliation/refreshes of either `#task-thread-view` or the whole task shell.
- Thread tab content is also lazy-loaded via `GET /tasks/:id/thread`; heavy execution transcripts should not be pre-rendered in hidden tabs.

Chat/frontend rendering:
- For streaming chat and tool cards, batch DOM rendering with `requestAnimationFrame` and force final flush on completion.
- Active chat/task-thread streaming should use smart autoscroll: only pin to bottom while the viewport is already at/near bottom; if the user scrolls up to read earlier streamed output, do not force-scroll back down until they return to the bottom or initiate a new send.
- Avoid expensive full-container reprocessing on polling refreshes; use content signatures and incremental cleaning.
- Chat/thread markdown rendering escapes raw HTML-like tags outside fenced/inline code before `marked.parse` so malformed model outputs do not break DOM.
- Plan-mode read-only repo exploration tool cards (`read_file`, `list_files`, `grep_search`) should remain visible in assistant bubble rendering during live streams and refreshes.
- Mode selector hydration should use hidden input + localStorage restore, mark hydration state, and re-evaluate plan-completion prompt after restoring mode.

Shared UI/page patterns:
- Chat/thread markdown links and task-result links share global link token `--ov-link-color: #7480ff` in `layout/base.templ`, with hover/focus/active/visited states.
- `/schedule` current-time timeline tracer uses `--ov-link-color` instead of hardcoded green for light/dark consistency.
- Schedule `Run At` controls should expose click-anywhere picker behavior using `showPicker()` with focus fallback while preserving keyboard entry.
- Schedule repeat controls should stay parity-aligned across `/schedule` create modal and task-detail schedule forms: `Repeat Every`, hidden/disabled interval for `once`, whole-number interval validation, consistent `repeat_interval` submit.
- `/schedule` New Scheduled Task defaults Repeat to Daily and treats missing `repeat_type` in schedule-page create submissions as `daily` server-side.
- `/personality` uses card-list UX in `app_settings.templ`; Base personality is pinned first, labelled Base, never active-ring highlighted, and Base kebab behavior differs depending on selection.
- `/workers` uses a single Worker Capacity & Utilization table-style card with global row pinned first. Worker-limit inputs use dirty-state highlighting only while editing and suppress dirty restore after successful Set during immediate swaps.
- `/models` uses `LLMConfig`/`agent_configs`; Default badge uses shared `ov-badge-default` style.
- `/agents` is plugin-first with modal marketplace/install state, generate flow, plugin endpoints, and no `color` field/UI.
- Agent tool-selection UI should avoid ambiguous aggregate labels. If a capability like `Read` includes multiple concrete tools such as single-file read, directory listing, and search, expose enough detail in labels/help text so users can understand the granted operations. If `Managed Memory` appears as a tool/profile, make clear it is a scoped memory-file capability rather than inheriting arbitrary normal `Read`/`Write` repo tools.
- Toast rendering must account for native dialog top-layer behavior by re-hosting toasts into the active modal dialog when needed.
