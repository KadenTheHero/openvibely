---
kind: openvibely.agent_skill
version: 1
skill:
    key: openvibely_worktree_merge_lineage_workflow
    name: OpenVibely Worktree Merge And Lineage Workflow
    scope: project
    description: Implement and audit OpenVibely task worktrees, merge actions, Changes tab recovery, cleanup, and chained-task lineage.
---

# OpenVibely Worktree Merge And Lineage Workflow

Use this skill when changing task worktree creation, startup sync, local merge/squash/fast-forward behavior, Changes tab merge controls, stale worktree metadata recovery, cleanup, or chained-task lineage.

## How To Invoke

Example prompts:

- `Fix stale merge_status hiding Task Changes merge actions`
- `Audit worktree cleanup and lineage behavior`
- `Implement dirty-target-safe local merge handling`
- `Debug a task follow-up that appears to undo merged work`
- `Fix Changes tab direct ?tab=changes recovery`

For manual rebase-only work, also use `openvibely_git_worktree_rebase_workflow`.

## MANDATORY: Worktree Path Discipline

**This section must be followed before touching any file when a worktree path is given in the system prompt.**

Tool calls (`read_file`, `edit_file`, `write_file`, `bash`) resolve **relative paths against the agent's working directory, which is always the main repo root — never the worktree.** This means silently editing the wrong tree is possible and has happened. Prevent it:

1. **First action on every worktree task:** run `pwd` and `git branch --show-current` inside the worktree to confirm location before reading or writing anything:
   ```bash
   cd /path/to/worktree && pwd && git branch --show-current && git status --short
   ```
2. **All `bash` commands must be prefixed** with `cd <absolute-worktree-path> &&` for the duration of the task, or the shell must be explicitly changed to the worktree directory first and confirmed.
3. **All `read_file`, `edit_file`, `write_file` calls must use absolute paths** rooted at the worktree, not relative paths from the repo root.
4. **Before committing**, run `git -C <worktree-path> status` and `git -C <worktree-path> diff --stat HEAD` to verify changes are in the worktree branch, not main.
5. If accidental edits land in the main checkout, stop immediately: inspect both trees, move changes to the worktree, restore main before continuing.

## Core Model

- Task execution uses isolated git worktrees under `.worktrees/task_<id>` with task-scoped branches such as `task/<id_prefix>-<slug>`.
- The worktree absolute path is provided in the system prompt; treat it as the working root for all file operations.
- LLM task prompts should include explicit worktree path orientation when a workdir is present, but runtime workdir enforcement remains authoritative.
- `LLMService.ExecuteTaskWithAgent` creates the worktree before execution, runs startup sync when the worktree is clean, and handles post-execution merge.
- Startup sync should use the task's `MergeTargetBranch` when set, falling back to the default branch only when no target is stored.

## Follow-Up Lineage

- Task-thread follow-ups to terminal merged/stale tasks should not blindly merge the current target into an old historical task branch/worktree.
- Historical original task branches are read-only lineage when already merged, conflict-aborted, or stale after squash/duplicate acceptance. Follow-up execution should continue from the current merge target on fresh `task/<id>-followup-*` lineage and skip startup sync for that first current-target setup.
- Preserve active follow-up worktrees as current lineage. Dirty/local follow-up work must be reused. Clean read-only follow-up branches may be reused when they have no commits beyond the target.
- If startup auto-merge is aborted in a clean preserved worktree that still has real unmerged task commits, keep conflict metadata and allow the follow-up run to start there with explicit context that the worktree may be behind/diverged.

## Merge Handling

- Manual merge conflicts from `/tasks/:id/worktree/merge` are handled results, not ordinary request failures. The merge service should set conflict status, return conflict file details, refresh the panel, and emit an HTMX toast.
- Local merges should not use a blanket dirty-target guard. Allow dirty-but-non-overlapping target checkout changes; block only for active conflict/merge state or when Git reports local changes would be overwritten/conflict.
- Git overwrite/refusal cases without unmerged files should be surfaced as merge failures, not conflict-resolution states. Leave user files untouched, set failed status, and return Git's message.
- Merge/squash cleanup needs pre-merge status snapshots and ownership-aware handling so OpenVibely does not reset, unstage, delete, or commit pre-existing user work.
- Sequential local fast-forward merges for existing task worktrees should stay in the task worktree for validation/rebase, verify the expected branch, require a clean task worktree, rebase onto the current target, and use the post-rebase task worktree `HEAD`.
- After a local fast-forward rebase, update the target branch conditionally. If the target branch is checked out in a worktree, merge `refs/heads/<task-branch>` inside that target worktree. If not checked out, use protected `git update-ref refs/heads/<target> <rebased-task-HEAD> <old-target>`.
- Avoid `reset --hard` as default cleanup because it can destroy real user work.
- Squash merge failure handling must clean or abort squash state and leave the target checkout clean without hard-resetting the main/default target branch.

