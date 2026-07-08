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
3. Assign GitHub issues to the GitHub account configured by the GitHub channel token or GitHub App installation when you want OpenVibely scheduled tasks to notice them.
4. Optionally open the GitHub channel settings modal and configure `GitHub Runtime Settings` for extra trust checks or an explicit override inbox account.
5. Ensure the scheduled task's model/provider supports runtime tool calls.

A token or GitHub App lets OpenVibely call GitHub APIs. The default Dev Inbox path uses that configured GitHub account directly: scheduled tasks call `github_list_my_assigned_issues` to find open issues assigned to the channel account.

## Minimum Visible Loop

Start with two scheduled tasks before adding more finders/fixers.

| Task | Cadence | Purpose |
|---|---:|---|
| `GitHub Offering Manager: Vision Suggestions` | Daily | Reads project vision/source files and opens suggestion issues only. |
| `GitHub Dev Inbox` | Hourly | Checks open issues assigned to the GitHub account configured for the channel and links/updates eligible work. |

You can later add bug, performance, duplication, and loop-auditor tasks using the same pattern.

## Dev Inbox Prompt Pattern

Create a visible scheduled task with a prompt like:

```text
Check GitHub for implementation mailbox work for this project.

Use `github_list_my_assigned_issues` to list open issues assigned to the GitHub account configured for this OpenVibely project's GitHub channel credentials. If GitHub credentials are missing or the account cannot be resolved, stop and explain the missing configuration.

For each returned issue, inspect it with `github_get_issue`. Apply the user's workflow eligibility rule before creating implementation work. If this workflow still requires an associated PR before automation may touch an assigned issue, use `github_list_assigned_issues_with_prs` for the same assignee and skip assigned issues that have no associated PR according to that tool result.

Treat an eligible issue as actionable when it is assigned to the configured GitHub channel account and has any explicit human approval signal required by your workflow.

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

Offering and finder tasks should open issues only. Implementation and fixer tasks should act on issues assigned to the GitHub account configured for the channel, or on issues with an explicit human approval signal required by your workflow.

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

- Dev Inbox stops with missing GitHub account: configure the GitHub channel token or GitHub App installation.
- Dev Inbox refuses work: make sure the issue is assigned to the GitHub account configured for the channel, or apply the expected human approval label.
- Assigned issue is skipped: verify it has the associated PR signal required by the current workflow rule.
- PR creation fails: check that the task has a worktree branch and that GitHub credentials can push branches and open PRs.
- Labels are rejected: remove any `openvibely:` prefix and use the plain label vocabulary above.

## Related Guides

- [GitHub Channels Setup](./github-channels-setup.md)
- [Schedule User Guide](./schedule-user-guide.md)
- [Tasks User Guide](./tasks-user-guide.md)
