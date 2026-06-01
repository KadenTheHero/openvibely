---
name: agent_lifecycle_and_skills
type: project
created: 2026-05-24
updated: 2026-06-01
source: consolidation
source_id: memory_consolidation_2026_06_01
confidence: high
title: Agent Lifecycle and Skills
---

OpenVibely lifecycle-agent and skill behavior is guided by the on-disk agent/skill catalog plus lifecycle hooks; verify current source before relying on exact implementation details.

Catalog model:
- The built-in system agent for skill work is Skill Curator with clean `skill_curator` identifiers. Its scheduled maintenance skill is `maintain_skill_library`; bundled Skill Curator text should say “skill” when it means skill, not “agent.”
- The feature has not shipped broadly, so do not preserve compatibility aliases, old paths, or intermediate names unless real persisted release data requires them.
- Agents are global by default and reusable across projects. Project-scoped agents/skills live under `<project_root>/.openvibely/agents/...`; global agents live under the global app/config agents root shared by local web/server and desktop modes unless explicitly overridden.
- The on-disk catalog is authoritative after seed: `<agents_root>/AGENTS.md` lists agents, `<agents_root>/<agent_key>/SKILLS.md` lists that agent’s skills/metadata, and `<agents_root>/<agent_key>/skills/<skill_key>/SKILL.md` is the skill body.
- Standalone skills live under `<root>/skills/<skill_key>/SKILL.md`; agent-owned skills live under `<root>/agents/<agent_key>/skills/<skill_key>/SKILL.md`. Project definitions win over global definitions on duplicate keys.
- Standalone skills are file-backed, not DB-backed. In indexed behavior, `<root>/skills/SKILLS.md` `## <handle>` headings are canonical handles and loaders expect `<root>/skills/<handle>/SKILL.md`; heading/link/frontmatter mismatches can make manually added skills invisible.
- Product direction favors explicit import/index maintenance over automatic disk auto-discovery for manually dropped skill directories.
- Assigned-agent tasks should use that agent’s merged agent-owned skill catalog, not the top-level standalone catalog. Runtime `skill_view` and available-skill rendering must inspect the same assigned-agent scoped catalog.
- Built-in/global agents and skills are synced through the embedded/built-in sync path. Bundled `SKILL.md` bodies may be overwritten as app-owned source; bundled `AGENTS.md`/`SKILLS.md` indexes are only created when missing because the LLM/user owns them once on disk.
- Missing indexes should degrade behavior but not crash or trigger deterministic regeneration. Bootstrap helpers should create directories only, except for the built-in first-run seed path. Do not reintroduce deterministic index rebuilders, runtime index generators, or a `rebuild_indexes` runtime tool.

Mutation and migration behavior:
- `agent_manage`, `skill_manage`, and maintenance instructions should rely on mutation tools for catalog mutations where available. Scoped file edits are for root/index narrative polish or `AGENTS.md` updates, not redundant manual minimal-link edits when tooling maintains links.
- Skill handles and paths must remain constrained to indexed catalog entries; never allow model-supplied arbitrary paths.
- Standalone skill mutations must keep top-level `<root>/skills/SKILLS.md` consistent with `<root>/skills/<skill_key>/SKILL.md`. Deleting only the skill directory can hide a card from catalog loading while still advertising the deleted skill through prompts or `skills_list`.
- Agent root `SKILLS.md` is the authoritative agent overview/index and metadata surface for Agents page, lifecycle hooks, normal task loading, tool permissions, and related declarations. It should link to actual skill files and be updated idempotently by preserving existing links and upserting new ones.
- Do not treat agent root `SKILLS.md` as the canonical prompt container. Agent prompts/instructions should come from the appropriate `SKILL.md`; the root file is a human-readable overview and metadata/index surface.
- Legacy DB-backed agent skills (`models.Agent.Skills`) are compatibility data distinct from routed on-disk agent-owned skills. Migrate/materialize legacy DB-only agents safely into the global on-disk catalog, using clean slugs from display names rather than `legacy_` prefixes when possible, and clear or hide legacy concepts after successful migration.
- Legacy-agent materialization must preserve data and be idempotent. Discovering an existing on-disk agent during list-time materialization should not rewrite DB state from a stale in-memory copy without verifying importer/root-declaration sync behavior.
- Standalone skill declarations should stay compact and limited to current catalog/manager fields. Keep active compact selection metadata such as `routing.triggers`, `routing.priority`, and `routing.description`; do not reintroduce removed legacy agent-routing hint scaffolding.
- Generated or native OpenVibely agent/skill declarations must include the explicit frontmatter `kind` expected by the catalog/manager when required. Explicit import flows may accept standard Skills packages with top-level `name`/`description` frontmatter and convert them into OpenVibely standalone declarations while preserving safe bundled resource files.

