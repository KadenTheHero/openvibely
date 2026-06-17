---
name: agent_lifecycle_and_skills
type: project
created: 2026-05-24
updated: 2026-06-17
source: consolidation
source_id: memory_consolidation_2026_06_15
confidence: high
title: Agent Lifecycle and Skills
---

OpenVibely lifecycle-agent and skill behavior is guided by the on-disk agent/skill catalog plus lifecycle hooks. Exact implementation details are source-authoritative.

Agent and catalog facts:
- Built-in protected system agents include Skill Curator (`skill_curator`), Memory Curator (`memory_curator`), Goal Agent (`goal`), and Loop Agent (`loop`). Fresh-startup initialization must materialize them idempotently from bundled declarations.
- Goal Agent ships under `internal/builtinskills/builtin/agents/goal/` with root `SKILLS.md` and `skills/evaluate_task_goal/SKILL.md`.
- Loop Agent is a protected built-in lifecycle agent and runs after-complete only for tasks with dynamic-loop state enabled.
- Agents are global by default and reusable across projects. Project-scoped agents/skills live under `<project_root>/.openvibely/agents/...`; global agents live under the app/config agents root.
- The on-disk per-agent `SKILLS.md` declaration is authoritative for agent skills, lifecycle hooks, task loading, tool permissions, and declarations.
- Standalone skills are filesystem-backed packages. `<root>/skills/SKILLS.md` headings are canonical handles and match `<root>/skills/<handle>/SKILL.md`.
- An indexed standalone skill is unusable unless the matching package body exists in the checkout the running app loads; creating the package only inside an isolated task worktree leaves the main catalog pointing at a dead path.
- Project scope overrides global scope for matching standalone or agent-owned skill keys. Product direction favors explicit import/index maintenance over automatic disk auto-discovery.
- `skill.enabled: false` disables a skill for task execution, lifecycle hooks, routing, `skill_view`, and context injection; management/admin listings still show disabled skills.
- Standalone top-level `always_use` metadata is catalog control data and does not appear in model-visible `<available_skills>` rendering.
- Generated/native OpenVibely declarations include explicit `kind` frontmatter. Explicit skill import surfaces, including the Skills page `/skills/import` flow and the `skill_import` runtime tool, materialize packages through shared normalization into `<root>/skills/<handle>/SKILL.md` and update `<root>/skills/SKILLS.md`.
- Skill import normalization guarantees YAML frontmatter with at least `name`, `description`, `kind: skill`, and `enabled: true`; it supports raw Markdown bodies, common top-level `name`/`description` packages, and existing OpenVibely declarations without wholesale clobbering valid fields.
- `skill_import` is treated as a skill-library write capability alongside `skill_manage`; it should be granted to write-authorized skill/curation agents rather than exposed broadly to ordinary task turns.

Project guidance facts:
- The 2026-06-06 guidance migration removed OpenVibely's own root `AGENTS.md`, `CLAUDE.md`, `PRACTICES.md`, and `guardrails.md` files as required app artifacts.
- Static OpenVibely repository guidance belongs in `.openvibely/skills/openvibely_project_guidance/SKILL.md`, indexed by `.openvibely/skills/SKILLS.md`.
- `openvibely_project_guidance` should be selected for applicable ordinary tasks through top-level `always_use`, not hardcoded routing prompt text or a bespoke service path.
- `.gitignore` selectively unignores committed project skills/memories while leaving local app-managed `.openvibely` state ignored.

Skill Curator facts:
- Skill Curator is a recursive self-improvement loop: `observe_task_for_learning` reviews completed task conversations for reusable learnings and can create or patch skills; `maintain_skill_library` consolidates and prunes the skill library on a schedule.
- `observe_task_for_learning` is a Skill Curator `after_complete` hook, not execution as the task's assigned primary agent.
- Cross-agent improvements belong in standalone skills.
- Skill-library maintenance may create, patch, consolidate, or archive skills, but agents are user-managed configurations: do not create, edit, archive, route, reassign, or mutate agent metadata/tools/hooks/attachments as part of skill maintenance.
- Assigned-agent updates are reserved for behavior specific to that assigned agent's role, purpose, private workflow, or selected agent-owned skill. Agent-owned skill mutation uses the server-scoped `agent_skill_manage` path.

