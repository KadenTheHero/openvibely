---
name: integrations_and_channels
type: project
created: 2026-05-09
updated: 2026-05-29
source: consolidation
source_id: memory_consolidation_2026_05_29
confidence: high
title: Integrations and Channels
---

OpenVibely has channel integrations for GitHub, Slack, Telegram, and generic inbound webhooks. Integration UIs should separate discovery/add flows from management cards, render explicit connection states, and keep destructive action language consistent as `Delete` with confirmation.

Channels page:
- `/channels` uses an Add Channel chooser (`GitHub`, `Slack`, `Telegram Bot`, `Webhook`) and only renders active cards after a channel is added/configured.
- Channel-card kebab menus use consistent destructive actions: `Delete`, `text-error`, provider-specific confirmation copy.
- Telegram card includes first-class delete action `POST /channels/telegram/remove`.

GitHub:
- Default/recommended auth mode is PAT (`github_auth_mode=pat`) for local/self-hosted OSS installs. GitHub App mode (`github_auth_mode=app`) is Advanced for cloud deployments.
- GitHub operations such as clone, push, startup fetch, and PR creation should mint/use operation tokens by mode: PAT directly in PAT mode; installation access tokens in App mode. Installation tokens are ephemeral and never persisted.
- Active GitHub card supports mode-aware App connect/callback/disconnect plus kebab edit/remove; PAT mode shows connected status/details without token-specific inline actions.
- GitHub edit dialog pre-fills stored PAT/private key values masked by default; users reveal explicitly via eye toggles.
- Projects support `repo_url` in addition to `repo_path`. Local Path availability is controlled only by `OPENVIBELY_ENABLE_LOCAL_REPO_PATH`; unset/invalid defaults to GitHub-only mode.
- GitHub URL projects still store/use `repo_path` as the local managed clone path; `repo_url` is the remote source. Runtime repo/memory/shared-repo logic should read `repo_path` for filesystem access.
- GitHub URL mode clones into managed storage (`PROJECT_REPO_ROOT`, default `./repos`, Docker default `/data/repos`) and Edit performs re-clone swap behavior.
- When local mode is disabled, legacy local-path projects still show a source selector with both Local Path (existing) and GitHub URL so users can migrate.
- Git operations auto-detect system SSL CA bundles and fall back to `GIT_SSL_NO_VERIFY=true` if no valid bundle is found; users can override with `GIT_SSL_CAINFO` or explicit `GIT_SSL_NO_VERIFY`.
- Project create/edit GitHub clone failures return HTMX toast guardrails (`openvibelyToast`) instead of raw error payloads.
- Task Changes tab supports one-click PR creation, one PR per task, and reuse of existing task/remote-branch PRs.
- In Wails desktop mode, Task Changes `View PR` should open GitHub PR URLs in the system browser rather than navigating the local WebView; preserve existing web/server behavior and surface a clear error if no PR URL is available.
- Merge/PR action menus group Local and GitHub sections. Toasts are destination-prefixed such as `Merged locally into <branch>` or `GitHub PR created (#N)`.
- Failed tasks keep Local merge actions visible when additional uncommitted edits need reconciliation, even if stored merge status says merged.

Generic inbound webhooks:
- Inbound route is `POST /webhooks/inbound/:pathToken` with auth via `X-Webhook-Secret` constant-time compare or `X-Hub-Signature-256` HMAC-SHA256. Body limit is 1MB.
- Handler must guard missing task repository dependency and return `500 {"error":"internal error"}` instead of nil-pointer panic if wiring is incomplete.
- Each webhook call creates exactly one active pending task with `created_via=webhook`. Primary agent is the first selected endpoint agent; all selected agents persist for future multi-agent runtime.
- Payload normalization extracts `event_type` and `summary` from common field names and embeds structured raw JSON in the task prompt.
- Backend supports title/prompt template variables `{{event_type}}`, `{{summary}}`, and `{{name}}`, though UI may hide template fields.
- CRUD routes live under `/channels/webhooks`, including create, edit, delete, rotate-secret, and test.
- Webhook cards should not render raw inbound endpoint URL text; expose `Copy URL` action with toast success/failure. Status row shows only `Active`/`Disabled` badge using the same class ordering as other channel cards.

Slack:
- Slack OAuth and manual bot-token modes should stay separate so switching modes does not wipe working credentials.
- Slack inbound behavior requires Socket Mode plus `app_mention` and `message.im` events.
- Authorized-user enforcement is project-scoped and allow-by-default when no authorized users are configured.

Telegram:
- Telegram attachment and command behavior should remain project-aware.
- Tests should not spill runtime upload files into package directories.
