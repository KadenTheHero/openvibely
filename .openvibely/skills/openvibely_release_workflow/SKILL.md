---
kind: openvibely.agent_skill
version: 1
skill:
    key: openvibely_release_workflow
    name: OpenVibely Release Workflow
    scope: project
    description: Automate the OpenVibely release process — preflight, artifact builds, AI-synthesized release notes, and GitHub release publishing — for a given semver version.
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
| `Release version 0.1.1` | Full pipeline — preflight, build, notes, publish |
| `Release version 0.1.1 — dry run only` | Runs all steps with `DRY_RUN=1`; no git push, no GitHub release |
| `Release version 0.1.1 — create as draft` | GitHub release is created as a draft for review before publishing |
| `Release version 0.1.1 — skip Docker` | Suppresses the Docker reminder (still a manual step regardless) |
| `Release version 0.1.1 — build step only` | Runs preflight + build; stops before notes or publish |
| `Release version 0.1.1 — preflight already passed, start from build` | Skips preflight, runs build → notes → publish |

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
| `gh` (GitHub CLI) | Create GitHub release, upload assets | https://cli.github.com |
| `zip` | Package Windows and macOS archives | pre-installed macOS/Linux |
| `tar` | Package server tarballs | pre-installed |
| `sha256sum` / `shasum` | Generate SHA256SUMS | pre-installed (Linux/macOS) |
| `docker` *(optional)* | Build and push Docker image | https://docker.com |
| `x86_64-w64-mingw32-gcc` *(optional)* | Cross-compile Windows desktop-cli | `brew install mingw-w64` |

**macOS-only:** macOS desktop `.app` bundles can only be built on a macOS host. Server artifacts for all platforms can be cross-compiled from any OS.

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

The orchestrator runs steps in sequence, with a mandatory AI synthesis + review pause before publishing.

```
Step 1: release-preflight.sh  — validate environment, auth, tags, build caps
Step 2: release-build.sh      — build all artifacts → dist/0.1.1/
Step 3: release-notes.sh      — collect COMMITS.txt, render RELEASE_NOTES.md shell
Step 4: (agent)               — read COMMITS.txt, synthesize changelog, fill placeholder
Step 5: (review)              — confirm RELEASE_NOTES.md looks correct
Step 6: release-publish.sh    — create GitHub release, upload artifacts
```

Step 4 is not automated — the orchestrating agent reads `COMMITS.txt` and writes the "What's Changed" section using AI judgment (see Changelog Generation below).

### Option B: Step-by-step (for debugging or partial runs)

```bash
SCRIPTS=".openvibely/skills/openvibely_release_workflow/scripts"

# 1. Preflight
bash $SCRIPTS/release-preflight.sh 0.1.1

# 2. Build artifacts
bash $SCRIPTS/release-build.sh 0.1.1 ./dist/0.1.1

# 3. Collect commits + render notes template
bash $SCRIPTS/release-notes.sh 0.1.1 v0.1.0 ./dist/0.1.1

# 4. (Agent step) Read commits, synthesize changelog, fill placeholder
#    Read:  ./dist/0.1.1/COMMITS.txt
#    Edit:  ./dist/0.1.1/RELEASE_NOTES.md  (replace AI_CHANGELOG_PLACEHOLDER block)

# 5. Review the final RELEASE_NOTES.md, then publish
bash $SCRIPTS/release-publish.sh 0.1.1 ./dist/0.1.1
```

### Dry-run mode

All scripts respect `DRY_RUN=1`. In dry-run mode, build commands, git operations, and GitHub API calls are printed but not executed. Run this first on a new release to confirm the expected behavior.

```bash
DRY_RUN=1 .openvibely/skills/openvibely_release_workflow/scripts/release.sh 0.1.1
```

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

## Changelog Generation

`release-notes.sh` is deterministic: it collects raw commit data and renders the static template, but **does not synthesize the changelog**. That step requires AI judgment and is explicitly delegated to the orchestrating agent.

### What the script does

1. Runs `git log <prev_tag>..HEAD` with full commit metadata (hash, date, author, subject, body) and writes it to `COMMITS.txt` in the dist dir.
2. Renders `RELEASE_NOTES.md` with all static sections populated (Highlights, Downloads table, Docker, Known Limitations) and an `AI_CHANGELOG_PLACEHOLDER` comment block in the "What's Changed" section.

### What the agent must do (AI synthesis step)

After `release-notes.sh` runs, the agent must:

1. **Read `dist/<version>/COMMITS.txt`** — full commit log since the previous release.
2. **Synthesize a high-level, user-facing changelog** — not a dump of commit subjects. The agent should:
   - Identify what actually changed from a user's perspective.
   - Group related commits into coherent bullet points (3–8 total, plain English).
   - Leave out noise: internal refactors, memory updates, test changes, typo fixes, chore commits, anything with no user impact.
   - Combine multiple small commits about the same feature into one bullet.
   - If commit messages are terse or technical, infer intent from context (file paths, bodies, task references).
   - Inspect related diffs or touched paths for ambiguous commits when practical instead of guessing from subjects alone.
3. **Replace the `AI_CHANGELOG_PLACEHOLDER` block** in `RELEASE_NOTES.md` with the synthesized content.
4. Confirm the notes look correct before running `release-publish.sh`: no placeholder text, duplicated commit-derived bullets, leaked secrets, or claims about Docker/artifacts/features that the scripts did not actually build or verify.

### Why this approach

Commit messages in this repo are not always detailed or conventional. A regex-based grouper produces noisy, low-signal output. The agent reading the full commit context (including bodies and file diffs if needed) produces a far better high-level summary that's actually useful to users reading the GitHub release page.

---

## Release Notes Template (matches v0.1.0)

The generated notes follow this structure:

```
# OpenVibely <version>

<product intro paragraph>

## Highlights
<static feature bullets>

## What's Changed
<grouped commit log>
Full changelog: <link>

## Downloads
<SHA256 table from SHA256SUMS>

## Docker
<docker pull / run commands>

## Known Limitations
<static limitation bullets>
```

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
- Release notes template structure (`AI_CHANGELOG_PLACEHOLDER` present, all required sections)
- `COMMITS.txt` output structure (hash, date, author, subject, body fields present)
- If deterministic changelog grouping is replaced with AI synthesis, remove obsolete regex bucket tests and keep tests proving the placeholder and `COMMITS.txt` structure exist.

---

## Reference Release: v0.1.0

- Tag: `v0.1.0` — commit `d654a06`
- GitHub release title: `OpenVibely 0.1.0`
- Assets published: darwin amd64/arm64 desktop `.app.zip`, darwin/linux amd64/arm64 server tarballs, windows amd64 server zip, windows amd64 desktop-cli zip, SHA256SUMS
- Docker image: `openvibely/openvibely:0.1.0`
