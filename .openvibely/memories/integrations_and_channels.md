---
name: integrations_and_channels
type: project
created: 2026-05-09
updated: 2026-06-01
source: after_complete
source_id: f076cd4c16ee53c0a0e05418c388f12f
confidence: high
title: Integrations and Channels
---

OpenVibely has channel integrations for GitHub, Slack, Telegram, and generic inbound webhooks. Integration UIs should separate discovery/add flows from management cards, render explicit connection states, and keep destructive action language consistent as `Delete` with confirmation.

Channels page:
- `/channels` uses an Add Channel chooser (`GitHub`, `Slack`, `Telegram Bot`, `Webhook`) and only renders active cards after a channel is added/configured.
- Channel-card kebab menus use consistent destructive actions: `Delete`, `text-error`, and provider-specific confirmation copy.
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
- Slack socket processing should start only after the pending-input repository, queued-turn promoter, shared channel chat runner, and shared channel task runner callbacks are wired; otherwise early inbound messages can fall back to divergent local behavior.
- Slack-origin active Chat runs should hand initial responses to the shared steering-aware chat runner in production, not a service-local LLM loop, so web Chat steering can be consumed. Immediate Slack chat handoff must persist `SlackTaskContext` before execution creation; if persistence fails, clean up the chat task.
- Queued Slack input promotion should persist `SlackTaskContext` in the same transaction that claims the pending input and creates the promoted task/execution, so a queued row cannot become applied and start without reply metadata.
- Slack `send_to_task` follow-ups use the shared queued task-thread behavior from `chat_thread_system.md`, carry reply metadata on `thread_inputs` or per-run context, use handler-resolved task worktrees, keep marker processing disabled, and should not rewrite the target task origin just to route a reply.

Telegram:
- Telegram attachment and command behavior should remain project-aware.
- Tests should not spill runtime upload files into package directories.
- Telegram services created or restarted from settings must wire the shared channel chat runner before `Start()`; otherwise settings-enabled bots can fall back to service-local LLM loops even if server startup wiring is correct. Startup-created and settings-created Telegram services should also set AgentRepo before `Start()` so channel orchestration can advertise and resolve explicit Agent-definition names consistently from the first inbound message. Settings-created Telegram services must also wire the task-thread queued promoter, not just the chat queued promoter, so `send_to_task` can promote idle pending task-thread follow-ups after appending behind them.
- Telegram-origin active Chat runs should hand initial responses to the shared steering-aware chat runner in production, preserve the initial “Thinking…” acknowledgement message id, and promote queued follow-ups after both success and failure.
- For shared-runner Telegram Chat, the shared runner owns worker cancellation registration; the Telegram service should register/deregister cancellation only on the fallback local-LLM path so deferred service cleanup cannot remove the handler-owned cancel function after handoff.
- Telegram `send_to_task` follow-ups use the shared queued task-thread behavior from `chat_thread_system.md`, carry reply metadata on `thread_inputs` or per-run context, use handler-resolved task worktrees, keep marker processing disabled, and should not rewrite the target task origin just to route a reply.
- Telegram `Start`/`Stop` should be nil-safe for partially constructed/test services.
