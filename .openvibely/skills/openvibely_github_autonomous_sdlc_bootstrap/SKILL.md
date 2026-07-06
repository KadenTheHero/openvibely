---
kind: openvibely.agent_skill
version: 1
skill:
  key: openvibely_github_autonomous_sdlc_bootstrap
  name: OpenVibely GitHub Autonomous SDLC Bootstrap
  scope: project
  description: Bootstrap a GitHub-backed, prompt-driven autonomous SDLC loop using generic GitHub tools and visible OpenVibely tasks, goals, and schedules.
---

# OpenVibely GitHub Autonomous SDLC Bootstrap

Use this skill when the user asks to automate a project with GitHub, use GitHub issues as a mailbox, or set up reviewable GitHub issue-to-task-to-PR development.

Keep the setup generic. Use existing OpenVibely tasks, task goals, schedules, task threads, and generic GitHub runtime tools. Do not create hidden daemon or poller services, workflow-specific database state, or bespoke SDLC-only backends when a visible scheduled task prompt and reusable tools are enough.

## Required Direction

- Scheduled tasks are the loop engine. Create or update visible scheduled OpenVibely tasks with prompts that say exactly what GitHub mailbox work to perform.
- GitHub tools are reusable capabilities, not SDLC-only APIs. Use `github_get_project_inbox`, `github_is_actor_authorized`, `github_create_issue`, `github_get_issue`, `github_list_assigned_issues_with_prs`, `github_comment_on_issue`, `github_add_issue_labels`, `github_link_task_to_issue`, and `github_open_pull_request` when available.
- GitHub labels must be unprefixed. Never use labels beginning with `openvibely:`. Use labels such as `suggestion`, `approved`, `in-progress`, `task-created`, `pr-opened`, `blocked`, `needs-human`, `done`, `duplicate`, `bug`, `feature`, `performance`, and `duplication`.
- Assigned GitHub issues without an associated PR must be skipped by automation unless the user explicitly changes that workflow rule. Use `github_list_assigned_issues_with_prs` and `github_link_task_to_issue`; do not use `github_open_pull_request` as a loophole to start work on an ineligible assigned issue.
- Human trust is separate from API credentials. Use configured GitHub authorized actors for approval/trigger decisions and the project inbox assignee for mailbox polling.
- Offering/finder tasks open GitHub issues only. Implementation/fixer tasks act only on authorized or explicitly approved issues and open or reuse PRs for OpenVibely task branches.

## Bootstrap Steps

1. Confirm the current project, repository, GitHub credentials, configured project inbox assignee, and authorized GitHub actors. Ask only for missing inputs.
2. Create or update visible tasks and persisted goals for the desired loop. Prefer a small starting set before adding every possible finder/fixer.
3. Create recurring schedules with `schedule_task`; usually daily for suggestion/finder tasks and hourly for inbox/fixer tasks.
4. Put the behavior in each task prompt. Prompts should tell the agent which generic GitHub tools to call, which labels/auth checks to apply, what to skip, and what visible task/comment/PR updates to make.
5. Report exactly which tasks, goals, schedules, labels, actors, and inbox settings were created or still need user action.

## Suggested Visible Tasks

- `GitHub Offering Manager: Vision Suggestions`, daily. Reads the project vision/source-of-truth files and opens suggestion issues only.
- `GitHub Dev Inbox`, hourly. Checks the configured project inbox assignee, reconciles assigned issues with associated PRs, skips issues with no associated PR, verifies authorization/approval, links eligible issues to OpenVibely work, and comments status.
- `GitHub Bug Finder`, daily. Audits a focused component and opens bug issues only.
- `GitHub Bug Fixer`, hourly or daily. Acts only on authorized or approved bug issues and opens/reuses PRs for implementation tasks.
- `GitHub Loop Auditor`, weekly. Reviews stale labels, blocked work, duplicate tasks, missing issue/task/PR links, and unauthorized triggers.

## Prompt Pattern For Dev Inbox

Use a prompt like this when creating the Dev Inbox scheduled task:

```text
Check GitHub for implementation mailbox work for this project.

Use `github_get_project_inbox` to get the assignee login. If no inbox is configured, stop and explain the missing configuration.

Use `github_list_assigned_issues_with_prs` for that assignee. Skip any assigned issue that has no associated PR according to the tool result. Do not create implementation work for skipped issues.

For each eligible issue, inspect the issue with `github_get_issue`. Treat the issue as actionable only if it is explicitly approved or the relevant actor is authorized according to `github_is_actor_authorized`.

For actionable issues, create or link visible OpenVibely work using task/thread/goal tools available in this run, then call `github_link_task_to_issue` when a concrete task and associated PR are known. Comment concise status on the issue with `github_comment_on_issue`.

Use unprefixed labels only, such as `task-created`, `in-progress`, `blocked`, `needs-human`, and `pr-opened`. Never use labels beginning with `openvibely:`.

When implementation work is complete in a task branch, use `github_open_pull_request` for that task and include issue metadata so the task PR record stays linked.
```

## Prompt Pattern For Offering Manager

```text
Review the configured project vision/source files and identify small, reviewable feature gaps.

Open GitHub suggestion issues only. Use `github_create_issue` with unprefixed labels such as `suggestion` and `feature`. Do not create implementation tasks and do not modify code.

Include enough context for a human to approve, reject, or assign the issue. Avoid duplicates by searching/inspecting existing visible work when the available tools allow it.
```

## Common Pitfalls

- Do not create hidden services or background workers for the GitHub loop.
- Do not make GitHub runtime tools explicit-agent-grant-only as part of this setup; scheduled tasks need the generic GitHub tools when GitHub is configured and the provider supports runtime tool calls.
- Do not create or mutate agents unless the user explicitly asks and the available tool surface supports it. Prefer visible tasks, goals, schedules, and configured GitHub identities.
- Do not treat GitHub API credentials as authorization for human-triggered auto-fix work.
- Do not rely on prompt memory for dedupe or status. Use visible GitHub issues, comments, labels, task goals, task threads, and PR records.
- Do not claim the bootstrap is complete if required GitHub tools, credentials, inbox identity, authorized actors, or schedules are missing.
