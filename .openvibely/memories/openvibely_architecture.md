---
name: openvibely_architecture
type: project
created: 2026-05-09
updated: 2026-06-05
source: consolidation
source_id: memory_consolidation_2026_06_05
confidence: high
title: OpenVibely Architecture
---

OpenVibely is an open-source Go application for automated task scheduling and AI-powered execution. Users create tasks, schedule them, and have LLM agents execute them automatically.

The backend uses Echo v4, SQLite via `modernc.org/sqlite`, goose migrations, and a channel-based worker pool. The frontend is server-rendered with HTMX, templ, Tailwind CSS, and DaisyUI.

Dual mode architecture:
- `internal/server.Start(ctx, cfg)` wires the shared backend and returns a server instance with bound address, base URL, and shutdown handle.
- Local web/server and desktop release-binary runs should default DB/repos/uploads and related runtime state to the same user app-data directory, specifically `$HOME/.openvibely` unless an env override applies, not separate web/desktop directories, the source checkout, or the current working directory.
- Hosted/Docker deployments should still use explicit env-driven storage such as mounted `/data` paths and should not silently fall back to local `$HOME/.openvibely` behavior when the image is meant to persist under `/data`.
- Desktop mode (`cmd/desktop`) uses `config.LoadWithMode(ModeDesktop)`, uses ephemeral port `PORT=0`, enables local repo paths, and loads the Wails WebView from the server base URL.
- `OPENVIBELY_APP_DATA_DIR` is the shared override for the local app-data root when users need both web/server and desktop to point at the same runtime state; if set, use it directly and do not run legacy-path migration into another app-data default.
- Env vars override mode defaults. Users who set `DATABASE_PATH`, `PROJECT_REPO_ROOT`, or related storage env vars get those explicit paths.
- Desktop defaults to localhost OAuth callback flow (`APP_BASE_URL` unset). Server/VPS mode should set `APP_BASE_URL` for hosted callbacks.
- Desktop/Wails GUI launches, especially on macOS, may not inherit the user's interactive shell `PATH`; task execution must use centralized environment/PATH construction. Do not hardcode assumed developer-tool install paths; derive or merge the user's real initialized shell `PATH` for desktop task execution without forking per-command behavior.
- Desktop external-link handling should not assume desktop-mode detection means the Wails browser bridge is ready. Feature-detect the actual loaded runtime API before calling it, and remember the desktop WebView may be loaded from the local server URL rather than a `wails:` page.
- Desktop file/folder selection UX should prefer Wails/native dialog APIs for reliability; browser-only upload features such as `webkitdirectory` are not guaranteed consistently across OS-native WebViews.
- Do not fork backend code between server and desktop; both modes share `internal/server`.

Storage and runtime-state pitfalls:
- Storage changes must remain compatible across Docker/VPS, local server, and desktop deployments.
- Docker/VPS should persist under mounted `/data` paths where applicable.
- Local storage migrations should preserve existing user state by moving/copying the old local database, SQLite sidecars such as WAL/SHM files, repos, uploads, and related runtime directories from previous app/runtime defaults into the new `$HOME/.openvibely` location when no explicit `OPENVIBELY_APP_DATA_DIR` or storage env override is set.
- Do not reintroduce defaults that put local server DB/repos in `./openvibely.db`, `./repos`, or another project/current-working-directory path; release binaries should have stable app-owned storage.
- Web/server and desktop local runs are expected to use the same database by default unless env vars explicitly separate them.
- When debugging local runtime state, verify the active process, port, and database path before assuming a particular local database location.

OAuth and base URLs:
- Model OAuth initiate/callback resolves absolute app URLs through shared URL-building behavior: `APP_BASE_URL` first, then forwarded/request host fallback.
- Hosted deployments should set `APP_BASE_URL` so Anthropic/OpenAI OAuth redirects stay on the public hostname.
- Without `APP_BASE_URL`, OAuth keeps localhost callback-server behavior for local development.

Shared conventions:
- `internal/handler` is the Echo HTTP boundary: `handler.go` owns the shared `Handler` dependency graph and route registration, while feature-specific files attach methods for tasks, projects, chat, models, auth, integrations, SSE, worktrees, and related HTMX/API surfaces.
- Do not redeclare shared helpers in templates or JS; reuse existing utilities and feature-detect optional elements.
- Use existing components/partials and shared CSS tokens instead of duplicating styles.
- Swagger/OpenAPI is generated with `swag init`; generated docs live under `docs/` and the UI is mounted at `/swagger/*`.
- User-facing documentation also lives under `docs/`, generally as concise `*-user-guide.md` Markdown pages that match the existing guide structure. README editing preferences and docs-site positioning live in `coding_agent_product_discipline.md`.
