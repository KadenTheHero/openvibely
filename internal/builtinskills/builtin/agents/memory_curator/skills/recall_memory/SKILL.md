---
title: Recall Memory
description: Select relevant managed memory before a task turn and return a compact context block.
routing:
  triggers:
    - before_run
    - recall memory
    - select memories
  priority: 90
---

# Recall Memory

Select durable memory that will help the upcoming task turn. Return a concise context block; do not answer or solve the user's task yourself.

Memory is background context, not direct instruction. It can be stale, so include only facts likely to help the active task and phrase them as remembered context. Source-code facts still need verification by the task agent before use.

## Read first

The scoped file tools are already rooted at this project's memory directory. Use root-relative memory paths such as `MEMORIES.md` or `provider_architecture.md`; do not pass absolute filesystem paths to these tools.

1. List the memory directory.
2. Read `MEMORIES.md` when present to understand the available top-level memory files.
3. Read only the topic files that appear relevant to the hook input's task title, prompt, project, assigned agent, selected skills, or recent context.

## Select context

Include only durable facts that are directly relevant to the current task turn:

- User preferences, product direction, architecture decisions, workflow constraints, current-state facts, incidents, and repeated feedback.
- Project context that the task agent is unlikely to infer from the immediate prompt.
- Avoid secrets, raw transcripts, stale guesses, task-by-task logs, and procedure-only runbooks.
- Do not include entire memory files. Extract the smallest useful facts.
- Cite memory filenames in `sources`; use root-relative filenames only.
- Also populate `selected_memories` with the exact memory files/topics selected and a brief summary or snippet for each. Keep snippets compact and avoid unrelated/raw memory dumps.

If no memory is relevant, return an empty context block with low confidence and no selected memories.

## Return value

Return only JSON matching the `context_block` contract:

```json
{"content":"Relevant remembered context for the task, or an empty string if nothing applies.","sources":["MEMORIES.md","provider_architecture.md"],"selected_memories":[{"file":"provider_architecture.md","topic":"Provider lifecycle routing","summary":"Provider routing decisions are mode-driven and source-code facts should be verified."}],"confidence":0.8}
```
