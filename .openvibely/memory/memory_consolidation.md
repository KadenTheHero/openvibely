---
name: memory_consolidation
type: project
created: 2026-05-09
updated: 2026-05-12
source: consolidation
source_id: memory_consolidation_2026_05_12
confidence: high
title: Memory Consolidation
---

Memory Consolidation should run as a normal scheduled task assigned to the built-in Memory Consolidator agent. Each project should get a real system-created scheduled task, visible on the Schedule page like other scheduled tasks, while remaining hidden from the normal Tasks board. Avoid hidden-run behavior, separate memory-specific schedule cards, parallel schedule tables, bespoke schedule status/run-history paths, or prompt-display special cases when existing scheduler and task-thread features can represent the behavior.

Bootstrap may create or repair the Memory Consolidator agent and schedule, but runtime execution should use the generic scheduled-agent path. `agents.system_kind = "memory_consolidator"` may remain for bootstrap/UI identity, but it must not trigger special prompt swapping, special tool grants, or post-run hooks in `LLMService`.

The durable consolidation task is identified by the system Memory Consolidator agent/profile and the system task title, then uses a normal `schedules` row.

The scheduled task prompt and agent system prompt should express consolidation policy: list the scoped memory directory, read `MEMORY.md`, inspect relevant topic files, merge/prune/split/rewrite durable memory, maintain index references, avoid transient logs/secrets/raw transcripts, and summarize changes.

The Memory Consolidator should get filesystem access through generic `ScopedFiles` tool config, not a memory-specific tool/profile:

```text
ScopedFiles
Directory: .openvibely/memory
Permissions: read/write/delete
Skip default tools: true
```

Use normal task/execution status, output, error, and alert mechanisms for scheduled consolidation results. Do not add a separate scheduled-consolidation runtime branch or duplicate status/error/notes/touched-path metadata unless a clear product/UI reason appears later.

The scoped file executor should stay generic: enforce only hard capability boundaries such as project-relative scope resolution, explicit read/write/delete permissions, absolute-path rejection, traversal rejection, and symlink escape rejection. Memory-specific policy belongs in prompts/config, not in generic tool code.
