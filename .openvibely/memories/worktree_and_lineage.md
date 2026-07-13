---
name: worktree_and_lineage
type: project
created: 2026-05-09
updated: 2026-07-13
source: after_complete_memory_update
source_id: 5b9e5aa8290c9b3717453f19bc573984
confidence: high
title: Worktree and Lineage
---

Task execution uses isolated git worktrees in `.worktrees/task_<id>` with task-scoped branches `task/<id_prefix>-<slug>`. LLM task prompts include explicit worktree path orientation when a workdir is present, while runtime workdir enforcement remains the source of truth. Coding changes for assigned tasks must be made in the assigned task worktree, not the main checkout, unless the user explicitly asks for main-checkout changes.

Worktree path discipline is mandatory when a task provides a worktree path: relative tool paths resolve against the agent's working directory, not automatically against the task worktree.

Durable worktree model:
- Auto-merge supports merge commit, fast-forward only, and squash merge.
- Task Changes local actions include merge commit, fast-forward only, squash merge, and rebase onto the task's target/default branch when the task branch is behind and no active merge/conflict state is present.
- Changes-tab rebase runs against the task worktree, refreshes the Changes partial on success/already-up-to-date/conflict result, and treats already-up-to-date as an informational success.
- `LLMService.ExecuteTaskWithAgent` creates the worktree before execution, runs startup sync from the latest target/default branch when the worktree is clean, and handles post-execution merge.
- Startup sync is a real `git merge --no-edit <target>` inside the task worktree before execution, including task-thread follow-ups. On content conflict it detects conflicted paths, aborts the merge, and persists `MergeStatusConflict`. Initial task execution still fails before model dispatch, but follow-up execution now continues in the preserved clean worktree with model-visible recovery context; clearing the database flag alone cannot prevent the same conflict from recurring.
- Startup sync uses the task's `MergeTargetBranch` when set, falling back to branch detection only when no target is stored. Without a target it prefers a local `main` branch, then `GetDefaultBranch`; `GetDefaultBranch` uses `origin/HEAD` for branch-name detection, then local `main`, then local `master`, then hardcoded `main`; `upstream/HEAD` is not consulted.
- Startup sync treats the selected local branch as the source of truth by default; merely having `origin/<branch>` must not cause a fetch, merge, or rebase from that remote-tracking branch. Remote startup sync should only exist behind an explicit user/admin opt-in policy.
- In repos with local `master` and no local `main`, startup sync should use local `master` when default-branch detection resolves to it. Current caveat: if `origin/HEAD` points to `main` while only local `master` exists, branch-name detection may choose absent `main` and worktree creation/startup merge can fail.
- Worktree setup fails closed: a repository with a local commit needs no Git remote, but an unborn repository has no commit/tree for `git worktree add`, so task execution and follow-ups must provide initial-commit guidance and never dispatch the coding model in the main checkout. Existing task worktrees or branches remain recoverable if their original base ref was renamed/deleted; operational `rev-parse` failures preserve their real error. Channel-origin and review-submission setup failures promote the next queued follow-up instead of parking it.
- Changes tab shows worktree branch diff vs target branch when available, falling back to execution diff.
- Active worktree Changes diffs should represent one net diff from the task's target branch to the current worktree state, including committed, staged, and unstaged tracked changes as a single per-path diff. Untracked files are appended separately when Git cannot include them in that comparison; do not concatenate committed branch diff blocks with `git diff HEAD`.
- `GetWorktreeDiffWithUncommitted` prefers a live diff against the worktree's actual working tree (`captureWorktreeDiffAgainstTarget`, gated on `isGitWorktreeDir`/`gitRefExists`) and only falls back to the committed-branch `GetWorktreeDiff` when no live worktree is available. This fixed a recurring `[worktree] error getting worktree diff: exit status 128` log spam from the 2-second follow-up diff snapshot loop (`chat_processing.go` `startFollowupDiffSnapshotBroadcast`) that was caused by a stale/mismatched DB `worktree_branch` no longer matching the actual worktree branch. `GetWorktreeDiff`/`captureWorktreeDiffAgainstTarget` still log unexpected `git diff` failures with repo/ref/stderr context; they just no longer fire repeatedly for missing/mismatched refs on the polling path.
- Cleanup policy supports after-merge, keep, and manual.
- Periodic cleanup removes merged worktrees and detects orphaned worktrees with no corresponding task.
- Chained tasks carry git lineage through `base_branch`, `base_commit_sha`, and `lineage_depth`.

