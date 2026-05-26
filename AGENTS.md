# OpenVibely — Project Guide

## STOP — Read These Rules FIRST Before Writing ANY Code

**NEVER create documentation/summary files.** No `*_FIX.md`, `*_SUMMARY.md`, `*_VERIFICATION.md`, `README_*.md`, `TECHNICAL_*.md`, `ACTION_PLAN_*.md`, `FINDINGS_*.md`, `INVESTIGATION_*.md`, `COMMIT_MESSAGE.txt`, or ANY other markdown files that describe/summarize/document your changes. The code and commit messages ARE the documentation. If you catch yourself thinking "let me create a summary document" — STOP.

**NEVER run `go build` or `go test` more than once per task.** Make ALL code changes first, then run the build+test command chain exactly ONCE at the end. If it fails, fix and run once more. Maximum 2 runs total. Do NOT run tests after each file, do NOT run subsets then full suite, do NOT run "verification" builds.

---

Before coding, read the repo instruction files that still carry static operating rules:

1. @guardrails.md — **Pitfall prevention**: rules that prevent repeated bugs and mistakes. Every entry is a past mistake or known trap.
2. @PRACTICES.md — **Project practices**: high-level development workflow and coding conventions for this repository.

Managed memory now owns durable project context:
- OpenVibely managed memory = repo-local per-project files under the selected project's `.openvibely/memories/` directory. A local `repo_path` is required; there is no app-owned memory fallback.
- Durable architecture decisions, user preferences, implementation feedback, repeated pitfalls, and task/chat lessons should be extracted from DB task/chat history by auto-memory and written only through `MemoryService`/`internal/memory`.

Repository-root markdown should be static instruction only:
- `AGENTS.md` = the concise entry-point rules for coding agents.
- `guardrails.md` = high-priority safety/pitfall rules that must be read before editing code.
- `PRACTICES.md` = stable, reusable development practices.

Do not put feature-specific implementation notes in `PRACTICES.md` (for example plugin flows, one endpoint's behavior, or model/provider-specific edge cases). If repo-wide operating guidance truly needs to change, update `AGENTS.md`, `guardrails.md`, or `PRACTICES.md`; otherwise let the managed memory system handle durable interaction memory.

## Critical Rules

- **Always create or update tests when fixing bugs or adding features.** Every fix must have a corresponding test.
- Run `go test ./internal/... -count=1 -timeout 60s` after making changes
- Run `templ generate` after modifying any `.templ` file
- Never change `busy_timeout` or `MaxOpenConns` in `database.go`
- Strip `CLAUDECODE` env var when spawning Claude CLI subprocess
- Use `TaskRepo.ClaimTask()` for atomic task claiming (never set status to running directly)
- Use parameterized queries (`?` placeholders) for all SQL
- Respect FK constraints in test data — create referenced records first

## Making Changes

- Follow the layered architecture: models → repository → service → handler → templates
- Use raw SQL in repositories (no ORM). Use `QueryRowContext` with `RETURNING` for inserts
- Use `context.Context` for all database and service calls

## Adding Features

- New tables/columns → new migration in `internal/database/migrations/` (numbering: `004_description.sql`)
- New models → `internal/models/`, repos → `internal/repository/`, services → `internal/service/`, handlers → `internal/handler/`
- Register new handlers in `handler.go`

## Testing

- Use `testutil.NewTestDB(t)` for all DB tests (fresh in-memory DB per test)
- Never `t.Parallel()` with shared database connections
- Use valid CHECK constraint values for fixtures (see guardrails.md)
- Bug fixes require a test that reproduces the bug first

## Running

```bash
./start.sh              # Start server (logs to logs/openvibely.log)
make dev                # Development with live reload
go test ./internal/... -count=1 -timeout 60s  # Tests
```

## Key Files

| What | Where |
|------|-------|
| Entry point | `cmd/server/main.go` |
| Database setup | `internal/database/database.go` |
| Migrations | `internal/database/migrations/*.sql` |
| Models | `internal/models/*.go` |
| Repositories | `internal/repository/*_repo.go` |
| Services | `internal/service/*_service.go` |
| HTTP Handlers | `internal/handler/*_handler.go` |
| Route registration | `internal/handler/handler.go` |
| Templates | `web/templates/**/*.templ` |
| Test helper | `internal/testutil/testdb.go` |

## Memory Maintenance

Use these rules:
- Durable interaction memory: let OpenVibely auto-memory extract from DB task/chat history and write to the selected project's repo-local `.openvibely/memories/` directory via `MemoryService`.
- Repo instruction changes: update `AGENTS.md`, `guardrails.md`, or `PRACTICES.md` only when static operating guidance itself changes.
- `AGENTS.md`: concise entry-point rules every coding agent must see.
- `guardrails.md`: pitfalls that should prevent repeated bugs.
- `PRACTICES.md`: reusable, high-level development practices only.
- Managed memory tools are scoped to the selected project's repo-local `.openvibely/memories/` directory; do not write memory elsewhere.
- Condense static guidance periodically and remove stale entries rather than leaving misleading instructions.