## Changes Tab And Metadata Recovery

- Changes tab should show worktree branch diff vs target branch when available, falling back to execution diff.
- Do not hide or reject merge actions solely because `tasks.merge_status=merged` or worktree metadata is blank. First revalidate against Git and recover conventional `.worktrees/task_<id>` / expected `task/<id_prefix>-<slug>` metadata when present.
- Clear stale merged metadata whenever Git shows the task branch still has commits beyond the target, including diverged branches where the target also has newer commits.
- Only treat a task as already merged when the task branch is fully reachable from the target.
- Apply stale metadata recovery consistently across `/tasks/:id/changes`, merge POST, older `/tasks/:id/changes/worktree`, worktree info panel, and direct task-detail renders with `?tab=changes`.
- Direct `?tab=changes` render should lazy-load `/tasks/:id/changes` on page load instead of inlining stale `TaskChangesContent(...)`.
- Direct `/tasks/:id/changes/file` lazy-file requests currently need care because normal UI flow runs `/tasks/:id/changes` first and persists recovery before lazy file loads.
- Worktree merge HTMX flows should close the dropdown, disable/show busy state for the clicked action, and refresh the initiating Changes surface or show a toast.

## Diagnostics

- When a follow-up appears to undo already-merged work, verify the real target/default branch and task branch ancestry instead of trusting a linked worktree's local `main` ref.
- When a user worries a commit merged into `main` is missing from a task worktree, use explicit reachability/range checks:

```bash
git merge-base --is-ancestor main HEAD
git merge-base --is-ancestor HEAD main
git log --oneline HEAD..main
git log --oneline main..HEAD
```

- If accidental edits land in the main checkout while a task has an assigned worktree, inspect both statuses/diffs, move or recreate the relevant changes in the task worktree, then restore main only after confirming nothing will be lost.
- For conflict triage, check actual merge direction. `main -> task` may report already up to date while `task -> main` or cherry-picking a follow-up commit can still conflict.
- Because `internal/database/migrations/068_baseline.sql` seeds `worktree_merge_target` as `main`, tests that create temporary Git repos for worktree setup/cleanup paths should ensure those repos have a `main` branch.

## Cleanup And Chained Tasks

- Cleanup policy supports after-merge, keep, and manual. Periodic cleanup removes merged worktrees and detects orphaned worktrees with no corresponding task.
- Cleanup/recovery must not blank conventional task worktree metadata when an original `.worktrees/task_<id>` worktree/branch still exists and contains task-side commits beyond the target.
- Treat `.worktrees/task_<id>` paths as in-use when that task ID still exists, even if `worktree_path` metadata is temporarily empty.
- Skip locked worktrees rather than removing them manually.
- Chained tasks carry lineage through `base_branch`, `base_commit_sha`, and `lineage_depth`. Child tasks should inherit parent changes from the parent worktree branch HEAD or merge target/default branch HEAD as appropriate.
- Cleanup should not delete branches with non-terminal descendants.

## Testing

- Add regressions for stale `merge_status=merged` with blank/recovered metadata, target moved, task branch still has commits, and Changes tab showing local actions after status resets.
- Cover direct `?tab=changes`, lazy `/tasks/:id/changes`, merge POST, worktree panel, and legacy fragments when stale metadata recovery changes.
- Cover dirty-but-non-overlapping target changes, Git overwrite refusals, true conflicts, squash failure cleanup, checked-out target fast-forward merge, and ref-only target updates.
- Cover follow-up lineage for terminal merged/stale tasks, dirty follow-up reuse, clean follow-up staleness, startup sync conflict fallback, cleanup preserving conventional worktrees, and chained-task descendants.