Commit-message direction:
- Task execution auto-commits use generated descriptive commit messages driven by the actual worktree diff. Generation happens while changes are still in the worktree for initial execution diff capture, later task-thread completion, post-execution safety capture, and merge-prep dirty-worktree commits. GitHub PR branch publication now uses API-backed synthesized branch commits rather than local `git add`/`git commit`/`git push`, but the synthesized commit message is still generated from the task worktree diff.
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
- Startup-conflict recovery for task follow-ups is implemented: safely aborted startup merge content conflicts are represented by typed `StartupSyncConflictError` values carrying the target branch, task branch, worktree path, and conflicted files. The follow-up handler no longer depends on terminal task status, so reactivated `running` tasks continue in the preserved clean worktree and the coding agent receives instructions to merge, resolve, build, test, and commit before handling the follow-up. Failed merge aborts, dirty worktrees, missing branches, setup failures, and non-conflict Git errors remain fatal. Regression coverage reproduces the post-reactivation status ordering and verifies provider-visible conflict context.

Merge and metadata direction:
- Manual merge conflicts from `/tasks/:id/worktree/merge` are handled results, not ordinary request failures.
- Changes-tab rebase conflicts are handled by aborting the rebase and surfacing guidance; because no rebase remains in progress after abort, the task should not be left in `MergeStatusConflict` solely from that aborted rebase.
- Current known fast-forward defect: `fastForwardTaskWorktreeToTarget` unconditionally runs `git rebase <target>` before advancing the target, even when the target is already an ancestor of the task branch. For a task branch containing a merge commit, this can replay old commits, produce a false conflict, abort back to an otherwise mergeable branch, and persist `MergeStatusConflict`. The fast-forward path should short-circuit the rebase when ancestry already permits `--ff-only`.
- Local merges do not use a blanket dirty-target guard; dirty-but-non-overlapping target checkout changes are allowed.
- Git overwrite/refusal cases without unmerged files surface as merge failures rather than conflict-resolution states.
- Changes-tab and local merge handlers revalidate stale `merge_status` and recover conventional worktree metadata before hiding or rejecting merge actions.
- A conflict-resolution commit made directly in a task worktree does not itself clear the task's persisted `MergeStatusConflict`; while that status remains, the Changes UI hides local merge actions. The task must be resumed/rerun in its owning project or otherwise pass through explicit merge-status reconciliation before the option reappears. Task controls are project-scoped, so an agent running under another project cannot perform that rerun.
- Local worktree commits are not automatically remote publication: verify the configured remote and compare its task-branch tip before claiming a fix is available outside the local app/worktree.
- A task branch is already merged only when the task branch is fully reachable from the target.
- Direct task-detail renders with `?tab=changes` hit the same Changes-tab recovery path as lazy tab loads.
- Current non-blocking consistency gap: direct `/tasks/:id/changes/file` lazy-file requests do not recover stale worktree metadata before resolving diff output; normal UI flow runs `/tasks/:id/changes` first.
- A local merge error saying the worktree is not on the expected task branch indicates branch/metadata drift, not that the branch is merely behind target. Always diagnose from the exact assigned worktree path using worktree/status/ref evidence and identify which task-lineage branch actually owns the diff.
- Multi-turn follow-ups can surface a newer empty follow-up worktree while implementation commits remain on an earlier `task/<id>-followup-*` branch. Treat missing merge options or apparently lost changes as a lineage-discovery incident: enumerate task branches/worktrees, recover the implementation-bearing branch into the assigned worktree, rebase if needed, and verify task metadata expects that branch.
- Worktree status `exit status 128` can be transient; re-check the exact assigned path before recreating anything. If the assigned directory truly vanished and disappeared from `git worktree list`, recover it through the main repository's git worktree metadata rather than editing the main checkout.

Cleanup and descendant direction:
- Cleanup/recovery preserves conventional task worktree metadata when an original `.worktrees/task_<id>` worktree/branch still exists and contains task-side commits beyond the target.
- Orphan cleanup treats `.worktrees/task_<id>` and `.worktrees/task_<id>_followup_<timestamp>` paths as in-use when that task ID still exists, even if `worktree_path` metadata is temporarily empty.
- Follow-up worktree branches use `task/<id_prefix>-followup-*`; cleanup must preserve active follow-up lineage and not make follow-up commits unreachable.
- Locked, dirty, unmerged, or task/follow-up-lineage-referenced worktrees are skipped rather than removed manually.
- Cleanup does not delete branches with non-terminal descendants or branches that are not conclusively merged into the target.

