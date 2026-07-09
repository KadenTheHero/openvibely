---
name: product_vision_and_autonomy
type: project
created: 2026-06-10
updated: 2026-07-09
source: after_complete_memory_update
source_id: a7e8ff17f79ab7e721b50dafcbd0099a
confidence: high
title: Product Vision and Reviewable Autonomy
---

OpenVibely's product direction centers on recursive, reviewable self-improvement: goals become tasks, agents execute work in isolated worktrees, schedules and dynamic wakeups keep progress moving, and skills/memory compound learning across runs.

Durable vision principles:
- The system should help continuously drive work toward `VISION.md`, but not behave as a hidden autonomous developer.
- Autonomy should remain inspectable and review-gated through task threads, lifecycle evidence, worktree diffs, schedules, goals, selected skills, selected memories, and review/merge boundaries.
- Humans remain responsible for product judgment, priority tradeoffs, credentials/integration setup, and final merge/release decisions.
- Goal Agent, Loop Agent, scheduled tasks, task chaining, Skill Curator, and Memory Curator are the intended primitives for recursive self-improvement loops.
- The strongest current self-evolution pattern is a durable goal-driven Vision Driver task, dynamic loop wakeups for adaptive continuation, scheduled audits for reliable recurrence, task chaining for reviewable decomposition, and curator agents for compounding durable knowledge.

User-priority direction:
- Explicit user-provided bug lists and specs are higher-priority work sources than autonomously discovered `VISION.md` gaps.
- Preferred operating pattern is a durable User Priority Inbox plus triage schedule.
- Vision Driver-style autonomous loops should inspect user-priority tasks first, promote safe/focused P0/P1 user work before autonomous vision work, and only fall back to self-discovered gaps when the user queue is empty or blocked.

Recursive bootstrap skill direction:
- A reusable `openvibely_recursive_self_improvement_bootstrap` skill concept is being developed to bootstrap reviewable autonomy across projects.
- The user wants the skill indexed in `.openvibely/skills/SKILLS.md` for local OpenVibely testing before release; the draft package body may remain ignored/local until ready to publish.
- The skill should behave as an easy button: short prompts such as “make this project autonomous” or “loop on this vision” should be enough.
- The skill should use real OpenVibely control-plane tools to create/update the priority inbox, autonomous driver task, persisted goals, triage/audit/implementation/repair/review tasks, recurring schedules, dynamic wakeup guidance, task chaining, and curator loops; it should not merely describe the loop.
- Source-of-truth file selection should be conservative: use explicit file paths or a single obvious canonical root file. If vision/spec/defect files are missing or ambiguous, ask the user which exact file(s) to use rather than guessing or creating a generic discovery task.
- Direct setup for bootstrap skills should run from a visible task or task-thread follow-up where lifecycle routing can select standalone skills and expose `skill_view`; ordinary interactive Chat does not run standalone skill routing today. Initial task runs now have runtime-tool-capable bootstrap support for visible task creation, persisted task goals, schedules, capabilities, and GitHub inbox/issue/PR tools when dependencies are wired and the provider supports runtime tools.
- Indexed standalone skills still do not automatically kick in from ordinary interactive Chat prompts: interactive Chat uses recall-only memory preparation and does not run Skill Curator selected-skill routing or expose `skill_view`. “Make this project autonomous” needs a visible bootstrap task/task-thread invocation or a product change that adds Chat standalone-skill routing.
- The tool contract should stay aligned with actual chat-control capabilities and avoid fictional orchestration APIs; current bootstrap idempotency is limited because there is no generic `list_tasks`/`list_schedules` action, and several goal/task actions require real task IDs rather than titles.
- A successful bootstrap should produce visible OpenVibely objects, not just guidance: a User Priority Inbox, an Autonomous Project Driver, User Priority Triage, focused audit tasks, recurring schedules, safe initial executions when allowed, generated user-priority implementation tasks, and chained review/audit follow-ups.
- The autonomous-loop skill should describe task coordination as mediated orchestration, not task-to-task communication.

GitHub-backed autonomous SDLC direction:
- The user wants GitHub issues and PRs to act as the durable mailbox/status board for autonomous product development: suggestions, approved work, active implementation, PR review, and completion should be visible in GitHub rather than only internal OpenVibely tasks.
- Desired review boundaries are GitHub-centered and manual at key gates: Offering Manager-style agents may open suggestion issues from `VISION.md`, humans approve by assigning issues or labels, Dev/Fixer agents create OpenVibely tasks and PRs, and humans review/merge PRs in GitHub.
- A reusable “automate this project with GitHub” skill is desired; it should create visible tasks, schedules, goals, GitHub label/workflow configuration, authorization checks, and only the minimal generic identity/config primitives needed by scheduled tasks rather than merely explaining the loop.
- As of 2026-07-08, the documented minimum prompt-driven GitHub autonomous SDLC loop is complete. "Minimum" means the smallest end-to-end loop: a visible scheduled OpenVibely task periodically finds assigned GitHub issues, decides what to work on from its prompt/goals, performs implementation work, opens or reuses a GitHub PR, and leaves human review/merge in GitHub. It intentionally excludes hidden daemons, instant webhook reaction, auto-merge, rich comment/status reconciliation, dashboards, and advanced event dedupe. The `openvibely_github_autonomous_sdlc_bootstrap` skill is bundled as a reusable global standalone skill, with this repository's project-scoped copy still able to override it; it is indexed, generic/reusable, instructions-only, and uses existing control-plane/runtime tools to create visible prompt-driven schedules/tasks/goals rather than hidden daemons. The user guide lives at `docs/github-autonomous-sdlc-user-guide.md` and should stay aligned with the bootstrap contract: PAT setups use `github_list_my_assigned_issues`; GitHub App/custom setups add the real GitHub user/bot to GitHub `Authorized Users`, read assignee candidates with `github_get_project_inbox`, and pass each login to `github_list_assigned_issues`; no `openvibely:` labels; skip assigned issues without associated PRs when the workflow requires that gate; keep future expansions optional/generic.
- The GitHub-centered approach should reuse the older Vision Driver ideas of durable goals, schedules, dynamic wakeups, user-priority-first handling, task chaining, and review-gated autonomy, but move external coordination/status to GitHub issues/PRs.
- GitHub autonomous-SDLC support should remain generic: provide reusable GitHub/runtime/control-plane tools that can be used in many workflows, avoid bespoke hidden SDLC daemons or workflow-specific services unless a generic primitive is actually needed, and make the bootstrap skill set up prompt-driven visible schedules/tasks such as “check GitHub for assigned issues matching the configured mailbox criteria” instead of hardcoded backend loops.

Current product-completeness themes from `VISION.md` expected to require recurring work include outcome-to-work decomposition, multi-agent team coordination, reviewable autonomy UX, durable learning quality, external integrations, operational clarity, and provider/model normalization.
