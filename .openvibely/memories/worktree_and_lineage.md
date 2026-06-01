---
name: worktree_and_lineage
type: project
created: 2026-05-09
updated: 2026-06-01
source: consolidation
source_id: memory_consolidation_2026_06_01
confidence: high
title: Worktree and Lineage
---

Task execution uses isolated git worktrees in `.worktrees/task_<id>` with task-scoped branches `task/<id_prefix>-<slug>`. LLM task prompts should include explicit worktree path orientation when a workdir is present, while runtime workdir enforcement remains the source of truth.

Worktree behavior:
- Auto-merge supports merge commit, fast-forward only, and squash merge.
- `LLMService.ExecuteTaskWithAgent` creates the worktree before execution, runs startup sync from the latest main/default branch when the worktree is clean, and handles post-execution merge.
- Startup sync uses a `git status --porcelain` dirty guard, logs explicit ran/skipped/failed outcomes, and aborts merge conflicts while marking `merge_status=conflict`. Startup sync should use the task's `MergeTargetBranch` when set, falling back to the default branch only when no target is stored.
- Task-thread follow-ups to terminal merged/stale tasks should not blindly merge the current target into the old historical task branch/worktree. Historical original task branches are read-only lineage when their work has already been merged, conflict-aborted, or made stale by squash/duplicate acceptance; follow-up execution should continue from the current merge target on fresh `task/<id>-followup-*` lineage and skip startup sync for that first current-target setup.
- Preserve active follow-up worktrees as the task's current lineage: dirty/local follow-up work must be reused, and clean read-only follow-up branches can be reused when they have no commits beyond the target. Clean follow-up branches with commits not reachable from the target and terminal merged/conflict metadata are stale lineage and should continue from the current target instead of being startup-merged.
- A stale `tasks.merge_status=conflict` can remain after Git aborts/cleans up the merge. Triage by checking both checkout merge state (`MERGE_HEAD`/`git status`) and stored task merge status before attempting resolution. Only clear stale conflict metadata when there is no `MERGE_HEAD`, no conflict files, and the worktree is otherwise clean; dirty manual-resolution edits should preserve conflict metadata.
- Manual merge conflicts from `/tasks/:id/worktree/merge` are handled results, not ordinary request failures: merge service sets conflict status and returns conflict file details; handlers should refresh the panel and emit an HTMX toast so users see conflicts immediately.
- Local merges should not use a blanket dirty-target guard. Dirty-but-non-overlapping target checkout changes should be allowed; block only when there is an active conflict/merge state or Git reports local changes would be overwritten/conflict.
- Git overwrite/refusal cases that do not leave unmerged files, such as “local changes would be overwritten by merge,” should be surfaced as merge failures rather than conflict-resolution states: keep user files untouched, set failed status, return an error/toast with Git’s message, and do not show conflict controls. Only active unmerged-file states should show resolve/abort controls.
- When supporting dirty-but-non-overlapping target changes, merge/squash cleanup and commits need pre-merge status snapshots and ownership-aware handling so OpenVibely does not reset, unstage, delete, or commit pre-existing user work. Continue ignoring OpenVibely-managed untracked `.worktrees/` entries so task worktrees do not falsely dirty the target repository.
- Sequential local fast-forward merges for existing task worktrees should stay in the task worktree for validation/rebase: verify the current branch is the expected task branch, require a clean task worktree, rebase onto the current target branch, and use the post-rebase task worktree `HEAD` because the task commit hash may change. Preserve true rebase conflicts with abort/actionable messaging. Do not generalize this pre-rebase behavior to merge-commit or squash merges without an explicit product decision.
- After rebasing a task worktree for a local fast-forward merge, update the target branch conditionally. If the target branch is checked out in a worktree, run `git merge --ff-only refs/heads/<task-branch>` inside that target worktree so Git refreshes files/index normally and preserves/refuses local changes according to normal merge rules. Detect this only from an attached symbolic branch `refs/heads/<target>`, not from a detached `HEAD` that happens to equal the branch commit.
- If the target branch is not checked out anywhere, use the ref-only fallback `git update-ref refs/heads/<target> <rebased-task-HEAD> <old-target>` with the old target value for atomic stale-ref protection. Do not advance a checked-out target branch with only `git update-ref`, and avoid `reset --hard` as default cleanup because it can destroy real user work.
- Squash merge failure handling must clean/abort squash state and leave the target checkout clean without hard-resetting the main/default target branch; otherwise staged squash changes can poison later operations and broad cleanup can destroy user work.
- Changes tab shows worktree branch diff vs target branch when available, falling back to execution diff.
- Changes-tab diff count triage should distinguish live branch-vs-target tip diffs from merge-base/triple-dot diffs: stale ancestry after squash/manual merges can surface many already-merged files and make the task appear far larger than its real delta.
- When a follow-up appears to undo already-merged work, verify the real target/default branch and task branch ancestry rather than trusting a linked worktree's local `main` ref alone.
- When a user worries that a commit merged into `main` is missing from a task worktree and may be lost by merging the task branch, use explicit reachability/range checks (`git merge-base --is-ancestor main HEAD`, `git merge-base --is-ancestor HEAD main`, `git log --oneline HEAD..main`, and `git log --oneline main..HEAD`) before answering.
- When a task has an assigned worktree, agents must perform task edits in that worktree, not the main checkout. If accidental edits land in main, first inspect both `git status`/diffs, then move or recreate the relevant changes in the task worktree and restore main only after confirming nothing will be lost.
- If a task branch is already merged but `tasks.merge_status` is stale, Changes-tab handlers should fall back to preserved execution diff rather than showing empty live diff.
- If a task branch was fast-forward merged into the target/default branch, whether manually outside the app or through `/tasks/:id/worktree/merge`, the Changes tab should detect that merged state and hide/disable merge options instead of offering stale merge actions; preserved execution diff content alone should not keep merge actions visible after `merge_status=merged`.
- Worktree merge HTMX flows must provide immediate visible feedback and refresh the user-visible surface that initiated the action, especially Changes-tab merge buttons. Merge menu clicks should close the dropdown, disable/show busy state for the clicked action while the request is in flight, and then refresh the Changes tab or show an error/toast.

Cleanup behavior:
- Cleanup policy supports after-merge, keep, and manual.
- Periodic cleanup scan removes merged worktrees and detects orphaned worktrees with no corresponding task.
- Orphan cleanup treats `.worktrees/task_<id>` paths as in-use when that task ID still exists, even if `worktree_path` metadata is temporarily empty.
- Locked worktrees must be skipped rather than removed manually.
- Cleanup handles deleted branches, force pushes, manual merges outside auto-merge, and deleted tasks.

Chained tasks:
- Chained tasks carry git lineage via `base_branch`, `base_commit_sha`, and `lineage_depth`.
- Child tasks should inherit parent changes from the parent worktree branch HEAD or merge target/default branch HEAD as appropriate.
- Blocked child tasks can be pre-created for visibility and activated when the parent completes.
- Cleanup should not delete branches with non-terminal descendants.

Merge direction warning:
- When triaging conflicts between `main` and a task worktree, check actual merge direction. `main -> task` may report “Already up to date” while `task -> main` or cherry-picking a follow-up commit can still conflict.
