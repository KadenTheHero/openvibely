---
name: agent_lifecycle_and_skills
type: project
created: 2026-05-24
updated: 2026-06-03
source: consolidation
source_id: memory_consolidation_2026_06_03
confidence: high
title: Agent Lifecycle and Skills
---

OpenVibely lifecycle-agent and skill behavior is guided by the on-disk agent/skill catalog plus lifecycle hooks. Verify current source before relying on exact implementation details.

Catalog model:
- Built-in system agents include Skill Curator (`skill_curator`), Memory Curator (`memory_curator`), and Goal Agent (`goal`). They are protected system agents and should be hidden/protected/list-filtered consistently where system agents are not user-selectable.
- Goal Agent ships as embedded markdown under `internal/builtinskills/builtin/agents/goal/` with root `SKILLS.md` and `skills/evaluate_task_goal/SKILL.md`. Its seed/repair path must persist `system_kind=goal`, `created_by=system`, `generated_status=protected`, source refs, tool grants, and the `evaluate_task_goal` lifecycle hook through the system-agent path, not the generic generated-agent path. Repair must handle older databases with an existing `key=goal` row and blank `system_kind`; otherwise the unique `agents.key` index can block protected Goal Agent creation and leave lifecycle evaluation running in the wrong context or unavailable.
- Skill Curator uses clean `skill_curator` identifiers. Its scheduled maintenance skill is `maintain_skill_library`; bundled Skill Curator copy should say “skill” when it means skill, not “agent.”
- This subsystem has not shipped broadly, so do not preserve compatibility aliases, old paths, or intermediate names unless real persisted release data requires them.
- Agents are global by default and reusable across projects. Project-scoped agents/skills live under `<project_root>/.openvibely/agents/...`; global agents live under the app/config agents root shared by local web/server and desktop modes unless explicitly overridden.
- The on-disk catalog is authoritative after seed: `<agents_root>/AGENTS.md` lists agents, `<agent_key>/SKILLS.md` lists that agent’s skills/metadata, and `<agent_key>/skills/<skill_key>/SKILL.md` is the skill body.
- Standalone skills live under `<root>/skills/<skill_key>/SKILL.md`; agent-owned skills live under `<root>/agents/<agent_key>/skills/<skill_key>/SKILL.md`. Project definitions win over global definitions on duplicate keys.
- Standalone skills are file-backed, not DB-backed. `<root>/skills/SKILLS.md` `## <handle>` headings are canonical handles and loaders expect matching `<root>/skills/<handle>/SKILL.md`; heading/link/frontmatter mismatches can make manually added skills invisible.
- Product direction favors explicit import/index maintenance over automatic disk auto-discovery for manually dropped skill directories.
- Assigned-agent tasks should use that agent’s merged agent-owned skill catalog, not the top-level standalone catalog. Runtime `skill_view` and available-skill rendering must inspect the same assigned-agent scoped catalog.
- Built-in/global agents and skills sync through the embedded/built-in path. Bundled `SKILL.md` bodies may be overwritten as app-owned source; bundled `AGENTS.md`/`SKILLS.md` indexes are only created when missing because the LLM/user owns them once on disk.
- Missing indexes should degrade behavior but not crash or trigger deterministic regeneration. Bootstrap helpers should create directories only, except for built-in first-run seeding. Do not reintroduce deterministic index rebuilders, runtime index generators, or a `rebuild_indexes` runtime tool.

Mutation and migration behavior:
- `agent_manage`, `skill_manage`, and maintenance instructions should rely on mutation tools for catalog mutations where available. Scoped file edits are for root/index narrative polish or `AGENTS.md` updates, not redundant manual minimal-link edits when tooling maintains links.
- Skill handles and paths must remain constrained to indexed catalog entries; never allow model-supplied arbitrary paths.
- Standalone skill mutations must keep top-level `<root>/skills/SKILLS.md` consistent with `<root>/skills/<skill_key>/SKILL.md`; deleting only the directory can leave stale advertised skills.
- Agent root `SKILLS.md` is the authoritative overview/index and metadata surface for Agents page, lifecycle hooks, task loading, tool permissions, and declarations. It links to skill files and is updated idempotently, but it is not the canonical prompt container.
- Legacy DB-backed agent skills (`models.Agent.Skills`) are compatibility data distinct from routed on-disk agent-owned skills. Migrate/materialize DB-only agents safely into the global on-disk catalog with clean slugs, preserve data idempotently, and do not rewrite DB state from stale in-memory copies without verifying importer/root-declaration sync behavior.
- Standalone skill declarations should stay compact and limited to current catalog/manager fields. Keep active selection metadata such as `routing.triggers`, `routing.priority`, and `routing.description`; do not reintroduce removed legacy agent-routing scaffolding.
- Generated/native OpenVibely declarations must include required explicit `kind` frontmatter. Explicit import flows may accept standard Skills packages with top-level `name`/`description` and convert them into OpenVibely standalone declarations while preserving safe bundled resource files.

