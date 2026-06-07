---
name: integrations_and_channels
type: project
created: 2026-05-09
updated: 2026-06-07
source: consolidation
source_id: memory_consolidation_2026_06_07
confidence: high
title: Integrations and Channels
---

OpenVibely has channel integrations for GitHub, Slack, Telegram, and generic inbound webhooks. Integration UIs separate discovery/add flows from management cards, render explicit connection states, and keep destructive action language consistent as `Delete` with confirmation.

Durable channel direction:
- Supported channel integrations keep task-goal controls at parity with web/API chat.
- Slack and Telegram runtime goal actions are wired through durable `TaskGoalService` behavior rather than surface-specific stubs.
- Channel-origin Chat and task-thread behavior stays aligned with web/API queueing, steering, cancellation, task-goal, agent-resolution, and selected-memory behavior where the surface supports it.

GitHub facts:
- Default/recommended auth mode is PAT (`github_auth_mode=pat`) for local/self-hosted OSS installs.
- GitHub App mode (`github_auth_mode=app`) is Advanced for cloud deployments.
- GitHub operation tokens are mode-specific: PAT mode uses the PAT directly; App mode mints ephemeral installation access tokens that are not persisted.
- Projects support both `repo_url` and `repo_path`. GitHub URL projects still use `repo_path` as the local managed clone path; `repo_url` is the remote source.
- Local Path availability is controlled by `OPENVIBELY_ENABLE_LOCAL_REPO_PATH`.
- GitHub URL mode clones into managed storage (`PROJECT_REPO_ROOT`, default `./repos`, Docker default `/data/repos`).
- Task Changes supports one-click PR creation, one PR per task, and reuse of existing task/remote-branch PRs.

Webhook facts:
- Inbound route is `POST /webhooks/inbound/:pathToken`.
- Webhook auth supports `X-Webhook-Secret` constant-time compare or `X-Hub-Signature-256` HMAC-SHA256.
- Each webhook call creates one active pending task with `created_via=webhook`.
- CRUD routes live under `/channels/webhooks`, including create, edit, delete, rotate-secret, and test.
- Backend template variables include `{{event_type}}`, `{{summary}}`, and `{{name}}`, though the UI may hide template fields.

Slack facts:
- Slack OAuth and manual bot-token modes are separate so switching modes does not wipe working credentials.
- Slack inbound behavior requires Socket Mode plus `app_mention` and `message.im` events.
- Authorized-user enforcement is project-scoped and allow-by-default when no authorized users are configured.
- Slack-origin Chat and `send_to_task` paths use shared runner/queued task-thread behavior in production.

Telegram facts:
- Telegram attachment and command behavior remains project-aware.
- Telegram services can be created at startup or from Settings; both paths need equivalent shared-runner, AgentRepo, and queued-promoter wiring.
- Telegram-origin Chat and `send_to_task` paths use shared runner/queued task-thread behavior in production.
- Telegram `Start`/`Stop` is nil-safe for partially constructed/test services.

Operational guidance for implementing and debugging GitHub, Slack, Telegram, webhook, and channel UI behavior lives in the project skill `.openvibely/skills/openvibely_channel_integrations_workflow/SKILL.md`. Shared queueing/steering rules remain in `.openvibely/skills/openvibely_chat_thread_turn_workflow/SKILL.md`; task-goal behavior remains in `.openvibely/skills/openvibely_task_goals_workflow/SKILL.md`.
