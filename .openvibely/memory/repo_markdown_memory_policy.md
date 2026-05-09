---
name: repo_markdown_memory_policy
type: feedback
created: 2026-05-09
updated: 2026-05-09
source: manual_conversion
source_id: user_feedback_markdown_conversion
confidence: high
title: Repo markdown memory policy
---

Do not use repo-root MEMORY.md as active task/chat memory. Durable architecture decisions, user preferences, implementation feedback, repeated pitfalls, and task/chat lessons should go into OpenVibely managed memory through MemoryService.

Repo-root MEMORY.md has been removed; active durable context lives under `.openvibely/memory/`. Do not recreate a root MEMORY.md compatibility pointer. If old branches contain durable architecture or workflow content in root MEMORY.md, migrate the durable parts into focused `.openvibely/memory/*.md` files instead of restoring the root file.

Keep AGENTS.md, guardrails.md, and PRACTICES.md as static operating instructions. Avoid adding feature-history or task-summary content to repo-root markdown.
