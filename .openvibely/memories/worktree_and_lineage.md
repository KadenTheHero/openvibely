---
name: worktree_and_lineage
type: project
created: 2026-05-09
updated: 2026-06-21
source: consolidation
source_id: memory_consolidation_2026_06_21
confidence: high
title: Worktree and Lineage
---

Task execution uses isolated git worktrees in `.worktrees/task_<id>` with task-scoped branches `task/<id_prefix>-<slug>`. LLM task prompts include explicit worktree path orientation when a workdir is present, while runtime workdir enforcement remains the source of truth. Coding changes for assigned tasks must be made in the assigned task worktree, not the main checkout, unless the user explicitly asks for main-checkout changes.

Worktree path discipline is mandatory when a task provides a worktree path: relative tool paths resolve against the agent's working directory, not automatically against the task worktree, so coding agents must explicitly operate in and verify the assigned worktree branch. Procedure-level details belong in the `openvibely_worktree_merge_lineage_workflow` skill.

Durable worktree model:
- Auto-merge supports merge commit, fast-forward only, and squash merge.
- Task Changes local actions include merge commit, fast-forward only, squash merge, and rebase onto the task's target/default branch when the task branch is behind and no active merge/conflict state is present.
- Changes-tab rebase runs against the task worktree, refreshes the Changes partial on success/already-up-to-date/conflict result, and treats already-up-to-date as an informational success.
- `LLMService.ExecuteTaskWithAgent` creates the worktree before execution, runs startup sync from the latest target/default branch when the worktree is clean, and handles post-execution merge.
- Startup sync uses the task's `MergeTargetBranch` when set, falling back to the default branch only when no target is stored.
- Changes tab shows worktree branch diff vs target branch when available, falling back to execution diff.
- Active worktree Changes diffs should represent one net diff from the task's target branch to the current worktree state, including committed, staged, and unstaged tracked changes as a single per-path diff. Untracked files are appended separately when Git cannot include them in that comparison; do not concatenate committed branch diff blocks with `git diff HEAD`, because follow-ups that re-edit an already-committed file can otherwise duplicate the same path in Task Changes while running.
- Cleanup policy supports after-merge, keep, and manual.
- Periodic cleanup removes merged worktrees and detects orphaned worktrees with no corresponding task.
- Chained tasks carry git lineage through `base_branch`, `base_commit_sha`, and `lineage_depth`.

Commit-message direction:
- Task execution auto-commits use generated descriptive commit messages driven by the actual worktree diff. Generation happens while changes are still in the worktree for initial execution diff capture, later task-thread completion, post-execution safety capture, merge-prep dirty-worktree commits, and manual GitHub PR-prep dirty-worktree commits.
- Commit-message generation first collects compact diff facts/hunks from actual changes (`git status --porcelain --untracked-files=all`, unstaged/staged diffs, and snippets for untracked text files) and sends that to an LLM prompt requesting one plain subject.
- Task title, prompt, and execution output are supporting context only and must be ignored when they conflict with the diff. Stored execution text must not become the subject by itself.
- If no usable LLM summary is available, fall back deterministically from diff/path/status facts with plain subject-only summaries such as `Add <label>`, `Update <label>`, `Remove <label>`, `Update <area> files`, `Update <n> files`, `Update changes`, `Refine changes`, or `Prepare changes for merge`.
- Commit-summary diff context must not follow untracked symlinks or read snippet content outside the worktree. Inspect untracked paths with `Lstat`-style behavior, skip symlinks before reading content, and verify resolved paths remain inside the resolved worktree.
- Task-execution commit subjects should be concise, subject-only, plain language, and follow Tim Pope-style git subject guidance: capitalized imperative mood such as `Fix bug`, not lowercase `fix bug` or conventional-prefix `fix: bug`. Strip provider/status/tool boilerplate, conventional commit prefixes, and common body/file-list boilerplate headings from LLM-provided candidates before accepting a subject.
- Do not add or accept a `Changed files:`/file-list body; do not invent `task` scopes or mention task/worktree machinery unless it is the actual code scope; do not use generic `Task completed:`/`Followup:` subjects or lifecycle labels.
- Existing historical commits keep their original subjects. Changes-tab integration commits remain static (`Merge task:`, `Squash merge task:`), and fast-forward creates no merge commit.

Follow-up lineage direction:
- Task-thread follow-ups to terminal merged/stale tasks are guarded against blindly merging the current target into an old historical task branch/worktree.
- Historical original task branches are read-only lineage when their work has already been merged, conflict-aborted, or made stale by squash/duplicate acceptance.
- Follow-up execution continues from the current merge target on fresh `task/<id>-followup-*` lineage when the old branch is stale.
- Active follow-up worktrees remain the task's current lineage; dirty/local follow-up work is reused.

Merge and metadata direction:
- Manual merge conflicts from `/tasks/:id/worktree/merge` are handled results, not ordinary request failures.
- Changes-tab rebase conflicts are handled by aborting the rebase and surfacing guidance; because no rebase remains in progress after abort, the task should not be left in `MergeStatusConflict` solely from that aborted rebase.
- Local merges do not use a blanket dirty-target guard; dirty-but-non-overlapping target checkout changes are allowed.
- Git overwrite/refusal cases without unmerged files surface as merge failures rather than conflict-resolution states.
- Changes-tab and local merge handlers revalidate stale `merge_status` and recover conventional worktree metadata before hiding or rejecting merge actions.
- A task branch is already merged only when the task branch is fully reachable from the target.
- Direct task-detail renders with `?tab=changes` hit the same Changes-tab recovery path as lazy tab loads.
- Current non-blocking consistency gap: direct `/tasks/:id/changes/file` lazy-file requests do not recover stale worktree metadata before resolving diff output; normal UI flow runs `/tasks/:id/changes` first.

Cleanup and descendant direction:
- Cleanup/recovery preserves conventional task worktree metadata when an original `.worktrees/task_<id>` worktree/branch still exists and contains task-side commits beyond the target.
- Orphan cleanup treats `.worktrees/task_<id>` and `.worktrees/task_<id>_followup_<timestamp>` paths as in-use when that task ID still exists, even if `worktree_path` metadata is temporarily empty.
- Follow-up worktree branches use `task/<id_prefix>-followup-*`; cleanup must preserve active follow-up lineage and not make follow-up commits unreachable.
- Locked, dirty, unmerged, or task/follow-up-lineage-referenced worktrees are skipped rather than removed manually.
- Cleanup does not delete branches with non-terminal descendants or branches that are not conclusively merged into the target.

Operational guidance belongs in `openvibely_worktree_merge_lineage_workflow`; manual rebase-only work remains covered by `openvibely_git_worktree_rebase_workflow`.
