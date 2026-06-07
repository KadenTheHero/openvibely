---
kind: openvibely.agent_skill
version: 1
skill:
    key: openvibely_release_workflow
    name: OpenVibely Release Workflow
    scope: project
    description: Automate the OpenVibely release process — preflight, artifact builds, AI-synthesized release notes, docs updates, and GitHub release publishing — for a given semver version.
---

# OpenVibely Release Workflow

Use this skill when creating a new OpenVibely release for a given semver version, or when creating, reviewing, or modifying OpenVibely release automation and GitHub release notes. It orchestrates the end-to-end release pipeline using deterministic scripts checked into the repo under this skill's `scripts/` directory.

---

## How to Invoke

The minimum prompt to trigger this skill:

> Release version 0.1.1

The version can include or omit the `v` prefix — both are accepted:

> Release version v0.1.1

Modifier examples the agent will understand:

| Prompt | Behavior |
|--------|----------|
| `Release version 0.1.1` | Full pipeline — preflight, build, notes, docs, publish |
| `Release version 0.1.1 — dry run only` | Runs all steps with `DRY_RUN=1`; no git push, no GitHub release |
| `Release version 0.1.1 — create as draft` | GitHub release is created as a draft for review before publishing |
| `Release version 0.1.1 — skip Docker` | Suppresses the Docker reminder (still a manual step regardless) |
| `Release version 0.1.1 — build step only` | Runs preflight + build; stops before notes, docs, or publish |
| `Release version 0.1.1 — preflight already passed, start from build` | Skips preflight, runs build → notes → docs → publish |

The agent should always run `DRY_RUN=1` first, review the output, then run the full pipeline.

---

## Required Inputs

| Input | Example | Notes |
|-------|---------|-------|
| Version | `0.1.1` or `v0.1.1` | Normalized to `X.Y.Z`; `v` prefix accepted |

---

## Required Environment / Tools

| Tool | Purpose | Install |
|------|---------|---------|
| `go` | Build server/desktop binaries | https://go.dev/dl |
| `git` | Tag, branch, commit range | pre-installed |
| `gh` (GitHub CLI) | Create GitHub release, upload assets | `brew install gh` or https://cli.github.com |
| `zip` | Package Windows and macOS archives | pre-installed macOS/Linux |
| `tar` | Package server tarballs | pre-installed |
| `sha256sum` / `shasum` | Generate SHA256SUMS | pre-installed (Linux/macOS) |
| `docker` *(optional)* | Build and push Docker image | https://docker.com |
| `x86_64-w64-mingw32-gcc` *(optional)* | Cross-compile Windows desktop-cli | `brew install mingw-w64` |

**macOS-only:** macOS desktop `.app` bundles can only be built on a macOS host. Server artifacts for all platforms can be cross-compiled from any OS.

### Missing tools — install before proceeding

If preflight or a dry run reports a required release tool as missing, install it during the task before continuing. Do not stop at "tool missing" unless installation fails, requires unavailable privileges, or requires user-owned credentials/auth that cannot be completed non-interactively.

Common macOS installs:

```bash
brew install gh
brew install mingw-w64
```

After installing `gh`, run `gh auth status`; if unauthenticated, run `gh auth login` or ask the user to complete GitHub authentication. For optional tools, install them when the requested release artifact or Docker step depends on them; otherwise document the skipped capability in the release summary.

---

## Preflight Checks

Run `release-preflight.sh <version>` first, or let `release.sh` run it automatically. Preflight validates:

1. **Semver format** — `X.Y.Z` or `vX.Y.Z` only; no pre-release suffixes.
2. **Required tools** — `go`, `git`, `zip`, `tar`, `sha256sum`/`shasum` present.
3. **GitHub CLI auth** — `gh auth status` passes; repo push permission confirmed.
4. **Worktree clean** — warns if uncommitted changes exist (does not block, but should be resolved before publishing).
5. **Tag collision** — fails if `v<version>` already exists locally or in GitHub releases.
6. **Previous tag** — finds latest `v*` tag for changelog range.
7. **Commit count** — lists unreleased commits for review.
8. **Build capability** — reports whether macOS desktop, Windows desktop-cli, and Docker are available on the current host.

---

## Release Flow

### Option A: Automated (recommended)

```bash
cd /path/to/openvibely
DRY_RUN=1 .openvibely/skills/openvibely_release_workflow/scripts/release.sh 0.1.1
# Review dry-run output, then:
.openvibely/skills/openvibely_release_workflow/scripts/release.sh 0.1.1
```

The orchestrator runs steps in sequence, with a mandatory AI synthesis + docs + review pause before publishing.