Skill Curator and post-task learning:
- `observe_task_for_learning` is a Skill Curator `after_complete` hook, not execution as the task’s assigned primary agent. Mutation requires explicit lifecycle runtime tool grants for the hook owner.
- The hook should review the compacted backend LLM conversation snapshot used for the task, not persisted threads, diffs, summaries, execution artifacts, or separately invented truncation policies.
- Before saving learning, Skill Curator should inspect existing agents/skills as needed and avoid duplicate or already-covered learning. Reusable cross-agent improvements belong in standalone skills; assigned-agent updates are reserved for behavior specific to that assigned agent’s role, purpose, private workflow, or selected agent-owned skill.
- Hook context/tool descriptions should label assigned agent identity, purpose, selected agent-owned skills, selected standalone skills, provenance, and write policy explicitly; do not rely on path inference.
- If uncertain about placement, prefer standalone skill updates or a proposed-change outcome rather than mutating. Avoid bulk-copying standalone or unrelated skills into an agent.
- Assigned-agent skill mutation for post-task learning should use a server-scoped agent-owned mutation path such as `agent_skill_manage`, constrained to the current task’s assigned agent and selected/owned skills, not arbitrary `skill_manage` writes.
- Agents/hooks that create, change, consolidate, or retire skills must have the needed mutation or scoped-file access and preserve affected indexes. If a session lacks write access, it should report the required index/consolidation follow-up rather than claiming the mutation happened.

Lifecycle hooks and routing:
- Lifecycle hooks are implemented around `internal/lifecycle/` and task execution/server setup. Durable concepts include `route_task`, `before_run`, `after_complete`, `scheduled`, task-mode bookkeeping, blocking versus non-blocking execution, idempotency/audit rows, recursion prevention, and strict validation of hook types/tool access.
- `task_mode` represents the primary active task execution slot/bookkeeping and should not be exposed as an ordinary user-authored lifecycle hook unless deliberately redesigned.
- The `route_task` hook is an LLM routing decision over the prompt-safe agent index. It should not be confused with catalog/index generation.
- Route-task inputs must include the actual user task title/prompt being routed, not only `available_skills` and lifecycle metadata.
- Lifecycle hook skill resolution should be scoped to the hook owner. If the hook owner’s skill is missing, fail rather than falling back to the task turn’s selected/available catalog.
- Router output supports a JSON `skills` array. Preserve multiple selected skills from the winning router result; do not merge multiple route-hook outputs.
- Routing/effective-mode logic should have one primary agent/effective mode. Do not merge tool permissions across multiple agents or introduce multi-agent execution without explicit redesign.
- Ordinary tasks may intentionally have no assigned primary agent. UI should label this as no assigned agent or skill-routed/default behavior, not “Auto Agent.”
- Any task with an explicit assigned primary agent should skip skill routing and use that agent’s curated/default or manual skill selection. Skill routing must not override deliberate assignment, including tasks created from Chat orchestrate with an explicit `agent` Agent-definition name.
- Maintenance/system agents should be blocked from auto-routing through `selectable_as_primary=false`, not hardcoded name checks. That flag prevents router auto-selection but should not ban explicit user/API assignment, scheduled tasks, or other deliberate invocations.
- Task detail UI should show the selected primary task agent from persisted assignment/effective mode. Lifecycle activity rows identify hook executors, not necessarily the routed primary task agent.
- Lifecycle/task detail visibility should show prompt-safe structured hook decisions. `route_task` selected skills and `before_run`/`recall_memory` selected memory identifiers should render as compact badge/pill rows, while freeform hooks such as `observe_task_for_learning` remain prose summaries.
- Expanded lifecycle execution detail remains useful for debugging decisions, including prompt snapshots, tool calls, raw final output, validated JSON, duration, model, and provider, but must stay scoped to the selected hook/task context.
- A lifecycle hook `OutputContract` constrains the final structured result stored/validated by lifecycle code, not the agent’s working notes, tool use, or reasoning during the session.

