---
kind: openvibely.agent_skill
version: 1
skill:
    key: openvibely_github_autonomous_sdlc_bootstrap
    name: OpenVibely GitHub Autonomous SDLC Bootstrap
    scope: global
    description: Bootstrap a GitHub-backed, prompt-driven autonomous SDLC loop using generic GitHub tools and visible OpenVibely tasks, goals, and schedules.
---

# OpenVibely GitHub Autonomous SDLC Bootstrap

Use this skill when the user asks to automate a project with GitHub, use GitHub issues as a mailbox, or set up reviewable GitHub issue-to-task-to-PR development.

Keep the setup generic. Use existing OpenVibely tasks, task goals, schedules, task threads, and generic GitHub runtime tools. Do not create hidden daemon or poller services, workflow-specific database state, or bespoke SDLC-only backends when a visible scheduled task prompt and reusable tools are enough.

## Required Direction

- Scheduled tasks are the loop engine. Create or update visible scheduled OpenVibely tasks with prompts that say exactly what GitHub mailbox work to perform.
- Bootstrap setup should run from a visible task or task-thread follow-up so lifecycle routing can select this skill and expose `skill_view`; ordinary Chat may have Orchestrate actions but does not run standalone skill routing today. If a user starts in Chat, create a small bootstrap task and continue setup there rather than claiming the skill was applied in Chat.
- GitHub tools are reusable capabilities, not SDLC-only APIs. For PAT setups, use `github_list_my_assigned_issues` to find open issues assigned to the authenticated PAT user. For GitHub App setups, do not treat the installation owner or organization as an issue assignee; add the real GitHub user or bot that should receive work to Authorized Users, read those assignee candidates with `github_get_project_inbox`, and pass each login to `github_list_assigned_issues`. When a prompt names a specific GitHub repository URL, pass `repo_url` to issue create/read/list/comment/label tools. Use `github_is_actor_authorized`, `github_create_issue`, `github_get_issue`, `github_list_assigned_issues_with_prs`, `github_comment_on_issue`, `github_add_issue_labels`, and `github_open_pull_request` when available.
- GitHub PR publication for implementation tasks should use `github_open_pull_request`, not ad hoc `git push` or GitHub CLI fallback. The tool is current-project/task scoped, publishes the task worktree branch through the configured GitHub token/API, then opens or reuses the PR and persists task PR metadata. The Changes tab Create PR button uses the same backend path with UI defaults; the runtime tool can additionally pass PR title/body/base/draft and issue metadata.
- GitHub labels must be unprefixed. Never use labels beginning with `openvibely:`. Use labels such as `suggestion`, `approved`, `in-progress`, `task-created`, `pr-opened`, `blocked`, `needs-human`, `done`, `duplicate`, `bug`, `feature`, `performance`, and `duplication`.
- Assignment to the configured OpenVibely GitHub inbox identity is the default human approval signal to start work. Assigned issues do not need an existing PR before automation may create OpenVibely implementation tasks; creating the task and later opening a PR is the normal issue-to-task-to-PR flow. Use `github_list_assigned_issues_with_prs` only for special workflows that explicitly require PR-associated issues.
- Human trust is separate from API credentials. PAT credentials identify a real GitHub user for default assignment polling. GitHub App credentials identify an installation and may be installed on an organization, so App setups use Authorized Users as the real issue assignee accounts to poll; assigning an issue to one of those configured identities is the user's approval to enter the implementation mailbox.
- Offering/finder tasks open GitHub issues only. Implementation/fixer tasks act on issues assigned to the PAT owner or configured Authorized Users. Optional labels such as `approved` may help humans audit the mailbox, but the default bootstrap workflow must not require an `approved` label in addition to assignment unless the user explicitly asks for that stricter gate.

## Bootstrap Steps

1. Confirm the current project, repository, and GitHub credentials. Ask only for missing inputs.
2. Create or update visible tasks and persisted goals for the desired loop. Prefer a small starting set before adding every possible finder/fixer.
3. Create recurring schedules with `schedule_task`; usually daily for suggestion/finder tasks and hourly for inbox/fixer tasks.
4. Put the behavior in each task prompt. Prompts should tell the agent which generic GitHub tools to call, which labels/auth checks to apply, what to skip, and what visible task/comment/PR updates to make.
5. Report exactly which tasks, goals, schedules, labels, and GitHub credential/settings dependencies were created or still need user action.

## Suggested Visible Tasks

- `GitHub Offering Manager: Vision Suggestions`, daily. Reads the project vision/source-of-truth files and opens suggestion issues only.
- `GitHub Dev Inbox`, hourly. Uses `github_list_my_assigned_issues` for PAT setups or `github_get_project_inbox` plus `github_list_assigned_issues` for GitHub App/custom setups, applies the workflow's eligibility rules, links eligible issues to OpenVibely work, and comments status.
- `GitHub Bug Finder`, daily. Audits a focused component and opens bug issues only.
- `GitHub Bug Fixer`, hourly or daily. Acts on eligible issues assigned to the PAT owner or configured Authorized Users and opens/reuses PRs for implementation tasks.
- `GitHub Loop Auditor`, weekly. Reviews stale labels, blocked work, duplicate tasks, missing issue/task/PR links, and unexpected GitHub assignments.

## Prompt Pattern For Dev Inbox

Use a prompt like this when creating the Dev Inbox scheduled task:

```text
Check GitHub for implementation mailbox work for this project.

If this project uses a PAT, call `github_list_my_assigned_issues` to list open issues assigned to the PAT owner. If this project uses GitHub App mode or custom mailbox accounts, call `github_get_project_inbox` to get Authorized Users; pass each returned assignee login to `github_list_assigned_issues`. If GitHub credentials or Authorized Users are missing, stop and explain the missing configuration.

For each returned issue, inspect it with `github_get_issue`. Treat assignment to the PAT owner or configured Authorized User as the user's approval to start implementation work, even when the issue has no associated PR yet. Do not call `github_list_assigned_issues_with_prs` as a default eligibility gate; use it only if the user explicitly asks for a PR-associated-issues-only workflow.

Treat an eligible issue as actionable when it is assigned to the PAT owner or configured Authorized Users. Optional labels such as `approved`, `feature`, `bug`, `performance`, or `duplication` may refine priority/scope, but do not require an `approved` label unless the user's workflow explicitly says to require one.

For actionable issues, create or continue visible OpenVibely work using task/thread/goal tools available in this run. Comment concise status on the issue with `github_comment_on_issue`.

Use unprefixed labels only, such as `task-created`, `in-progress`, `blocked`, `needs-human`, and `pr-opened`. Never use labels beginning with `openvibely:`.

When implementation work is complete in a task branch, use `github_open_pull_request` for that task and include issue metadata so the task PR record stays linked. Do not use local `git push` or GitHub CLI as a fallback if the tool fails; report the tool error so GitHub token/API publication can be fixed.
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
- Do not claim the bootstrap is complete if required GitHub tools, channel credentials, or schedules are missing.
