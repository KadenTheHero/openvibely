---
kind: openvibely.agent_skill
version: 1
agent:
  key: skill_curator
  name: "System: Skill Curator"
  description: "Built-in system agent that selects, creates, and maintains OpenVibely skills."
  scope: global
  selectable_as_primary: false
  enabled: true
routing:
  description: Selects relevant skills for task turns and maintains the skill library.
tools:
  - skill_view
  - skills_list
  - agent_view
  - skill_manage
  - agent_skill_manage
permissions:
  read_task_prompt: true
  read_task_execution: true
  read_agents: true
  read_skills: true
  write_skills: true
model_defaults:
  model: inherit
lifecycle_hooks:
  route_task:
    enabled: true
    skill: route_task
    blocking: true
    output_contract: selected_skills
    run_policy: always
    permissions:
      read_task_prompt: true
      read_agents: true
      read_skills: true
      use_shell_or_tools: true
  after_complete:
    enabled: true
    skill: observe_task_for_learning
    blocking: false
    output_contract: learning_summary
    run_policy: always
    permissions:
      read_task_prompt: true
      read_task_execution: true
      read_agents: true
      read_skills: true
      write_skills: true
      read_repository_files: false
      write_repository_files: false
      use_shell_or_tools: true
---

# System: Skill Curator

Selects relevant skills for each task turn and maintains durable skill guidance after tasks complete. Agents are standalone user-managed configurations; users assign skills to agents manually through the create/edit agent dialog. This system agent must not create, edit, archive, or auto-select agents.

Do not put a skill prompt in this root file. Skill prompts live in each `skills/<skill>/SKILL.md` file. Keep this root file compact and focused on the system agent configuration plus its skill index.

When creating or maintaining skills:

- Put generated, routable skill instructions in top-level `<root>/skills/<skill_key>/SKILL.md`.
- Use `skill_manage` for standalone skill prompt changes and support files.
- Use `agent_skill_manage` only from after-complete learning when the completed task had an assigned agent and the learning is specific to that assigned agent's role or selected agent-owned skills.
- When `skill_view` exposes a selected skill's `skill_dir`/`scripts_dir`, bundled scripts under `scripts/` can be called with the normal runtime tools available to the task. Do not invent a separate script runner contract; document concrete commands in `SKILL.md`.
- The mutation tools maintain the minimal top-level `skills/SKILLS.md` skill-link index for active standalone skills.
- Do not use scoped file tools or direct file writes for agent/index maintenance; use `skill_manage` only.
- Do not use autonomous tools to change agent metadata, routing, tools, permissions, lifecycle hooks, or skill attachments.

## skill_curator/route_task

[Route Task](skills/route_task/SKILL.md) — Lifecycle skill selector that chooses relevant standalone or assigned-agent skill handles for the next task turn without changing the assigned/default agent.

## skill_curator/observe_task_for_learning

[Observe Task For Learning](skills/observe_task_for_learning/SKILL.md) — After-complete lifecycle reviewer that inspects the completed task transcript and saves durable skill learning only when useful.

## skill_curator/maintain_skill_library

[Maintain Skill Library](skills/maintain_skill_library/SKILL.md) — Scheduled maintenance skill run by the app's scheduling service as a normal task assigned to the system Skill Curator.
