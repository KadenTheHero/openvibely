---
name: worktree_and_lineage
type: project
created: 2026-05-09
updated: 2026-05-13
source: manual_conversion
source_id: repo_root_MEMORY_md
confidence: high
title: Worktree and chained-task lineage
---

Task execution uses isolated git worktrees in `.worktrees/task_<id>` with task-scoped branches `task/<id_prefix>-<slug>`. LLM task prompts should include explicit worktree path orientation when a workdir is present, while runtime workdir enforcement remains the source of truth.

Worktree behavior:
- Auto-merge supports merge commit, fast-forward only, and squash merge.
- `WorktreeService` lives in `internal/service/worktree_service.go`.
- Migration `059_git_worktrees.sql` adds `worktree_path`, `worktree_branch`, `auto_merge`, `merge_target_branch`, and `merge_status` to tasks.
- UI includes a worktree info panel on task detail, merge buttons on Changes, and auto-merge toggles in create/edit forms.
- `LLMService.ExecuteTaskWithAgent` creates the worktree before execution, runs startup sync from latest main/default branch when the worktree is clean, and handles post-execution merge.
- Startup sync uses `git status --porcelain` dirty guard, logs explicit ran/skipped/failed outcomes, and aborts merge conflicts (`git merge --abort`) while marking `merge_status=conflict`.
- A stale `tasks.merge_status=conflict` can remain after Git aborts/cleans up the merge; triage by checking both checkout merge state (`MERGE_HEAD`/`git status`) and stored task merge status before attempting resolution.
- Manual merge conflicts from `/tasks/:id/worktree/merge` are handled results, not ordinary request failures: `WorktreeService.MergeBranch` sets `tasks.merge_status=conflict` and returns conflict file details; handlers should refresh the panel and emit an HTMX `openvibelyToast` so users see conflicts immediately.
- Local merges should not use a blanket dirty-target guard. Dirty-but-non-overlapping target checkout changes should be allowed; block only when there is an active conflict/merge state or Git reports local changes would be overwritten/conflict.
- Git overwrite/refusal cases that do not leave unmerged files, such as “local changes would be overwritten by merge,” should be surfaced as merge failures rather than conflict-resolution states: keep user files untouched, set `tasks.merge_status=failed`, return an error/toast with Git’s message, and do not show conflict controls. Only active unmerged-file states should set `merge_status=conflict` and show resolve/abort controls.
- When supporting dirty-but-non-overlapping target changes, merge/squash cleanup and commits need pre-merge status snapshots and ownership-aware handling so OpenVibely does not reset, unstage, delete, or commit pre-existing user work. Continue ignoring OpenVibely-managed untracked `.worktrees/` entries so task worktrees do not falsely dirty the target repository.
- Sequential local fast-forward merges should auto-update stale task branches before `--ff-only`: rebase task worktree branch onto current target, retry fast-forward merge, preserve true rebase conflicts with abort and actionable messaging. Do not generalize this pre-rebase behavior to merge-commit or squash merges without an explicit product decision, because that changes branch-history semantics rather than just satisfying `--ff-only`.
- Squash merge failure handling must clean/abort the squash state and leave the target checkout clean without hard-resetting the main/default target branch; otherwise staged squash changes can poison later operations and broad cleanup can destroy user work.
- Changes tab shows worktree branch diff vs target branch when available, falling back to execution diff.
- Changes-tab diff count triage should distinguish live branch-vs-target tip diffs from merge-base/triple-dot diffs: stale ancestry after squash/manual merges can surface many already-merged files and make the task appear far larger than its real delta.
- When a follow-up appears to undo already-merged work, verify the real target/default branch and task branch ancestry rather than trusting a linked worktree's local `main` ref alone; manual merges performed from inside task worktrees can make branch/ref state misleading during triage.
- When a task has an assigned worktree, agents must perform task edits in that worktree, not the main checkout. If accidental edits land in main, first inspect both `git status`/diffs, then move or recreate the relevant changes in the task worktree and restore main only after confirming nothing will be lost.
- If a task branch is already merged but `tasks.merge_status` is stale, Changes-tab handlers should fall back to preserved execution diff rather than showing empty live diff.
- If a task branch was fast-forward merged into the target/default branch, whether manually outside the app or through `/tasks/:id/worktree/merge`, the Changes tab should detect that merged state and hide/disable merge options instead of offering stale merge actions; do not let preserved execution diff content alone keep merge actions visible after `merge_status=merged`.
- Worktree merge HTMX flows must provide immediate visible feedback and refresh the user-visible surface that initiated the action, especially Changes-tab merge buttons. Merge menu clicks should close the dropdown, disable/show busy state for the clicked action while the request is in flight, and then refresh the Changes tab or show an error/toast; refreshing only `#worktree-info-panel` can make a successful fast-forward look like a no-op unless the Changes tab also receives a reliable refresh/event and hides stale merge actions. Keep busy feedback tied to the initiating control/action, not a separate after-menu status label, unless the user explicitly asks for that extra status UI.
- Scheduler runs cleanup scan every 5 minutes when worktree service is configured.

Cleanup behavior:
- Cleanup policy supports after-merge, keep, and manual.
- Detect manually merged branches via `IsBranchMerged()`.
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
