---
name: agent_lifecycle_and_skills
type: project
created: 2026-05-24
updated: 2026-06-09
source: consolidation
source_id: memory_consolidation_2026_06_07
confidence: high
title: Agent Lifecycle and Skills
---

OpenVibely lifecycle-agent and skill behavior is guided by the on-disk agent/skill catalog plus lifecycle hooks. Exact implementation details are source-authoritative.

Agent and catalog facts:
- Built-in protected system agents include Skill Curator (`skill_curator`), Memory Curator (`memory_curator`), and Goal Agent (`goal`).
- Fresh-startup initialization must materialize these protected system agents in the database idempotently from bundled declarations; generic root declaration sync may skip protected declarations and is not sufficient by itself.
- Goal Agent ships as embedded markdown under `internal/builtinskills/builtin/agents/goal/` with root `SKILLS.md` and `skills/evaluate_task_goal/SKILL.md`.
- Skill Curator uses clean `skill_curator` identifiers; its scheduled maintenance skill is `maintain_skill_library`.
- Agents are global by default and reusable across projects. Project-scoped agents/skills live under `<project_root>/.openvibely/agents/...`; global agents live under the app/config agents root.
- The on-disk per-agent `SKILLS.md` declaration is the authoritative overview/index and metadata surface for agent skills, lifecycle hooks, task loading, tool permissions, and declarations.
- Standalone skills are filesystem-backed packages. `<root>/skills/SKILLS.md` headings are canonical handles and match `<root>/skills/<handle>/SKILL.md`.
- Project scope overrides global scope for matching standalone or agent-owned skill keys.
- Product direction favors explicit import/index maintenance over automatic disk auto-discovery for manually dropped skill directories.

Project guidance facts:
- The 2026-06-06 guidance migration removed OpenVibely's own root `AGENTS.md`, `CLAUDE.md`, `PRACTICES.md`, and `guardrails.md` files as required app artifacts.
- Static OpenVibely repository guidance belongs in `.openvibely/skills/openvibely_project_guidance/SKILL.md`, indexed by `.openvibely/skills/SKILLS.md`.
- `openvibely_project_guidance` is intended to be selected for every applicable ordinary task through top-level `always_use`, not through hardcoded routing prompt text or a bespoke service path.
- `.gitignore` selectively unignores committed project skills/memories while leaving local app-managed `.openvibely` state ignored.

Skill metadata facts:
- `skill.enabled: false` disables a skill; absent/true means enabled.
- Runtime catalogs for task execution, lifecycle hooks, skill routing/selection, `skill_view`, and context injection exclude disabled skills.
- Management/admin listings still show disabled skills as manageable.
- Standalone top-level `always_use` metadata is catalog control data and does not appear in model-visible `<available_skills>` rendering.
- Generated/native OpenVibely declarations include required explicit `kind` frontmatter.
- Standard skill packages with top-level `name`/`description` can be converted into OpenVibely standalone declarations during explicit import.

Skill Curator facts:
- Skill Curator is a recursive self-improvement loop: `observe_task_for_learning` reviews completed task conversations for reusable learnings and can create or patch skills so future tasks benefit; `maintain_skill_library` consolidates and prunes the skill library on a schedule.
- `observe_task_for_learning` is a Skill Curator `after_complete` hook, not execution as the task's assigned primary agent.
- Cross-agent improvements belong in standalone skills.
- Skill-library maintenance may create, patch, consolidate, or archive skills, but agents are user-managed configurations: do not create, edit, archive, route, reassign, or mutate agent metadata/tools/hooks/attachments as part of skill maintenance.
- Assigned-agent updates are reserved for behavior specific to that assigned agent's role, purpose, private workflow, or selected agent-owned skill.
- Agent-owned skill mutation for post-task learning uses the server-scoped `agent_skill_manage` path.

Lifecycle facts:
- Lifecycle hooks live around `internal/lifecycle/` and task execution/server setup.
- Durable hook concepts include `route_task`, `before_run`, `after_complete`, `scheduled`, task-mode bookkeeping, blocking versus non-blocking execution, idempotency/audit rows, recursion prevention, output contracts, and runtime-tool filtering.
- `route_task` runs before `before_run`. Skill Curator returns selected skill handles; Memory Curator returns selected memory handles.
- Skill Curator `selected_skills` and Memory Curator `selected_memories` can both occupy the route slot. Built-in route hooks default non-blocking, while the runner waits for route-slot completion before the main model turn starts.
- Lifecycle hook skill resolution is scoped to the hook owner.
- Routing/effective-mode logic has one primary agent/effective mode. Multi-agent permission merging is not part of the current design.
- Ordinary tasks may intentionally have no assigned primary agent. Explicit assigned primary agents skip standalone skill routing and use that agent's curated/default or manual skill selection.
- Maintenance/system agents are excluded from auto-routing via `selectable_as_primary=false`.
- Lifecycle visibility renders structured selected-skill and selected-memory `route_task` decisions as compact prompt-safe badges/pills, without duplicate plain-text selected-handle summaries.
- `after_complete` hook rows such as learning and memory-update summaries should still render their text summaries; selected-handle summary suppression is scoped to structured route-selection rows.
- Lifecycle output contracts constrain final stored/validated results, not the agent's working notes or tool use.

Goal Agent facts:
- Goal Agent evaluation runs as a protected generic `after_complete` lifecycle evaluator, not a deterministic checkpoint.
- Goal Agent authority comes from protected `system_kind=goal` identity and explicit runtime tool grants.
- Goal Agent after-complete evaluation is detached from the user-visible task response and reloads/publishes current goal state after evaluation.
- Goal runtime tool IDs such as `get_task_goal`, `send_to_task`, `mark_task_goal_achieved`, and `report_task_goal_blocked` are part of the agent tool catalog/UI so grants survive saves.

Scheduled maintenance and UI facts:
- Scheduled maintenance is modeled as normal scheduled tasks assigned to agents unless a future runbook explicitly requires invisible background hooks.
- Fresh installs must idempotently create visible scheduled tasks for `System: Memory Consolidation` and `System: Skill Library Maintenance` during startup, including when the default project exists but has no repo path.
- Scheduled maintenance task titles may remain app/storage identifiers; lifecycle hook input uses prompt-safe titles without low-value internal prefixes such as `System:`.
- Memory consolidation specifics live in `managed_memory.md`.
- The standalone Skills page uses shared shell/sidebar conventions, searchable cards, scope badges, disabled badges, and create/edit controls for Enabled and Always use.
- The agent create/edit dialog aligns with on-disk agent-owned skills and labels the area “Skills.”
- Lifecycle editing focuses on real hook slots, not `task_mode`.

Operational implementation guidance for skill lifecycle, lifecycle hooks, Memory Curator routing, Goal Agent behavior, skill UI, import/indexing, lifecycle output validation, and regression coverage belongs in `.openvibely/skills/openvibely_skill_lifecycle_workflow/SKILL.md`, `.openvibely/skills/openvibely_lifecycle_hook_workflow/SKILL.md`, and `.openvibely/skills/openvibely_task_goals_workflow/SKILL.md`.
