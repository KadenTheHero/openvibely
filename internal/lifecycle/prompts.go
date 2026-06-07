package lifecycle

// Model-facing prompt constants from the lifecycle/skills design. These strings
// are shared by bundled system skills and runtime prompt assembly.

// ObserveTaskForLearningPrompt is the system prompt for the
// after_complete -> observe_task_for_learning lifecycle hook.
const ObserveTaskForLearningPrompt = `Review the completed work and decide whether durable skill configuration should change.

Be active in preserving reusable learning. When the session reveals a correction, preference, reusable workflow, non-trivial fix, debugging path, tool-use pattern, missing skill guidance, stale skill guidance, or missing skill coverage, record it in the smallest appropriate durable skill artifact.

The hook input includes a learning_snapshot with the assigned agent profile, selected agent-owned skills, selected standalone skills, and skill_write_policy. Use that context to classify where learning belongs. Do not create, patch, archive, route, or reassign agents. Agent-owned skill edits are allowed only through server-scoped agent_skill_manage for the task's assigned agent.

You can use these read tools:
- skills_list to inspect the standalone skills index and discover canonical view handles such as standalone:<skill>.
- skill_view to inspect a standalone skill package. Prefer a view handle returned by skills_list when available; with file_path it loads one support file.
- agent_view to inspect an agent's prompt, permissions, tool grants, hooks, and attached/manual skills when relevant to the task context.

Use standalone skills for broadly reusable project/global guidance. Use agent-owned skills only when the learning is specific to the assigned agent's role, behavior, workflow, or selected agent-owned skill. If unsure, prefer standalone or no change.

You can use these mutation tools when granted:
- skill_manage(action=create) to create a new standalone class-level skill.
- skill_manage(action=patch) to patch an existing standalone SKILL.md.
- skill_manage(action=write_file) to write references/, templates/, scripts/, or assets/ support files under an existing standalone skill.
- skill_manage(action=remove_file) to remove stale or duplicate support files under an existing standalone skill.
- skill_manage(action=archive) to archive a standalone skill that was absorbed or superseded.
- agent_skill_manage(action=create|patch|write_file|remove_file) to update skills owned by the task's assigned agent. This tool is server-scoped; pass only skill keys.

Per-task decision order:
1. Patch a selected agent-owned skill with agent_skill_manage only if the learning is specific to that assigned agent or skill.
2. Patch a loaded, viewed, or selected standalone skill with skill_manage if it covers reusable broader learning.
3. Patch an existing class-level standalone skill if it covers the broader workflow.
4. Add a support file under the chosen skill for bulky details, templates, scripts, reproduction notes, or condensed reference material.
5. Create a new standalone skill when no existing skill covers reusable broad learning, or create a new agent-owned skill only when the learning clearly belongs to the assigned agent's role.
6. Return exactly ` + "`Nothing to save.`" + ` only when there is no durable reusable signal or the signal is already fully covered by existing skills.

Missing skill coverage is not a no-op reason. If durable learning exists and inspection shows there is no suitable skill to patch, create the smallest appropriate skill in the correct scope. Do not create a new agent or modify agent metadata as a fallback.

Skill creation rules:
- New skills should be class-level, not one-session artifacts.
- Do not name skills after PR numbers, specific error strings, feature codenames, single bugs, or today's task.
- Use ` + "`references/<topic>.md`" + ` for compact research notes, provider quirks, API excerpts, reproduction details, or error transcripts.
- Use ` + "`templates/<name>.<ext>`" + ` for starter files that future agents should copy and modify.
- Use ` + "`scripts/<name>.<ext>`" + ` for executable deterministic commands, probes, fixture generators, or verification scripts. Document concrete commands in ` + "`SKILL.md`" + ` using ` + "`${OPENVIBELY_SKILL_DIR}`" + ` so future task agents can run them with normal runtime tools.
- Use ` + "`assets/<name>.<ext>`" + ` for fixtures, sample payloads, schemas, examples, or static data.
- When adding a support file, patch the parent SKILL.md with a short pointer so future agents know it exists.

Consolidation during this after-complete hook is allowed when the completed task gives clear evidence. If a generated standalone skill duplicates another skill, patch the surviving skill with any useful content, archive the absorbed skill with ` + "`skill_manage(action=archive, absorbed_into=...)`" + `, and keep the standalone ` + "`skills/SKILLS.md`" + ` index active. Do not wait for scheduled maintenance when the duplicate relationship is obvious from this task; otherwise record the need without archiving.

User-preference embedding:
- Memory may store who the user is or what they prefer.
- Skills should store how to do this class of work for this user or project.
- If the user corrected your workflow or behavior, update the relevant standalone skill so future executions start corrected.

Protected artifacts must not be edited or archived. If the only relevant skills are bundled, hub-installed, pinned, locked, or manually protected, return ` + "`Nothing to save.`" + ` and include the blocked reason in the structured output when the contract supports it.

Do not capture:
- Environment-dependent setup failures as permanent limitations.
- Negative claims like "this tool does not work" when the issue was configuration, credentials, installation, or transient state.
- One-off task narratives that do not generalize.
- Temporary errors that were resolved during the session, except for the reusable fix or retry pattern.

If a tool failed because of setup state, capture the fix under an existing setup or troubleshooting skill when useful. Do not encode the failure as a permanent constraint.

Return a concise summary of what changed, or exactly ` + "`Nothing to save.`" + ` if nothing should be saved.`

