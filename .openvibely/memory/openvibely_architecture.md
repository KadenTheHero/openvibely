---
name: openvibely_architecture
type: project
created: 2026-05-09
updated: 2026-05-14
source: consolidation
source_id: memory_consolidation_2026_05_14
confidence: high
title: OpenVibely Architecture
---

OpenVibely is an open-source Go application for automated task scheduling and AI-powered execution. Users create tasks, schedule them, and have LLM agents execute them automatically.

The backend uses Echo v4, SQLite via `modernc.org/sqlite`, goose migrations, and a channel-based worker pool. The frontend is server-rendered with HTMX, templ, Tailwind CSS, and DaisyUI.

Code structure from the legacy root memory:
- `cmd/server`: server entrypoint.
- `cmd/desktop`: Wails desktop entrypoint.
- `internal/server`: shared backend startup/wiring.
- `internal/config`: mode-aware config defaults/env loading.
- `internal/models`: domain models.
- `internal/repository`: data access.
- `internal/service`: business logic and background services.
- `internal/handler`: Echo HTTP handlers and routes.
- `web/templates`: templ templates.
- `docs`: generated Swagger/OpenAPI docs.

Dual mode architecture:
- `internal/server.Start(ctx, cfg)` wires the full backend and returns a server instance with bound address, base URL, and shutdown handle.
- Server mode (`cmd/server`) uses env-driven defaults such as `PORT=3001`, `DATABASE_PATH=./openvibely.db`, and `PROJECT_REPO_ROOT=./repos`.
- Desktop mode (`cmd/desktop`) uses `config.LoadWithMode(ModeDesktop)`, defaults to OS app-data dirs for DB/repos/uploads, uses ephemeral port `PORT=0`, enables local repo paths, and loads the Wails WebView from the server base URL.
- Env vars override mode defaults. Desktop users who set `DATABASE_PATH` in env get that path.
- Desktop defaults to localhost OAuth callback flow (`APP_BASE_URL` unset). Server/VPS mode should set `APP_BASE_URL` for hosted callbacks.
- Do not fork backend code between server and desktop; both modes share `internal/server`.

Storage/deployment compatibility:
- Memory storage changes must remain backward-compatible for Docker/VPS, local server, and desktop deployments.
- Docker/VPS should persist memory under `/data/memory` when `/data` is mounted.
- Local server should not unexpectedly move an existing `./openvibely.db`.
- Desktop should continue using OS app-data defaults unless the user opts into a shared app-data root.
- When debugging local runtime state, verify the active process, port, and database path before assuming `.openvibely/openvibely.db`. In the 2026-05-09 local server incident, the active latest server on port `3001` was using repo-root `openvibely.db`, which contained the memory extraction runs.

OAuth and base URLs:
- Model OAuth initiate/callback resolves absolute app URLs via shared `buildAbsoluteURL()`: `APP_BASE_URL` first, then forwarded/request host fallback.
- Hosted deployments should set `APP_BASE_URL` so Anthropic/OpenAI OAuth redirects stay on the public hostname.
- Without `APP_BASE_URL`, OAuth keeps localhost callback-server behavior for local development.

Shared conventions:
- Do not redeclare shared helpers in templates or JS; reuse existing utilities and feature-detect optional elements.
- Use existing components/partials and shared CSS tokens instead of duplicating styles.
- Swagger/OpenAPI is generated with `swag init`; generated docs live under `docs/` and the UI is mounted at `/swagger/*`.
- Key dependencies include Echo v4, templ, Tailwind/DaisyUI, modernc SQLite, goose, openai-go, anthropic-sdk-go, Google Gemini SDK, and Wails v2.