Skill Curator and post-task learning:
- `observe_task_for_learning` is a Skill Curator `after_complete` hook, not execution as the task’s assigned primary agent. Mutation requires explicit lifecycle runtime tool grants for the hook owner.
- The hook should review the compacted backend LLM conversation snapshot used for the task, not persisted threads, diffs, summaries, execution artifacts, or invented truncation policies.
- Before saving learning, Skill Curator should inspect existing agents/skills as needed and avoid duplicate or already-covered learning. Cross-agent improvements belong in standalone skills; assigned-agent updates are reserved for behavior specific to that assigned agent’s role, purpose, private workflow, or selected agent-owned skill.
- Hook context/tool descriptions should label assigned agent identity, purpose, selected agent-owned skills, selected standalone skills, provenance, and write policy explicitly; do not rely on path inference.
- If uncertain about placement, prefer standalone skill updates or a proposed-change outcome rather than mutating. Avoid bulk-copying standalone or unrelated skills into an agent.
- Assigned-agent skill mutation for post-task learning should use a constrained server-scoped agent-owned mutation path such as `agent_skill_manage`, not arbitrary `skill_manage` writes.
- Agents/hooks that create, change, consolidate, or retire skills must have needed mutation/scoped-file access and preserve affected indexes. If write access is absent, report the required follow-up instead of claiming mutation happened.

Lifecycle hooks and routing:
- Lifecycle hooks live around `internal/lifecycle/` and task execution/server setup. Durable concepts include `route_task`, `before_run`, `after_complete`, `scheduled`, task-mode bookkeeping, blocking versus non-blocking execution, idempotency/audit rows, recursion prevention, and strict validation of hook types/tool access.
- Goal Agent evaluation should run as an actual lifecycle/system-agent evaluator using its skill/tools, not as deterministic handler code. Filtering and runtime authorization should key on protected `system_kind=goal` identity, not only `SkillKey == "evaluate_task_goal"`.
- `task_mode` is primary active task execution slot/bookkeeping and should not be exposed as an ordinary user-authored lifecycle hook unless deliberately redesigned.
- `route_task` is an LLM routing decision over the prompt-safe agent index. It must receive the actual task title/prompt, may return a JSON `skills` array, and should preserve multiple selected skills from the winning router result without merging outputs from multiple route hooks.
- Lifecycle hook skill resolution is scoped to the hook owner. If the hook owner’s skill is missing, fail rather than falling back to the task turn’s selected/available catalog.
- Routing/effective-mode logic has one primary agent/effective mode. Do not merge tool permissions across multiple agents or introduce multi-agent execution without explicit redesign.
- Ordinary tasks may intentionally have no assigned primary agent. UI should label this as no assigned agent or skill-routed/default behavior, not “Auto Agent.”
- Explicit assigned primary agents skip skill routing and use that agent’s curated/default or manual skill selection, including tasks created from Chat orchestrate with an explicit `agent` Agent-definition name.
- Maintenance/system agents are excluded from auto-routing via `selectable_as_primary=false`, not hardcoded name checks. The flag should not ban explicit/API assignment, scheduled tasks, or deliberate invocations.
- Task detail UI should show the persisted selected primary task agent/effective mode. Lifecycle rows identify hook executors, not necessarily the routed primary task agent.
- Lifecycle visibility should render prompt-safe structured decisions: route selected skills and recall selected memories as compact badges/pills, while freeform hooks remain prose. Expanded hook detail may include prompt snapshots, tool calls, raw final output, validated JSON, duration, model, and provider, scoped to the hook/task.
- A lifecycle hook `OutputContract` constrains the final structured result stored/validated by lifecycle code, not the agent’s working notes, tool use, or reasoning.
- Lifecycle hook prompt/idempotency inputs must be JSON-safe even when a prior hook failed with invalid raw output. Invalid `PreviousOutputs` payloads should be sanitized before reuse, and final-output extraction should be contract-aware: skip syntactically valid tool-argument fragments such as `{ "task_id": "current" }`, select a JSON value that validates for the hook's declared output contract, and prevent concatenated objects from poisoning later hooks such as Memory Curator or Skill Curator.
- `activity_summary` lifecycle outputs require a real non-empty `summary` unless `skipped=true`; tool-call argument fragments must not be accepted as completed hook summaries.
- Goal Agent `evaluate_task_goal` should preflight required runtime tools after agent-grant filtering. If required goal tools are missing, fail clearly before the model call rather than letting the model produce a vague “tools unavailable” activity summary.
- Goal Agent `evaluate_task_goal` should run as a true generic `after_complete` lifecycle hook, not through a bespoke `runGoalAgentCheckpoint` path. The generic after-complete slot remains detached from the user-visible task response, while Goal Agent eligibility/tooling/reload semantics live in lifecycle execution: run for any task turn with an active/evaluable goal, inject goal runtime tools as after-complete-only tools through the worker's shared runtime provider so ordinary worker task completions and task-thread follow-ups both have them, preserve protected `system_kind=goal` hook identity for lineage/status authorization, and reload/publish current goal state after evaluation. Do not gate the protected Goal Agent to task-thread follow-ups only; that leaves initial worker task goals unevaluated.

