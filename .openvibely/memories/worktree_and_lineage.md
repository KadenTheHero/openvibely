---
name: worktree_and_lineage
type: project
created: 2026-05-09
updated: 2026-06-07
source: consolidation
source_id: memory_consolidation_2026_06_07
confidence: high
title: Worktree and Lineage
---

Task execution uses isolated git worktrees in `.worktrees/task_<id>` with task-scoped branches `task/<id_prefix>-<slug>`. LLM task prompts include explicit worktree path orientation when a workdir is present, while runtime workdir enforcement remains the source of truth.

Durable worktree model:
- Auto-merge supports merge commit, fast-forward only, and squash merge.
- `LLMService.ExecuteTaskWithAgent` creates the worktree before execution, runs startup sync from the latest main/default branch when the worktree is clean, and handles post-execution merge.
- Startup sync uses the task's `MergeTargetBranch` when set, falling back to the default branch only when no target is stored.
- Changes tab shows worktree branch diff vs target branch when available, falling back to execution diff.
- Cleanup policy supports after-merge, keep, and manual.
- Periodic cleanup removes merged worktrees and detects orphaned worktrees with no corresponding task.
- Chained tasks carry git lineage through `base_branch`, `base_commit_sha`, and `lineage_depth`.

Follow-up lineage direction:
- Task-thread follow-ups to terminal merged/stale tasks are guarded against blindly merging the current target into an old historical task branch/worktree.
- Historical original task branches are read-only lineage when their work has already been merged, conflict-aborted, or made stale by squash/duplicate acceptance.
- Follow-up execution continues from the current merge target on fresh `task/<id>-followup-*` lineage when the old branch is stale.
- Active follow-up worktrees remain the task's current lineage; dirty/local follow-up work is reused.

Merge and metadata direction:
- Manual merge conflicts from `/tasks/:id/worktree/merge` are handled results, not ordinary request failures.
- Local merges do not use a blanket dirty-target guard; dirty-but-non-overlapping target checkout changes are allowed.
- Git overwrite/refusal cases without unmerged files surface as merge failures rather than conflict-resolution states.
- Changes-tab and local merge handlers revalidate stale `merge_status` and recover conventional worktree metadata before hiding or rejecting merge actions.
- A task branch is already merged only when the task branch is fully reachable from the target.
- Direct task-detail renders with `?tab=changes` hit the same Changes-tab recovery path as lazy tab loads.
- Current non-blocking consistency gap: direct `/tasks/:id/changes/file` lazy-file requests do not recover stale worktree metadata before resolving diff output; normal UI flow runs `/tasks/:id/changes` first.

Cleanup and descendant direction:
- Cleanup/recovery preserves conventional task worktree metadata when an original `.worktrees/task_<id>` worktree/branch still exists and contains task-side commits beyond the target.
- Orphan cleanup treats `.worktrees/task_<id>` paths as in-use when that task ID still exists, even if `worktree_path` metadata is temporarily empty.
- Locked worktrees are skipped rather than removed manually.
- Cleanup does not delete branches with non-terminal descendants.

Operational guidance for implementing or auditing worktree merge, Changes tab recovery, cleanup, and lineage behavior lives in the project skill `.openvibely/skills/openvibely_worktree_merge_lineage_workflow/SKILL.md`. Manual rebase-only work remains covered by `.openvibely/skills/openvibely_git_worktree_rebase_workflow/SKILL.md`.
