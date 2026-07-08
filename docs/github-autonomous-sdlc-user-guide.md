# GitHub Autonomous SDLC User Guide

Use GitHub issues and pull requests as the visible mailbox for recurring OpenVibely work. The loop is prompt-driven: OpenVibely scheduled tasks run normal task prompts that call generic GitHub tools. There is no hidden GitHub poller daemon.

## What This Loop Is For

The GitHub autonomous SDLC loop helps a project turn suggestions, approvals, implementation tasks, and pull requests into a reviewable GitHub-centered workflow.

Why this matters:

- GitHub issues stay the durable inbox for suggestions, approved work, blockers, and status.
- GitHub PRs stay the review and merge boundary.
- OpenVibely tasks, goals, schedules, threads, and worktrees remain visible and inspectable.
- Humans keep control of approval, prioritization, review, merge, credentials, and release decisions.

## Prerequisites

Before creating the loop:

1. Create or select the OpenVibely project for the repository.
2. Configure GitHub in `/channels` using a PAT or GitHub App.
3. For PAT setups, assign GitHub issues to the PAT owner when you want OpenVibely scheduled tasks to notice them.
4. For GitHub App setups, open `GitHub Runtime Settings` in the GitHub channel settings modal and set `Issue Inbox Assignee` to the GitHub user or bot that should receive work.
5. Use `Authorized Users` the same way as other channels: GitHub users allowed by explicit authorization checks. Authorized users are separate from the issue inbox.
6. Ensure the scheduled task's model/provider supports runtime tool calls.

A PAT identifies a real GitHub user, so scheduled tasks can call `github_list_my_assigned_issues` to find issues assigned to that user. A GitHub App installation may be installed on an organization, which is not an issue assignee; use `github_get_project_inbox` plus `github_list_assigned_issues` with the configured override for GitHub App setups.

## Minimum Visible Loop

Start with two scheduled tasks before adding more finders/fixers.

| Task | Cadence | Purpose |
|---|---:|---|
| `GitHub Offering Manager: Vision Suggestions` | Daily | Reads project vision/source files and opens suggestion issues only. |
| `GitHub Dev Inbox` | Hourly | Checks open issues assigned to the PAT user or configured Project Inbox Assignee override and links/updates eligible work. |

You can later add bug, performance, duplication, and loop-auditor tasks using the same pattern.

## Dev Inbox Prompt Pattern

Create a visible scheduled task with a prompt like:

```text
Check GitHub for implementation mailbox work for this project.

If this project uses a PAT, call `github_list_my_assigned_issues` to list open issues assigned to the PAT owner. If this project uses GitHub App mode or an explicit mailbox account, call `github_get_project_inbox`; when it is configured, pass that assignee to `github_list_assigned_issues`. If GitHub credentials or the required assignee are missing, stop and explain the missing configuration.

For each returned issue, inspect it with `github_get_issue`. Apply the user's workflow eligibility rule before creating implementation work. If this workflow still requires an associated PR before automation may touch an assigned issue, use `github_list_assigned_issues_with_prs` for the same assignee and skip assigned issues that have no associated PR according to that tool result.

Treat an eligible issue as actionable when it is assigned to the PAT owner or configured Project Inbox Assignee and has any explicit human approval signal required by your workflow.

For actionable issues, create or link visible OpenVibely work using task/thread/goal tools available in this run, then call `github_link_task_to_issue` when a concrete task and associated PR are known. Comment concise status on the issue with `github_comment_on_issue`.

Use unprefixed labels only, such as `task-created`, `in-progress`, `blocked`, `needs-human`, and `pr-opened`. Never use labels beginning with `openvibely:`.

When implementation work is complete in a task branch, use `github_open_pull_request` for that task and include issue metadata so the task PR record stays linked.
```

This prompt intentionally uses generic tools. Do not replace it with a hidden background service unless a future product requirement explicitly changes the loop engine.

## Offering Manager Prompt Pattern

Create a visible scheduled task with a prompt like:

```text
Review the configured project vision/source files and identify small, reviewable feature gaps.

Open GitHub suggestion issues only. Use `github_create_issue` with unprefixed labels such as `suggestion` and `feature`. Do not create implementation tasks and do not modify code.

Include enough context for a human to approve, reject, or assign the issue. Avoid duplicates by searching or inspecting existing visible work when the available tools allow it.
```

Offering and finder tasks should open issues only. Implementation and fixer tasks should act on issues assigned to the PAT owner or configured Project Inbox Assignee, plus any explicit human approval signal required by your workflow.

## Labels

Use plain, unprefixed labels such as:

- `suggestion`
- `approved`
- `in-progress`
- `task-created`
- `pr-opened`
- `blocked`
- `needs-human`
- `done`
- `duplicate`
- `bug`
- `feature`
- `performance`
- `duplication`
- `security-review`

Never use labels beginning with `openvibely:`. OpenVibely rejects that prefix in GitHub issue creation and label-add paths.

## Safety Rules

- Scheduled tasks are the loop engine; do not create hidden GitHub poller daemons.
- GitHub tools are generic reusable capabilities, not SDLC-only APIs.
- Assigned GitHub issues without an associated PR must be skipped unless the workflow is explicitly changed.
- Do not use `github_open_pull_request` as a loophole to start automation for an assigned issue that the mailbox eligibility rule would otherwise skip.
- Do not treat GitHub API credentials as human authorization.
- Keep each implementation task tied to visible issue/task/PR state so humans can review and merge in GitHub.

## Troubleshooting

- Dev Inbox stops with missing GitHub account: configure a PAT, or configure a GitHub App plus Project Inbox Assignee override.
- Dev Inbox refuses work: make sure the issue is assigned to the PAT owner or configured Project Inbox Assignee, and apply any expected human approval label.
- Assigned issue is skipped: verify it has the associated PR signal required by the current workflow rule.
- PR creation fails: check that the task has a worktree branch and that GitHub credentials can push branches and open PRs.
- Labels are rejected: remove any `openvibely:` prefix and use the plain label vocabulary above.

## Related Guides

- [GitHub Channels Setup](./github-channels-setup.md)
- [Schedule User Guide](./schedule-user-guide.md)
- [Tasks User Guide](./tasks-user-guide.md)