```
Step 1: release-preflight.sh  — validate environment, auth, tags, build caps
Step 2: release-build.sh      — build all artifacts → dist/0.1.1/
Step 3: release-notes.sh      — collect COMMITS.txt, render RELEASE_NOTES.md shell
Step 4: (agent)               — read COMMITS.txt, synthesize release-specific highlights and changelog, fill placeholders
Step 5: (agent)               — update docs for features added or meaningfully changed in this release
Step 6: (review)              — confirm RELEASE_NOTES.md and docs updates look correct
Step 7: release-publish.sh    — create GitHub release, upload artifacts
```

Step 4 is not automated — the orchestrating agent reads `COMMITS.txt` and writes both the `Highlights` and `What's Changed` sections using AI judgment (see Release Notes Generation below). Step 5 is also agent-owned — update the user docs before publishing so the release tag and public documentation include the features being announced.

### Option B: Step-by-step (for debugging or partial runs)

```bash
SCRIPTS=".openvibely/skills/openvibely_release_workflow/scripts"

# 1. Preflight
bash $SCRIPTS/release-preflight.sh 0.1.1

# 2. Build artifacts
bash $SCRIPTS/release-build.sh 0.1.1 ./dist/0.1.1

# 3. Collect commits + render notes template
bash $SCRIPTS/release-notes.sh 0.1.1 v0.1.0 ./dist/0.1.1

# 4. (Agent step) Read commits, synthesize release-specific highlights and changelog, fill placeholders
#    Read:  ./dist/0.1.1/COMMITS.txt
#    Edit:  ./dist/0.1.1/RELEASE_NOTES.md  (replace AI_HIGHLIGHTS_PLACEHOLDER and AI_CHANGELOG_PLACEHOLDER blocks)

# 5. (Agent step) Update docs for new and changed user-facing features before publishing
#    Edit relevant in-repo docs/*.md files and the docs site repo if available.

# 6. Review the final RELEASE_NOTES.md and docs changes, then publish
bash $SCRIPTS/release-publish.sh 0.1.1 ./dist/0.1.1
```

### Dry-run mode

All scripts respect `DRY_RUN=1`. In dry-run mode, build commands, git operations, and GitHub API calls are printed but not executed. Run this first on a new release to confirm the expected behavior.

```bash
DRY_RUN=1 .openvibely/skills/openvibely_release_workflow/scripts/release.sh 0.1.1
```

When a user asks what the release notes would look like after a dry run, read the generated `COMMITS.txt` and `RELEASE_NOTES.md` from the dry-run/dist directory, synthesize both the `Highlights` and `What's Changed` sections in the response, and present the full filled-in notes preview without publishing or editing release state.

**Known dry-run caveat:** if `release-build.sh` prints `cp` or `chmod` errors while building macOS desktop bundles in `DRY_RUN=1`, treat them as a dry-run script leak unless the real build was requested. The `build_macos_app()` path may print the skipped binary build but still attempt bundle setup against a missing dry-run binary; report this as a script bug to fix before relying on clean dry-run output, but do not treat it as proof that an actual release build is blocked.

---

## Artifact Naming (matches v0.1.0 release pattern)

| Artifact | Description |
|----------|-------------|
| `OpenVibely_<version>_darwin_amd64.app.zip` | macOS desktop app bundle (Intel) |
| `OpenVibely_<version>_darwin_arm64.app.zip` | macOS desktop app bundle (Apple Silicon) |
| `openvibely_<version>_darwin_amd64_server.tar.gz` | macOS server binary (Intel) |
| `openvibely_<version>_darwin_arm64_server.tar.gz` | macOS server binary (Apple Silicon) |
| `openvibely_<version>_linux_amd64_server.tar.gz` | Linux server binary (x86_64) |
| `openvibely_<version>_linux_arm64_server.tar.gz` | Linux server binary (ARM64) |
| `openvibely_<version>_windows_amd64_server.zip` | Windows server binary zip |
| `openvibely_<version>_windows_amd64_desktop-cli.zip` | Windows desktop binary zip (requires mingw-w64) |
| `SHA256SUMS` | SHA-256 checksums for all artifacts |

**Casing rule:** Desktop macOS bundles use `OpenVibely_` (capitalized). All server/binary artifacts use `openvibely_` (lowercase).

---

## Release Notes Generation

`release-notes.sh` is deterministic: it collects raw commit data and renders the static template, but **does not synthesize release-specific highlights or the changelog**. Those steps require AI judgment and are explicitly delegated to the orchestrating agent.

Never reuse static product marketing bullets as release `Highlights`. The product intro can describe what OpenVibely is; `Highlights` must describe the 2–4 biggest user-facing changes that are new in this exact release compared to the previous tag.

### What the script does

