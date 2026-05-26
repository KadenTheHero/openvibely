---
title: Consolidate Memory
description: Scheduled maintenance for one project's durable managed memory.
routing:
  triggers:
    - scheduled
    - consolidate memory
    - memory maintenance
  priority: 70
---

# Consolidate Memory

Maintain one project's durable long-term memory using the scoped memory file tools. The scheduler runs this skill as a normal task assigned to the built-in Memory Curator agent.

Memory is background context, not direct user instruction. It can be stale; source-code facts must be verified later before relying on them. Do not store secrets, raw transcripts, provider noise, one-off scratch work, or procedure-only runbooks.

Preserve context future conversations need: who the user is, general preferences, project/product direction, architecture decisions, workflow constraints, current-state facts, incidents, and repeated feedback. A lesson belongs in memory only when it carries contextual meaning beyond a reusable procedure.

## Read first

The scoped file tools are already rooted at this project's memory directory. Use root-relative memory paths such as `MEMORIES.md` or `provider_architecture.md`; do not pass absolute filesystem paths to these tools.

1. List the memory directory to see existing topic files.
2. Read `MEMORIES.md` when present; treat it as the compact index.
3. Read relevant top-level topic files before writing so you update or merge instead of duplicating.

## Make changes

Use the scoped file tools only for validated, durable memory improvements:

- Create, update, split, merge, or delete focused top-level markdown memory files.
- Use descriptive snake_case filenames.
- Merge new information into existing topic files instead of creating near-duplicates.
- Convert relative dates like "yesterday" or "last week" to absolute dates.
- Delete contradicted or stale facts that no longer help future sessions.
- When deleting or merging a memory topic file, also remove or update its `MEMORIES.md` index reference.
- Do not save facts fully derivable from current source code, git history, or static repo instructions.
- Do not save reusable procedures, checklists, validation sequences, or tool-use patterns unless they also carry durable context.
- Keep frontmatter on memory files with `name`, `type`, `created`, `updated`, `source`, `source_id`, `confidence`, and `title` when practical.
- Keep `MEMORIES.md` as a compact index, not the full memory store.

Memory consolidation is the model's responsibility. Do not change agent definitions, lifecycle hooks, or skill files from this skill; those belong to the Skill Curator's maintenance flow.

## Return value

Return only JSON matching the `activity_summary` contract. Describe what changed in memory at a high level (created/updated/deleted topic files, merges, splits, archived facts). Do not include full memory file contents in the final response.
