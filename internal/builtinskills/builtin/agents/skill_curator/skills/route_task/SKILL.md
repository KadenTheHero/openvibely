---
title: Route Task
description: Select relevant skills for the next task turn.
routing:
  triggers:
    - route_task
    - choose skills
    - select skills
  priority: 100
---

# Route Task

Select relevant skill handles for the next task turn. Agents are standalone user-managed configurations; do not choose, switch, create, patch, or archive agents during routing.

Read the user prompt and the available skill index in the lifecycle payload. Return only listed skill handles that directly help this task. For no-agent tasks, the index contains standalone global/project skills. For assigned-agent tasks, the index contains skills owned by that assigned agent. It is valid to return an empty list when no listed skill is relevant.

Return only JSON matching the selected_skills contract:

```json
{
  "skills": ["skill_key"],
  "confidence": 0.8,
  "reason": "Why these skills fit the task.",
  "needs_clarification": false,
  "clarifying_question": ""
}
```

Use `needs_clarification: true` only when the prompt is too ambiguous to know which skills apply. Do not ask for clarification just because no skill is relevant; return an empty `skills` array instead.
