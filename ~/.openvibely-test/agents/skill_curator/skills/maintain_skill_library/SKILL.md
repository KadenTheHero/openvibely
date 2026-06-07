---
title: Maintain Skill Library
description: Scheduled maintenance for durable standalone and user-managed agent-owned skills.
routing:
  triggers:
    - scheduled
    - maintain library
    - maintain skills
  priority: 70
---

# Maintain Skill Library

Maintain the skill library across standalone skills and user-managed agent-owned skills. Consolidate duplicate standalone skills, prune stale standalone skills, and keep reusable skill guidance focused and discoverable.

Agents are standalone user-managed configurations. Do not create, patch, archive, route, reassign, or change agent metadata during scheduled maintenance. You may maintain skill packages owned by user-managed agents when the guidance is agent-specific. Never modify protected agent skills, including `skill_curator/*` and `memory_curator/*`; backend protection rejects those writes.

## Read first

Use the read tools to inspect current state before changing anything:

- `skills_list` returns the top-level `skills/SKILLS.md` narrative for global and project standalone scopes plus canonical `view_handle` values such as `standalone:<skill>`.
- `agent_list` returns enabled user-managed agents that may have maintainable agent-owned skills; use it to discover agent keys before inspecting or changing agent-owned skill packages.
- `agent_view` returns one user-managed agent's configuration and embedded/manual skills; use it to understand that agent's responsibilities before changing its owned skills.
- `skill_view` loads one standalone or selected skill package by handle, returning `SKILL.md` plus linked support-file metadata. Prefer the qualified `view_handle` returned by `skills_list` or agent-owned views; bare handles may be rejected when standalone and selected agent-owned skills share a name.
- `skill_view` with `file_path` loads one support file such as `references/common-failures.md`, `templates/checklist.md`, `scripts/validate.sh`, or `assets/example.json`. Load support files selectively; do not load every support file by default.

## Make changes

Use mutation tools only for validated, durable skill improvements. Do not hard-delete skills. Archive stale standalone skills with a reason and replacement handle when appropriate.

- `skill_manage(action=create)` creates a standalone skill at `skills/<skill>/SKILL.md`.
- `skill_manage(action=patch)` updates an existing standalone `SKILL.md`.
- `skill_manage(action=write_file)` writes `references/`, `templates/`, `scripts/`, or `assets/` support files under an existing standalone skill.
- `skill_manage(action=remove_file)` removes stale or duplicate support files from an existing standalone skill.
- `skill_manage(action=archive)` archives a standalone skill that was absorbed or superseded.
- `agent_skill_manage(action=create|patch)` creates or updates a skill package under `agents/<agent>/skills/<skill>/SKILL.md` for a user-managed agent. Pass the target agent key in `agent`, the bare skill key in `handle` when needed, and never include `agent/skill` in `handle`.
- `agent_skill_manage(action=write_file|remove_file)` writes or removes `references/`, `templates/`, `scripts/`, or `assets/` support files under an existing user-managed agent-owned skill.

Agent creation, editing, archival, routing, and skill attachment decisions belong to users in the create/edit agent dialog. Scheduled maintenance may change standalone skills and user-managed agent-owned skill packages only; it must not change agent metadata or protected agent skills.

## Skill Substance

Keep or create a skill only when it provides reusable execution guidance: concrete steps, checks, pitfalls, tool-use patterns, validation rules, command examples, or other procedures for a recurring class of work.

Consolidate or archive skills that mostly contain background context, status notes, user/profile facts, task summaries, or project facts without actionable procedural value.

When context affects execution, preserve only the actionable rule, checklist item, pitfall, or decision point in the skill. Avoid carrying over the full background narrative.

## Skill Package Hygiene

Keep `SKILL.md` short enough to route and read quickly. Move bulky examples, templates, scripts, schemas, fixtures, and long references into support files instead of bloating the main skill body.

Use support directories consistently:

- `references/` for long explanations, API notes, troubleshooting matrices, and background details that support the procedure.
- `templates/` for reusable prompt, config, patch, checklist, or document templates.
- `scripts/` for executable helper scripts that selected-skill task turns can call with normal runtime tools using the `skill_dir`/`scripts_dir` paths returned by `skill_view`.
- `assets/` for fixtures, sample payloads, schemas, examples, and static data.

During consolidation, preserve useful support files from absorbed skills, remove duplicate or stale support files, and update `SKILL.md` to point readers toward support files only when they are relevant.

## Maintain the index files

The top-level `skills/SKILLS.md` files are narrative discovery indexes for standalone skills. Agent-owned skills are discovered through their agent packages and should not be added to this standalone index.

After any successful `skill_manage` write, the mutation tool maintains the top-level `skills/SKILLS.md` skill-link index. Use `skills_list` to verify the result when needed, but do not attempt direct index-file edits.

If `skills/SKILLS.md` is missing, empty, or appears out of sync with the on-disk standalone `skills/<skill>/SKILL.md` tree, use `skill_manage` on the relevant existing skill so the mutation layer repairs the minimal skill-discovery index. Do not invent new agents to organize skills.

## Return value

Return only JSON matching the `library_update_summary` contract. Skill archives must appear in exactly one consolidation or pruning entry. Summarize any user-managed agent-owned skill changes clearly, and explicitly report when protected agent skills were skipped rather than modified.
