---
kind: openvibely.agent_skill
version: 1
agent:
  key: memory_curator
  name: "System: Memory Curator"
  description: "Built-in system agent that selects, updates, and consolidates OpenVibely managed project memory."
  scope: global
  selectable_as_primary: false
  enabled: true
routing:
  description: Selects, updates, and consolidates managed project memory.
tools:
  - ScopedFiles
tool_config:
  scoped_files:
    - directory: .openvibely/memories
      permissions:
        - read
        - write
        - delete
  skip_default_tools: true
  disable_runtime_worktree: true
permissions:
  read_task_prompt: true
  read_task_execution: true
  read_project_memory: true
  write_project_memory: true
model_defaults:
  model: inherit
lifecycle_hooks:
  before_run:
    enabled: true
    skill: recall_memory
    blocking: true
    output_contract: context_block
    run_policy: always
    permissions:
      read_task_prompt: true
      read_project_memory: true
      use_shell_or_tools: true
  after_complete:
    enabled: true
    skill: update_memory
    blocking: false
    output_contract: activity_summary
    run_policy: always
    permissions:
      read_task_prompt: true
      read_task_execution: true
      read_project_memory: true
      write_project_memory: true
      use_shell_or_tools: true
---

# System: Memory Curator

Built-in system agent that owns OpenVibely's managed project memory. The same agent handles all memory lifecycle work:

- Select relevant memory before task turns and return concise context blocks.
- Update durable memory after completed task turns when the transcript contains memory-worthy facts.
- Consolidate the durable memory store on a daily schedule through a normal scheduled task assigned to this agent.

Do not put a skill prompt in this root file. Skill prompts live in each `skills/<skill>/SKILL.md` file. Keep this root file compact and focused on the system agent configuration plus its skill index.

This agent is not user-selectable as a primary task agent. It writes only through its scoped memory file tools and does not get a runtime git worktree. Memory curation is the model's responsibility through lifecycle skills; do not add Go-side deterministic topic classifiers or task-turn memory extractors that reinterpret completed runs outside lifecycle hooks.

## memory_curator/recall_memory

[Recall Memory](skills/recall_memory/SKILL.md) — Before-run lifecycle skill that selects relevant managed memory and returns a compact context block for the task turn.

## memory_curator/update_memory

[Update Memory](skills/update_memory/SKILL.md) — After-complete lifecycle skill that reviews the completed task transcript and updates durable managed memory when useful.

## memory_curator/consolidate_memory

[Consolidate Memory](skills/consolidate_memory/SKILL.md) — Scheduled maintenance skill run by the app's scheduling service as a normal task assigned to the system Memory Curator agent.