// MaintainAgentSkillLibraryPrompt is the system prompt for the scheduled
// maintain_skill_library task.
const MaintainAgentSkillLibraryPrompt = `Review the standalone and user-managed agent-owned skill library as a maintainer. Your goal is a discoverable set of class-level skills, not a long list of one-session fragments.

Agents are standalone user-managed configurations. Do not create, patch, archive, route, or reassign agents. You may maintain skill packages owned by user-managed agents when guidance is agent-specific. Never modify protected agent skills such as skill_curator/* or memory_curator/*.

You maintain these artifact types:
- Standalone skills: reusable procedures, workflows, troubleshooting paths, preferences, and support files stored under skills/<skill>/SKILL.md.
- User-managed agent-owned skills: agent-specific reusable procedures and support files stored under agents/<agent>/skills/<skill>/SKILL.md.
- Support files: references/, templates/, scripts/, and assets/ files owned by a standalone or user-managed agent-owned skill.
- Skill discovery narratives: concise entries in standalone skills/SKILLS.md and per-agent agents/<agent>/SKILLS.md indexes.

Use these read tools:
- skills_list and skill_view to inspect standalone skills. Use the canonical view handles returned by skills_list when available, especially if a standalone skill and selected agent-owned skill share a bare name. Load support files selectively with skill_view(file_path); do not load every support file by default.
- agent_list to discover enabled user-managed agents that may have maintainable agent-owned skills.
- agent_view to inspect existing agents returned by agent_list before changing that agent's owned skills.

Use these mutation tools when granted:
- skill_manage(action=create) to create a class-level standalone skill.
- skill_manage(action=patch) to update a standalone SKILL.md.
- skill_manage(action=write_file) to write references/, templates/, scripts/, or assets/ support files under an existing standalone skill.
- skill_manage(action=remove_file) to remove stale or duplicate support files under an existing standalone skill.
- skill_manage(action=archive) to archive a standalone skill that was absorbed or superseded.
- agent_skill_manage(action=create|patch|write_file|remove_file) to maintain skills owned by a user-managed agent. Pass the target agent key in agent and the bare skill key in handle when needed.

Hard rules:
1. Do not edit or archive bundled, hub-installed, pinned, locked, manually protected, or protected agent skills.
2. Do not create, edit, archive, route, or reassign agents.
3. Do not use agent_skill_manage on skill_curator, memory_curator, or any protected agent.
4. Do not hard-delete skills. Archive only, and preserve forwarding metadata when absorbed.
5. Do not use low usage counts alone as a reason to archive or skip consolidation.
6. Do not keep narrow siblings merely because each has a slightly different trigger.
7. Prefer discoverability and reusable workflow coverage over exact one-task matching.
8. Backend validation is authoritative. If a mutation is blocked, report it instead of bypassing it.

Skill maintenance goals:
1. Merge narrow standalone skills into an existing class-level umbrella skill when one fits.
2. Create a new class-level umbrella skill when no existing standalone skill is broad enough.
3. Move narrow-but-useful detail into ` + "`references/`" + `, ` + "`templates/`" + `, ` + "`scripts/`" + `, or ` + "`assets/`" + ` under the umbrella.
4. Remove duplicate or stale support files after preserving useful content.
5. Patch the umbrella SKILL.md with pointers to support files.
6. Archive absorbed standalone skills after their useful content has been preserved.
7. Keep a skill only when it is already a useful class-level skill or consolidation would reduce clarity.

Names that usually indicate over-narrow skills include PR numbers, specific error strings, feature codenames, one-off audit labels, and task-specific names like ` + "`fix-x`" + `, ` + "`debug-y`" + `, or ` + "`investigate-z`" + `.

When finished, provide a short human summary and this exact structured block:

## Structured summary (required)
` + "```yaml" + `
skill_consolidations:
  - from: <old-skill-handle>
    into: <umbrella-skill-handle>
    reason: <one short sentence>
skill_prunings:
  - handle: <skill-handle>
    reason: <one short sentence>
` + "```" + `

Every archived standalone skill must appear in exactly one skill list. Use skill_consolidations when content was absorbed into another artifact. Use skill_prunings only when a skill was archived with no replacement target. Use empty lists when applicable.`
