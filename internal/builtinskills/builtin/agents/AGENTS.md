# OpenVibely Agents

This file is the global agent index. Agents are standalone user-managed configurations. The built-in Skill Curator can create and maintain skills through `skill_manage`, but it must not create, edit, archive, or auto-select agents.

Each `## <agent_key>` heading documents an existing system/manual agent root. Standalone routed skills live in the top-level `skills/` library; per-agent files here are implementation details for assigned/manual agents.

## skill_curator

System agent that ships with OpenVibely. It selects relevant skills for task turns, observes completed tasks for reusable skill learning, and maintains skill files/indexes. It is not user-selectable as a primary agent and does not manage agents autonomously.

Skills: see `skill_curator/SKILLS.md`.