1. Runs `git log <prev_tag>..HEAD` with full commit metadata (hash, date, author, subject, body) and writes it to `COMMITS.txt` in the dist dir.
2. Renders `RELEASE_NOTES.md` with static sections populated (Downloads table, Docker, Known Limitations) plus `AI_HIGHLIGHTS_PLACEHOLDER` in `Highlights` and `AI_CHANGELOG_PLACEHOLDER` in `What's Changed`.

### What the agent must do (AI synthesis step)

After `release-notes.sh` runs, the agent must:

1. **Read `dist/<version>/COMMITS.txt`** — full commit log since the previous release.
2. **Synthesize release-specific highlights** — 2–4 bullets, only for the most notable changes that are new in this release. Do not list evergreen features that existed in the prior release. Describe each highlight by what it **does** for the user — the functional capability or workflow it enables — not by what UI elements or files were added. Avoid using feature-specific examples in this skill; they rot as the product evolves. Prefer generic guidance such as: describe the user outcome, not the implementation detail or surface-level label.
3. **Synthesize a high-level, user-facing changelog** — not a dump of commit subjects. The agent should:
   - Identify what actually changed from a user's perspective.
   - Group related commits into coherent bullet points (3–8 total, plain English).
   - Leave out noise: internal refactors, memory updates, test changes, typo fixes, chore commits, anything with no user impact.
   - Combine multiple small commits about the same feature into one bullet.
   - If commit messages are terse or technical, infer intent from context (file paths, bodies, task references).
   - Inspect related diffs or touched paths for ambiguous commits when practical instead of guessing from subjects alone.
   - Sanity-check product and model names against the actual changed code/docs before publishing; do not rely on ambiguous commit subjects for exact marketing names or version numbers.
   - **Omit minor model version additions** (e.g. "Model X added to model selector") unless the model is a primary release feature. Adding a model to a dropdown is a maintenance change, not a release highlight. Omit it entirely or fold it into a generic "model updates" entry only if there are several such additions.
   - **Omit bug fixes and reliability patches** unless they fix a critical, user-visible breakage that affected a core workflow. Individual crash fixes, scroll bugs, state resets, worker slot leaks, UI polish items (light/dark mode rendering, panel detail tweaks), and narrow edge-case corrections are too low-level for release notes. If several related reliability fixes exist, fold them into a single understated sentence at most, or omit entirely.
   - **Omit UI panel detail changes** (e.g. column widths, button placement, diff rendering toggles, color coding in a sub-panel) — these are incremental polish, not release features.
   - The bar for inclusion is: would a developer choosing whether to upgrade specifically care about this? If the answer is only "maybe, if they hit that exact bug", leave it out.
4. **Replace both placeholder blocks** in `RELEASE_NOTES.md`: `AI_HIGHLIGHTS_PLACEHOLDER` and `AI_CHANGELOG_PLACEHOLDER`.
5. Confirm the notes look correct before running `release-publish.sh`: no placeholder text, no duplicated commit-derived bullets, no old highlights from a previous release, no leaked secrets, and no claims about Docker/artifacts/features that the scripts did not actually build or verify.

### Why this approach

Commit messages in this repo are not always detailed or conventional. A regex-based grouper produces noisy, low-signal output. The agent reading the full commit context (including bodies and file diffs if needed) produces a far better high-level summary that's actually useful to users reading the GitHub release page.

---

## Release Notes Template

The generated notes follow this structure:

```
# OpenVibely <version>

<product intro paragraph>

## Highlights
<AI_HIGHLIGHTS_PLACEHOLDER: 2–4 bullets for the biggest new changes in this release>

## What's Changed
<AI_CHANGELOG_PLACEHOLDER: grouped user-facing commit summary>
Full changelog: <link>

## Downloads
<SHA256 table from SHA256SUMS>

## Docker
<docker pull / run commands>

## Known Limitations
<static limitation bullets>
```

---

## Docs Update

After synthesizing the release notes, update documentation for features that are new or meaningfully changed in this release before publishing. Use `openvibely_docs_editing_workflow` when selected or available, and keep edits conservative.

### What to update

Update both documentation surfaces when they exist and are relevant:

1. **In-repo docs** — `docs/*.md`, `README.md`, and generated API docs when the release changes user-facing behavior, setup, environment variables, integrations, scheduling, skills, agents, task flows, or screenshots.
2. **Docs site repo** — the sibling `openvibely-doc` repository, usually at `/Users/dubee/go/src/github.com/openvibely/openvibely-doc`, for public docs pages that mirror or expand the in-repo guides.

### Decision rules