Scheduled maintenance and UI direction:
- Prefer modeling scheduled maintenance as normal scheduled tasks assigned to agents and running through the usual lifecycle, unless a runbook requires invisible background hooks. System-agent scheduled tasks should respect explicit assigned agent and selected/manual skill configuration instead of ordinary `route_task` skill routing.
- Let loaded agent/skill declarations drive scheduled-task runtime tool grants rather than worker-side hardcoded maintenance tools. Memory-consolidation specifics live in `managed_memory.md`.
- Skill-library maintenance should use Skill Curator with `maintain_skill_library`; it may inspect agent namespaces and available skills, but should not manage standalone user-controlled agents unless explicitly authorized.
- Lifecycle direct-call scoped-file setup must pass absolute directories for extra scopes such as `global_agents`, resolving configured/built-in roots before constructing `ScopedFiles` extras.
- The left navigation includes a standalone Skills page using shared shell/sidebar conventions, searchable cards, kebab Edit/Delete, Agents-style scope badges, no displayed skill-key metadata line, and a frontmatter-seeded add modal.
- Editing an existing standalone skill should show scope as disabled/read-only unless true move semantics exist. Importing standalone skills should run `SKILL.md` through the importer so `SKILLS.md` stays consistent, preserves safe package-relative files, and shows package files read-only.
- Desktop/Wails skill-package import should use a Wails/native folder-picker path or equivalent desktop-safe flow because OS-native WebViews may not support browser-only directory upload reliably.
- The agent create/edit dialog should align with on-disk agent-owned skills. Label the area “Skills”; avoid “Agent-Owned Skills,” legacy Routing-tab fields, and Model Defaults JSON editing in Advanced for now.
- Lifecycle editing should focus on real hook slots, not `task_mode`; fold permission/default tool policy into Lifecycle Hooks rather than a separate Permissions tab.
- Default `selectable_as_primary` to enabled for new-agent create flow and legacy conversions unless a source declaration explicitly says otherwise.
- Verify persisted allowed-tool configuration and the Agents page/editor state, not only model output or DB rows; an agent can appear created while having no tools enabled if permissions are not derived and saved.
- Goal runtime tool IDs such as `get_task_goal`, `send_to_task`, `mark_task_goal_achieved`, and `report_task_goal_blocked` should be present in the agent tool catalog/UI so grants are visible and survive saves. The user wants goal status tools available to any agent that explicitly grants them, including task-thread assigned agents and lifecycle-hook agents; do not treat `mark_task_goal_achieved` and `report_task_goal_blocked` as protected-Goal-Agent-only. Execution should still be grant-filtered and preserve stale `goal_id` plus `status='active'` guards. Lifecycle hook status-tool execution should authorize against the actual lifecycle hook agent's grants when hook-agent context is present; protected Goal Agent authority comes from `system_kind=goal` hook identity, not caller-supplied runtime-origin fields.

End-to-end expectations:
- Lifecycle/skills work is incomplete unless wired end-to-end: build the skill catalog per task turn, resolve hook skill bodies, run routing/effective-mode resolution before LLM execution, register correct runtime tools, make created agents/skills visible in the filesystem catalog, execute scheduled bindings, and log enough to debug behavior.
- Common audit gaps include UI without backend handlers, hook outputs not merged into prompts, route/effective decisions captured but unused, runtime tools registered with nil dependencies, and write-side mutations not visible until the intended refresh boundary.
- Do not treat ad-hoc project-scoped agents/skills created during tests as built-in seeded product behavior; distinguish runtime/user-created artifacts from embedded built-ins and migrations.
