---
name: coding_agent_product_discipline
type: feedback
created: 2026-05-11
updated: 2026-05-11
source: thread
source_id: a19496c4905c3efc4db7b1941a7528ca
confidence: high
title: Coding Agent Product Discipline
---

When implementing product behavior, identify the underlying product concept before coding. Do not derive major behavior from incidental implementation shape such as tool lists, default flags, or temporary code structure; model product policy explicitly in configuration, state, or data model when it affects workflow, isolation, data writes, recovery, or review.

Avoid making generic capabilities carry hidden special-case behavior for one built-in use case. If one built-in agent or workflow needs exceptional behavior, express it through that agent/workflow's explicit configuration while keeping the generic feature predictable for all users.

Treat similarly named roots as distinct concepts. Before resolving relative paths or write locations, determine whether the intended root is the project root, isolated worktree root, process working directory, durable repo location, or tool scope root. Preserve that meaning through storage and runtime resolution rather than casually switching roots.

Do not defer configuration correctness to runtime failures. Users should not be able to save invalid settings that will predictably fail later when the invalid state can be detected at configuration time.

Do not add legacy compatibility, aliases, migrations, or helper wrappers for bad abstractions unless there is actual shipped or persisted legacy state to support. Intermediate implementation attempts during the same feature are not legacy product states; fix the abstraction instead.

Prefer product-correct defaults over mechanically convenient defaults. For UI and workflow changes, optimize for clarity, reversibility, user intent, and useful behavior rather than merely making tests pass or minimizing implementation effort.

When drafting reusable coding-agent context or instructions, favor generalized patterns and broad behavioral rules over narrow, specific examples. The user values useful improvisation, but it must be constrained by explicit assumption discipline rather than unsupported product or architecture guesses.
