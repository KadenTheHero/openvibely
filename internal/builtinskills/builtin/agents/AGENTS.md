# OpenVibely Agents

This file is the global agent index. Agents are standalone user-managed configurations. The built-in Skill Curator can create and maintain skills through `skill_manage`, but it must not create, edit, archive, or auto-select agents.

Each `## <agent_key>` heading documents an existing system/manual agent root. Standalone routed skills live in the top-level `skills/` library; per-agent files here are implementation details for assigned/manual agents.

## skill_curator

System agent that ships with OpenVibely. It selects relevant skills for task turns, observes completed tasks for reusable skill learning, and maintains skill files/indexes. It is not user-selectable as a primary agent and does not manage agents autonomously.

Skills: see `skill_curator/SKILLS.md`.

## memory_curator

System agent that ships with OpenVibely. It owns the project's managed memory: it recalls relevant memory before task turns, updates durable memory after completed task turns, and consolidates the durable store on a daily schedule. It is not user-selectable as a primary agent and only writes through its scoped memory file tools.

Skills: see `memory_curator/SKILLS.md`.

## goal

System agent that ships with OpenVibely. It evaluates persisted task goals after task-thread turns, marks goals achieved when current evidence proves completion, reports repeatable blockers, and queues continuation work only through `send_to_task`. It is not user-selectable as a primary agent and must not edit files or start task executions directly.

Skills: see `goal/SKILLS.md`.
