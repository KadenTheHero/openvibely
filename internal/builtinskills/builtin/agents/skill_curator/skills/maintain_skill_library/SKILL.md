---
title: Maintain Skill Library
description: Scheduled maintenance for durable standalone skills.
routing:
  triggers:
    - scheduled
    - maintain library
    - maintain skills
  priority: 70
---

# Maintain Skill Library

Maintain the standalone skill library. Consolidate duplicate generated skills, prune stale generated skills, and keep skills focused and discoverable.

Agents are standalone user-managed configurations. Do not create, patch, archive, route, or reassign agents during scheduled maintenance. Use `agent_view` only to understand manually assigned agents when skill guidance mentions them.

## Read first

Use the read tools to inspect current state before changing anything:

- `skills_list` returns the raw top-level `skills/SKILLS.md` narrative for global and project scopes.
- `agent_view` returns one manually managed agent's configuration and embedded/manual skills.
- `skill_view` loads one standalone skill package by handle, returning `SKILL.md` plus linked support-file metadata.
- `skill_view` with `file_path` loads one support file such as `references/common-failures.md`, `templates/checklist.md`, `scripts/validate.sh`, or `assets/example.json`. Load support files selectively; do not load every support file by default.

## Make changes

Use mutation tools only for validated, durable standalone skill improvements. Do not hard-delete skills. Archive stale generated skills with a reason and replacement handle when appropriate.

- `skill_manage(action=create)` creates a standalone skill at `skills/<skill>/SKILL.md`.
- `skill_manage(action=patch)` updates an existing standalone `SKILL.md`.
- `skill_manage(action=write_file)` writes `references/`, `templates/`, `scripts/`, or `assets/` support files under an existing standalone skill.
- `skill_manage(action=remove_file)` removes stale or duplicate support files from an existing standalone skill.
- `skill_manage(action=archive)` archives a generated standalone skill that was absorbed or superseded.

Agent creation, editing, archival, and skill attachment decisions belong to users in the create/edit agent dialog. Scheduled maintenance only changes standalone generated skills.

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

The top-level `skills/SKILLS.md` files are narrative discovery indexes for standalone generated skills. Agent-owned/system implementation skills are not routed and should not be added to this index.

After any successful `skill_manage` write, the mutation tool maintains the top-level `skills/SKILLS.md` skill-link index. Use `skills_list` to verify the result when needed, but do not attempt direct index-file edits.

If `skills/SKILLS.md` is missing, empty, or appears out of sync with the on-disk standalone `skills/<skill>/SKILL.md` tree, use `skill_manage` on the relevant existing skill so the mutation layer repairs the minimal skill-discovery index. Do not invent new agents to organize skills.

## Return value

Return only JSON matching the `library_update_summary` contract. Skill archives must appear in exactly one consolidation or pruning entry. Leave all agent-related arrays empty.