Lifecycle facts:
- Lifecycle hooks live around `internal/lifecycle/` and task execution/server setup. Durable concepts include `route_task`, `before_run`, `after_complete`, `scheduled`, task-mode bookkeeping, blocking/non-blocking execution, idempotency/audit rows, recursion prevention, output contracts, and runtime-tool filtering.
- `route_task` runs before `before_run`. Skill Curator returns selected skill handles; Memory Curator returns selected memory handles. Both can occupy the route slot.
- Built-in route hooks default non-blocking, while the runner waits for route-slot completion before the main model turn starts.
- Lifecycle hook skill resolution is scoped to the hook owner.
- Routing/effective-mode logic has one primary agent/effective mode. Multi-agent permission merging is not part of the current design.
- Ordinary tasks may intentionally have no assigned primary agent. Explicit assigned primary agents skip standalone skill routing and use that agent's curated/default or manual skill selection.
- Maintenance/system agents are excluded from auto-routing via `selectable_as_primary=false`.
- Lifecycle visibility renders structured selected-skill and selected-memory route decisions as compact prompt-safe badges/pills; text summaries remain useful for non-route hook rows.
- Lifecycle output contracts constrain final stored/validated results, not the agent's working notes or tool use.
- Lifecycle hook and task-mode terminal execution status writes must not use the possibly cancelled operational hook/model context. After hook/model work returns, terminal `lifecycle_executions` updates use a fresh short-timeout finalization context so rows are not left `running` when LLM deadlines or cancellations fire.

Goal and Loop Agent facts:
- Goal Agent evaluation runs as a protected generic `after_complete` lifecycle evaluator, not a deterministic checkpoint. Its authority comes from protected `system_kind=goal` identity and explicit runtime tool grants.
- Goal Agent after-complete evaluation is detached from the user-visible task response and reloads/publishes current goal state after evaluation.
- Goal Agent after-complete evaluation runs on normal lifecycle hook input and task-thread `conversation_transcript`; lifecycle hooks must stay generic because they support other agents.
- Goal Agent must remain a generic model evaluator. Avoid goal-objective keyword parsing, audit-specific guard blocks, deterministic completion logic, prompt bias keyed to specific goal wording, Goal-Agent-specific lifecycle input fields, transcript patching, raw-output replacement, `diff_output` injection, or separate judgment-shaped facts such as `evaluated_execution` unless explicitly redesigned.
- A Goal Agent audit/read-only misclassification was resolved with a prompt-only fix after confirming ordinary transcript evidence already contained edit/action statements and changed-file summaries. Earlier experiments that patched `conversation_transcript` with raw execution output or injected/preserved `diff_output` were reverted as unnecessary; preserve the generic transcript-evidence approach.
- Goal Agent `evaluate_task_goal` prompt guidance now explicitly tells the model to read concrete transcript evidence rather than only the assistant's final completion claim. Explicit task-agent statements about actions taken, files changed, commands run, validation performed, or remaining issues are evidence to reconcile with the stored goal; if a goal requires that some action did not happen, assistant text saying the action happened is evidence that condition is not proven by that turn. Keep this guidance generic rather than audit/read-only keyword hardcoding.
- Goal runtime tool IDs such as `get_task_goal`, `send_to_task`, `mark_task_goal_achieved`, and `report_task_goal_blocked` are part of the agent tool catalog/UI so grants survive saves.
- Dynamic task-loop wakeups use the protected Loop Agent after-complete hook. Its `schedule_task_wakeup` runtime tool is lifecycle-only and should not be exposed to ordinary task agents by default.
- Loop Agent wakeups are task-thread continuations enqueued through durable `thread_inputs`, not direct worker submissions or separate worker tasks.
- Loop Agent wakeup scheduling is server-side blocked when a task goal is achieved, paused, cleared, blocked, or failed.
- Lifecycle-origin `send_to_task` continuations are rejected when the hook evaluated an older execution and a newer execution exists for the same task.

Scheduled maintenance and UI facts:
- Scheduled maintenance is modeled as normal scheduled tasks assigned to agents unless a future runbook explicitly requires invisible background hooks.
- Fresh installs must idempotently create visible scheduled tasks for `System: Memory Consolidation` and `System: Skill Library Maintenance` during startup, including when the default project exists but has no repo path.
- Scheduled maintenance task titles may remain app/storage identifiers; lifecycle hook input uses prompt-safe titles without low-value internal prefixes such as `System:`.
- The standalone Skills page uses shared shell/sidebar conventions, searchable cards, scope badges, disabled badges, and create/edit controls for Enabled and Always use.
- The agent create/edit dialog aligns with on-disk agent-owned skills and labels the area “Skills.” Lifecycle editing focuses on real hook slots, not `task_mode`.

Operational implementation guidance belongs in skills such as `openvibely_skill_lifecycle_workflow`, `openvibely_lifecycle_hook_workflow`, and `openvibely_task_goals_workflow`.
