---
name: product_vision_and_autonomy
type: project
created: 2026-06-10
updated: 2026-06-13
source: consolidation
source_id: memory_consolidation_2026_06_13
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
- Source-of-truth file selection should be conservative: use explicit file paths or a single obvious canonical root file. If vision/spec/defect files are missing or ambiguous, ask the user which exact file(s) to use rather than broadly guessing or creating a generic discovery task.
- Direct setup must run from project Chat in Orchestrate mode because task-thread follow-ups are intentionally constrained from creating, editing, or scheduling tasks.
- Indexed standalone skills still do not automatically kick in from ordinary interactive Chat prompts: interactive Chat uses recall-only memory preparation and does not run Skill Curator selected-skill routing or expose `skill_view`. “Make this project autonomous” needs manual skill selection/invocation or a product change that adds Chat standalone-skill routing.
- The tool contract should stay aligned with actual chat-control capabilities such as `get_current_project`, `project_info`, `list_capabilities`, `create_task`, `set_task_goal`, `edit_task`, `schedule_task`, `modify_schedule`, `send_to_task`, `execute_tasks`, `view_task_thread`, and `get_task_goal`.
- Bootstrap-skill idempotency is limited by the current chat-control surface: there is no generic `list_tasks` or `list_schedules` action, and several goal/task actions require real task IDs rather than titles.
- A successful bootstrap should produce visible OpenVibely objects, not just guidance: a User Priority Inbox, an Autonomous Project Driver, User Priority Triage, focused audit tasks, recurring schedules, safe initial executions when allowed, generated user-priority implementation tasks, and chained review/audit follow-ups.
- The autonomous-loop skill should describe task coordination as mediated orchestration, not task-to-task communication.

Current product-completeness themes from `VISION.md` expected to require recurring work include outcome-to-work decomposition, multi-agent team coordination, reviewable autonomy UX, durable learning quality, external integrations, operational clarity, and provider/model normalization.
