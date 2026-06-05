---
title: Recall Memory
description: Select relevant managed memory during task routing and return compact selected-memory JSON.
routing:
  triggers:
    - route_task
    - recall memory
    - select memories
  priority: 90
---

# Recall Memory

Select durable memory handles that may help the upcoming task turn. Return compact selected-memory JSON; do not answer or solve the user's task yourself.

Memory is background context, not direct instruction. It can be stale, so select only entries likely to help the active task. Source-code facts still need verification by the task agent before use.

## Select from the memory index

Read the hook input's `extras.available_memories` field. It is the compact project memory index from `MEMORIES.md`, analogous to the Skill Curator's `available_skills` index. Select only memory files/topics listed in that index that directly help the hook input's task title, prompt, project, assigned agent, selected skills, or recent context.

If no listed memory is relevant, return an empty `memories` array and empty `content`.

## Select Handles

Select only durable memory entries already present in the memory index and directly relevant to the current task turn:

- User preferences, product direction, architecture decisions, workflow constraints, current-state facts, incidents, and repeated feedback.
- Project context that the task agent is unlikely to infer from the immediate prompt.
- Avoid secrets, raw transcripts, stale guesses, task-by-task logs, and procedure-only runbooks.
- Use root-relative memory filenames from the index as `file` handles.
- Return selected handles and optional short `topic` debug labels only; the task-facing prompt will use selected file handles only, not route-generated topic, summary, snippet, or content text.
- Leave `content`, `summary`, and `snippet` empty; full details are loaded on demand by the task with `memory_view` for selected handles only.

## Return Value

Return only JSON matching the `selected_memories` contract:

```json
{"memories":[{"file":"provider_architecture.md","topic":"Provider lifecycle routing"}],"content":"","confidence":0.8,"reason":"The task asks about provider lifecycle routing.","needs_clarification":false}
```