- New user-facing feature: find the closest existing guide and add usage/setup details instead of creating a new page by default.
- Changed behavior: search docs for stale descriptions and update them to match the release behavior.
- New or changed environment variables/config: update `docs/environment.md` and the corresponding docs-site page if present.
- New or changed channel integration behavior: update the relevant GitHub, Slack, Telegram, or webhook guide.
- Internal-only changes, tests, refactors, and tooling changes with no user-visible impact: do not add release docs just to mention them.
- Screenshots: update only when the documented UI materially changed and local screenshot generation is practical; otherwise note the screenshot gap in the release summary.

### Validation

Before publishing, verify the docs changes are included in the release branch/tag plan and run the repo's normal docs validation where practical. At minimum, search for stale terminology introduced by the release changes and ensure release notes do not announce a feature that has no corresponding docs when docs are expected.

---

## Known Limitations (documented in release notes)

These are preserved from v0.1.0 and should appear in every release's notes unless resolved:

1. **Linux desktop** — GTK/WebKit dependencies prevent cross-compilation from macOS. Linux server artifacts are included; desktop artifacts are not. Build natively on Linux with `libgtk-4-dev`, `libwebkitgtk-6.0-dev`, etc. (see `ci.yaml`).
2. **Windows desktop-cli** — Artifact is an executable zip, not an installer. Requires `mingw-w64` cross-compiler for CGO-enabled Wails build; skipped if unavailable.
3. **Docker / VPS storage** — Mount `/data` as a persistent volume; do not rely on the container filesystem for database or repos.

---

## Docker Image (manual step)

Automated Docker image publishing is not wired to CI yet. After the GitHub release is created, publish the Docker image manually:

```bash
docker buildx build --platform linux/amd64,linux/arm64 \
    -t openvibely/openvibely:<version> \
    -t openvibely/openvibely:latest \
    --push .
```

`release-publish.sh` prints this command as a reminder after completing. If Docker Hub access is not set up, note this as a pending step in the GitHub release body.

---

## Rollback / Retry Behavior

| Failure point | Recovery action |
|---------------|----------------|
| Preflight fails | Fix the reported issue and re-run `release.sh` — no state was changed |
| Build fails mid-way | Fix the error; re-run `release-build.sh` (dist dir is recreated each run) |
| Notes generation fails | Re-run `release-notes.sh`; it overwrites `RELEASE_NOTES.md` |
| GitHub release creation fails | If tag was pushed, delete it: `git tag -d v<ver> && git push <remote> :refs/tags/v<ver>`; delete the remote release if partially created via `gh release delete v<ver>`; then retry |
| Partial asset upload | Use `gh release upload v<ver> <files>` to add missing assets to an existing release |

The scripts never delete local branches or reset commits. Git operations are additive (create branch, create tag, push). Rollback requires manual git and GitHub CLI cleanup described above.

---

## Expected Output

After a successful run:

```
dist/<version>/
├── OpenVibely_<version>_darwin_amd64.app.zip
├── OpenVibely_<version>_darwin_arm64.app.zip
├── openvibely_<version>_darwin_amd64_server.tar.gz
├── openvibely_<version>_darwin_arm64_server.tar.gz
├── openvibely_<version>_linux_amd64_server.tar.gz
├── openvibely_<version>_linux_arm64_server.tar.gz
├── openvibely_<version>_windows_amd64_server.zip
├── openvibely_<version>_windows_amd64_desktop-cli.zip  (if mingw-w64 present)
├── SHA256SUMS
└── RELEASE_NOTES.md
```

GitHub release `v<version>` is created with title `OpenVibely <version>`, the generated notes, and all artifacts attached.

---

## Validation Tests

Run `release-validate.sh` to verify script-level logic without building or publishing:

```bash
bash .openvibely/skills/openvibely_release_workflow/scripts/release-validate.sh
```

Tests cover:
- Semver normalization (strip `v` prefix)
- Semver validation (valid and invalid inputs)
- Artifact naming conventions (all eight artifact patterns)
- Tag format (`v<version>`)
- Release title format (`OpenVibely <version>`)
- Release branch naming (`release/v<version>`)
- Release notes template structure (`AI_HIGHLIGHTS_PLACEHOLDER` and `AI_CHANGELOG_PLACEHOLDER` present, all required sections)
- `COMMITS.txt` output structure (hash, date, author, subject, body fields present)
- If deterministic changelog grouping is replaced with AI synthesis, remove obsolete regex bucket tests and keep tests proving the placeholders and `COMMITS.txt` structure exist.

---

## Reference Release: v0.1.0

- Tag: `v0.1.0` — commit `d654a06`
- GitHub release title: `OpenVibely 0.1.0`
- Assets published: darwin amd64/arm64 desktop `.app.zip`, darwin/linux amd64/arm64 server tarballs, windows amd64 server zip, windows amd64 desktop-cli zip, SHA256SUMS
- Docker image: `openvibely/openvibely:0.1.0`
