---
name: worktree_and_lineage
type: project
created: 2026-05-09
updated: 2026-07-02
source: task
source_id: fedec62b48677b4d665f6671babc20ec
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
- Startup sync uses the task's `MergeTargetBranch` when set, falling back to branch detection only when no target is stored. Without a target it prefers a local `main` branch, then `GetDefaultBranch`; `GetDefaultBranch` uses `origin/HEAD` for branch-name detection, then local `main`, then local `master`, then hardcoded `main`; `upstream/HEAD` is not consulted.
- Startup sync treats the selected local branch as the source of truth by default; merely having `origin/<branch>` must not cause a fetch, merge, or rebase from that remote-tracking branch. Remote startup sync should only exist behind an explicit user/admin opt-in policy.
- In repos with local `master` and no local `main`, startup sync should use local `master` when default-branch detection resolves to it. Current caveat: if `origin/HEAD` points to `main` while only local `master` exists, branch-name detection may choose absent `main` and worktree creation/startup merge can fail.
- Changes tab shows worktree branch diff vs target branch when available, falling back to execution diff.
- Active worktree Changes diffs should represent one net diff from the task's target branch to the current worktree state, including committed, staged, and unstaged tracked changes as a single per-path diff. Untracked files are appended separately when Git cannot include them in that comparison; do not concatenate committed branch diff blocks with `git diff HEAD`.
- `GetWorktreeDiffWithUncommitted` prefers a live diff against the worktree's actual working tree (`captureWorktreeDiffAgainstTarget`, gated on `isGitWorktreeDir`/`gitRefExists`) and only falls back to the committed-branch `GetWorktreeDiff` when no live worktree is available. This fixed a recurring `[worktree] error getting worktree diff: exit status 128` log spam from the 2-second follow-up diff snapshot loop (`chat_processing.go` `startFollowupDiffSnapshotBroadcast`) that was caused by a stale/mismatched DB `worktree_branch` no longer matching the actual worktree branch. `GetWorktreeDiff`/`captureWorktreeDiffAgainstTarget` still log unexpected `git diff` failures with repo/ref/stderr context; they just no longer fire repeatedly for missing/mismatched refs on the polling path.
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
- The app-reported error `Local merge failed: task worktree must be on the expected task branch before fast-forward merge` means the task's current expected branch (often a `task/<id>-followup-*` branch created for later continuation turns) differs from the branch actually checked out in the task worktree, not that the branch is behind target. Diagnose with `git worktree list --porcelain`, `git status --short --branch`, and `git for-each-ref` for `task/<id_prefix>*`/`followup` refs to find the branch holding the real diff; fix by switching the task worktree to that branch (`git switch <branch>`), rebasing it onto current `main` if needed, then retrying the merge.
- This project's worktree paths can be reported under two different roots for the same task ID (e.g. a top-level `.worktrees/task_<id>` vs a nested `openvibely/openvibely/.worktrees/task_<id>_followup_<epoch>` checkout). A workspace-modifying turn operating in the wrong root does not affect the actually assigned worktree, so branch/ancestry evidence must always be gathered from the exact assigned worktree path (matching hook `work_dir`), not assumed from a prior turn's report. Always confirm current `pwd`/worktree path against the exact assigned worktree before trusting git branch/ancestry evidence.
- Recurring platform behavior (confirmed across multiple multi-turn goal-driven tasks, 2026-07-02): each new task-thread/web follow-up turn can be assigned a brand-new, empty follow-up worktree+branch (`main...HEAD = 0 0`) rather than reusing/rebasing the previously-stabilized implementation-bearing branch, even after that branch was already fast-forward-ready. The real implementation commits remain parked on an earlier `task/<id>-followup-<epoch>` branch in a different worktree. This is a persistent, expected platform/runtime characteristic, not evidence of prior LLM work being lost. Treat "no merge option" / "where are my changes" follow-ups as routine on multi-turn tasks. Fix pattern: from the currently assigned worktree, run `git worktree list --porcelain` plus `git branch --list 'task/<id_prefix>*' -vv` to enumerate all task-lineage branches/worktrees, identify which branch actually holds the non-empty diff vs `main`, then `git switch <that-branch>` in the current worktree (rebasing onto `main` first if it has drifted) so the Changes/Merge view has something to show. Each such switch+rebase is itself a workspace-modifying turn and should get its own fresh audit-only review afterward.
- Escalated variant of the same pattern: even when the currently assigned worktree is already checked out on a clean fast-forward-ready implementation branch, the UI can still show no merge option. Working hypothesis (unconfirmed against app storage): the Changes tab's "expected branch" is task metadata that can independently point at a different (often newer, empty) branch name than what the worktree has checked out, so a durable fix may require moving/cherry-picking commits onto the specific branch name the task metadata expects, not just switching the worktree checkout.
- Distinct failure mode: a pre-execution worktree status check can fail outright with `could not check worktree status in <path>: exit status 128` before any commands can run, blocking the task entirely. This is different from the branch-drift pattern above (worktree exists but silently switches branches) — here the worktree/git ref appears broken or inaccessible from the start. This has been observed to be transient/stale rather than a genuinely broken/orphaned worktree: on a later turn, `git worktree list --porcelain` and `git status --short --branch` against the exact assigned path can succeed with no repair needed. Do not assume platform-side worktree recreation is required after one or two exit-128 failures; re-check directly on a later turn before concluding the worktree is broken.
- More severe variant observed 2026-07-02: the assigned `.worktrees/task_<id>` directory can vanish entirely mid-task (not just become exit-128-inaccessible), causing shell commands to fail at `chdir` because the cwd no longer exists, and the worktree drops out of `git worktree list` in the main repo. Recovery: create the missing directory only enough to let a shell start, then use the main repository's git metadata (`git worktree add`/`git worktree list --porcelain`) to re-register and recreate the actual task worktree/branch from the main repo (e.g. from `main` at the assigned path) before resuming code changes, rather than treating the loss as unrecoverable or working from the main checkout.

Cleanup and descendant direction:
- Cleanup/recovery preserves conventional task worktree metadata when an original `.worktrees/task_<id>` worktree/branch still exists and contains task-side commits beyond the target.
- Orphan cleanup treats `.worktrees/task_<id>` and `.worktrees/task_<id>_followup_<timestamp>` paths as in-use when that task ID still exists, even if `worktree_path` metadata is temporarily empty.
- Follow-up worktree branches use `task/<id_prefix>-followup-*`; cleanup must preserve active follow-up lineage and not make follow-up commits unreachable.
- Locked, dirty, unmerged, or task/follow-up-lineage-referenced worktrees are skipped rather than removed manually.
- Cleanup does not delete branches with non-terminal descendants or branches that are not conclusively merged into the target.