Scheduled maintenance and UI direction:
- Prefer modeling scheduled maintenance as normal scheduled tasks assigned to agents and running through the usual task lifecycle, unless a runbook explicitly requires invisible background hooks. System-agent scheduled tasks should respect explicit assigned agent and selected/manual skill configuration instead of running ordinary `route_task` skill routing.
- Let loaded agent/skill declarations drive scheduled-task runtime tool grants rather than worker-side hardcoded maintenance tools. Memory-consolidation specifics live in `managed_memory.md`.
- Skill-library maintenance should use Skill Curator with `maintain_skill_library`. It may inspect agent namespaces and available skills for context, but should not create, edit, archive, route, reassign, or otherwise manage standalone user-controlled agents unless explicitly authorized.
- Lifecycle direct-call scoped-file setup must pass absolute directories for extra scopes such as `global_agents`, resolving configured/built-in roots before constructing `ScopedFiles` extras.
- The left navigation includes a standalone Skills page alongside Models, Agents, Channels, etc. It should use shared shell/sidebar conventions, searchable clickable cards, a kebab Edit/Delete menu, Agents-style scope badges, no displayed skill-key metadata line, and a frontmatter-seeded add modal for required OpenVibely YAML.
- Editing an existing standalone skill should show scope as disabled/read-only unless true project/global move semantics exist. Importing standalone skills should be an explicit Skills-page action that runs `SKILL.md` through the importer so `SKILLS.md` stays consistent, preserves safe package-relative files, and shows package files read-only in the edit modal.
- Desktop/Wails skill-package import should not rely solely on browser `webkitdirectory`; use a Wails/native folder-picker path or equivalent desktop-safe import flow because the desktop app uses OS-native WebViews.
- The agent create/edit dialog should align with on-disk agent-owned skills. Label the area simply as “Skills”; avoid “Agent-Owned Skills,” legacy Routing-tab fields, and Model Defaults JSON editing in Advanced for now.
- Lifecycle editing should focus on real hook slots, not `task_mode` as a normal hook. Fold permission/default tool policy into Lifecycle Hooks rather than a separate Permissions tab.
- Default `selectable_as_primary` to enabled for new-agent create flow and legacy conversions unless a source declaration explicitly says otherwise.
- Verify persisted allowed-tool configuration and the Agents page/editor state, not only model output or DB rows; an agent can appear created while having no tools enabled if permissions are not derived and saved.

End-to-end expectations:
- Lifecycle/skills work is incomplete unless wired end-to-end: build the skill catalog for each task turn, resolve hook skill bodies, run routing/effective-mode resolution before LLM execution, register correct runtime tools, make created agents/skills visible in the filesystem catalog, execute scheduled bindings, and log enough to debug behavior.
- Common audit gaps include UI without backend handlers, hook outputs not merged into prompts, route/effective decisions captured but unused, runtime tools registered with nil dependencies, and write-side mutations not visible until the intended refresh boundary.
- Do not treat ad-hoc project-scoped agents/skills created during tests as built-in seeded product behavior; distinguish runtime/user-created artifacts from embedded built-ins and migrations.
