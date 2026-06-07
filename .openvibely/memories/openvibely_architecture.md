---
name: openvibely_architecture
type: project
created: 2026-05-09
updated: 2026-06-07
source: consolidation
source_id: memory_consolidation_2026_06_07
confidence: high
title: OpenVibely Architecture
---

OpenVibely is an open-source Go application for automated task scheduling and AI-powered execution. Users create tasks, schedule them, and have LLM agents execute them automatically.

The backend uses Echo v4, SQLite via `modernc.org/sqlite`, goose migrations, and a channel-based worker pool. The frontend is server-rendered with HTMX, templ, Tailwind CSS, and DaisyUI.

Dual mode architecture:
- `internal/server.Start(ctx, cfg)` wires the shared backend and returns a server instance with bound address, base URL, and shutdown handle.
- Local web/server and desktop release-binary runs default DB/repos/uploads and related runtime state to the same user app-data directory, specifically `$HOME/.openvibely` unless an env override applies.
- Hosted/Docker deployments use explicit env-driven storage such as mounted `/data` paths rather than local `$HOME/.openvibely` behavior.
- Desktop mode (`cmd/desktop`) uses `config.LoadWithMode(ModeDesktop)`, uses ephemeral port `PORT=0`, enables local repo paths, and loads the Wails WebView from the server base URL.
- `OPENVIBELY_APP_DATA_DIR` is the shared override for the local app-data root when users need both web/server and desktop to point at the same runtime state; explicit overrides bypass legacy-path migration into another app-data default.
- Env vars override mode defaults. Users who set `DATABASE_PATH`, `PROJECT_REPO_ROOT`, or related storage env vars get those explicit paths.
- Desktop defaults to localhost OAuth callback flow (`APP_BASE_URL` unset). Server/VPS mode uses `APP_BASE_URL` for hosted callbacks.
- Desktop/Wails GUI launches, especially on macOS, may not inherit the user's interactive shell `PATH`; task execution relies on centralized environment/PATH construction rather than hardcoded developer-tool paths.
- Desktop external-link handling depends on the actual loaded Wails runtime API because the desktop WebView may be loaded from the local server URL rather than a `wails:` page.
- Desktop file/folder selection UX favors Wails/native dialog APIs because browser-only upload features such as `webkitdirectory` are not consistently reliable across OS-native WebViews.
- Server and desktop share `internal/server`; backend forking is not part of the intended architecture.

Storage and runtime-state pitfalls:
- Storage changes maintain compatibility across Docker/VPS, local server, and desktop deployments.
- Docker/VPS persist under mounted `/data` paths where applicable.
- Local storage migrations preserve existing user state by moving/copying the old local database, SQLite sidecars such as WAL/SHM files, repos, uploads, and related runtime directories into `$HOME/.openvibely` when no explicit storage override is set.
- Release binaries use stable app-owned storage rather than source-checkout/current-working-directory paths such as `./openvibely.db` or `./repos`.
- Web/server and desktop local runs are expected to use the same database by default unless env vars explicitly separate them.
- Local runtime-state diagnosis depends on the active process, port, and database path because multiple local/server/desktop instances may use different configured storage roots.

OAuth and base URLs:
- Model OAuth initiate/callback resolves absolute app URLs through shared URL-building behavior: `APP_BASE_URL` first, then forwarded/request host fallback.
- Hosted deployments use `APP_BASE_URL` so Anthropic/OpenAI OAuth redirects stay on the public hostname.
- Without `APP_BASE_URL`, OAuth keeps localhost callback-server behavior for local development.

Shared conventions:
- `internal/handler` is the Echo HTTP boundary: `handler.go` owns the shared `Handler` dependency graph and route registration, while feature-specific files attach methods for tasks, projects, chat, models, auth, integrations, SSE, worktrees, and related HTMX/API surfaces.
- Templates and JS reuse existing shared helpers/utilities and feature-detect optional elements.
- UI code uses existing components/partials and shared CSS tokens instead of duplicating styles.
- Swagger/OpenAPI is generated with `swag init`; generated docs live under `docs/` and the UI is mounted at `/swagger/*`.
- User-facing documentation also lives under `docs/`, generally as concise `*-user-guide.md` Markdown pages that match the existing guide structure. README editing preferences and docs-site positioning live in `coding_agent_product_discipline.md`.

Runtime logging:
- OpenVibely uses `internal/applog` as the runtime logging facade: `Infof` emits operational logs, while `Debugf` is gated by `OPENVIBELY_LOG_LEVEL=debug` and defaults off at info level.
- `start.sh` defaults `OPENVIBELY_LOG_LEVEL` to `info` while preserving env or `.env` overrides.
- App logging across internal/pkg/cmd code should go through `applog` (`Infof` or `Debugf`) unless a component intentionally owns a `*log.Logger` dependency or uses fatal/setup-only standard-log behavior such as `log.SetOutput`/`log.Fatalf`.
- Raw LLM/user content, high-frequency stream/SSE/diff/poll/routing traces, and OpenAI/Anthropic provider header or rate-limit dumps are debug-level diagnostics, not info logs. Very hot or low-value traces may remain as commented `applog.Debugf` examples to avoid call overhead.
- Streaming persistence errors and final one-time stream lifecycle summaries can remain info-level, but periodic/eager streaming flush success counters during active generation are debug-level noise.
