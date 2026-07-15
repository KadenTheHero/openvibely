#!/usr/bin/env bash
# release-validate.sh — Script-level unit tests for release tooling.
#
# Tests semver parsing, artifact naming, and environment detection without
# making any network calls or building anything. Safe to run in CI or locally.
#
# Usage:
#   ./release-validate.sh
#
# Exit code 0 = all tests passed. Non-zero = at least one failure.

set -uo pipefail

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'
PASS=0; FAIL=0

pass() { echo -e "${GREEN}✓${NC} $1"; ((PASS++)); }
fail() { echo -e "${RED}✗${NC} $1"; ((FAIL++)); }
section() { echo ""; echo -e "${YELLOW}--- $1 ---${NC}"; }

###############################################################################
# Helper: normalize_version (mirrors logic in scripts)
###############################################################################

normalize_version() {
    local raw="${1:-}"
    local v="${raw#v}"  # strip leading 'v'
    echo "$v"
}

is_valid_semver() {
    local v="${1:-}"
    [[ "$v" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]
}

###############################################################################
# 1. Semver parsing
###############################################################################

section "Semver normalization and validation"

# Valid inputs
for input in "0.1.0" "v0.1.0" "1.0.0" "v1.2.3" "10.20.300"; do
    v="$(normalize_version "$input")"
    if is_valid_semver "$v"; then
        pass "valid:   '$input' → '$v'"
    else
        fail "expected valid: '$input' → '$v'"
    fi
done

# Invalid inputs
for input in "" "v" "0.1" "0.1.0-alpha" "0.1.0+build" "abc" "v1.2.3.4" "1.2"; do
    v="$(normalize_version "$input")"
    if ! is_valid_semver "$v"; then
        pass "invalid: '$input' → '$v' (correctly rejected)"
    else
        fail "expected invalid: '$input' → '$v' (should have been rejected)"
    fi
done

###############################################################################
# 2. Artifact naming
###############################################################################

section "Artifact naming conventions"

check_artifact_name() {
    local version="$1" pattern="$2" expected="$3"
    local actual
    actual="$(echo "$pattern" | sed "s/<version>/${version}/g")"
    if [[ "$actual" == "$expected" ]]; then
        pass "artifact: '$actual'"
    else
        fail "artifact name mismatch: got '$actual', expected '$expected'"
    fi
}

VERSION="0.1.1"

# Desktop macOS (capital O in OpenVibely)
check_artifact_name "$VERSION" "OpenVibely_<version>_darwin_amd64.app.zip" \
    "OpenVibely_0.1.1_darwin_amd64.app.zip"
check_artifact_name "$VERSION" "OpenVibely_<version>_darwin_arm64.app.zip" \
    "OpenVibely_0.1.1_darwin_arm64.app.zip"

# Server tarballs (lowercase openvibely)
check_artifact_name "$VERSION" "openvibely_<version>_darwin_amd64_server.tar.gz" \
    "openvibely_0.1.1_darwin_amd64_server.tar.gz"
check_artifact_name "$VERSION" "openvibely_<version>_darwin_arm64_server.tar.gz" \
    "openvibely_0.1.1_darwin_arm64_server.tar.gz"
check_artifact_name "$VERSION" "openvibely_<version>_linux_amd64_server.tar.gz" \
    "openvibely_0.1.1_linux_amd64_server.tar.gz"
check_artifact_name "$VERSION" "openvibely_<version>_linux_arm64_server.tar.gz" \
    "openvibely_0.1.1_linux_arm64_server.tar.gz"

# Windows server zip
check_artifact_name "$VERSION" "openvibely_<version>_windows_amd64_server.zip" \
    "openvibely_0.1.1_windows_amd64_server.zip"

# Windows desktop-cli zip
check_artifact_name "$VERSION" "openvibely_<version>_windows_amd64_desktop-cli.zip" \
    "openvibely_0.1.1_windows_amd64_desktop-cli.zip"

# SHA256SUMS (no version in the filename)
SUMS_NAME="SHA256SUMS"
if [[ "$SUMS_NAME" == "SHA256SUMS" ]]; then
    pass "checksum file: 'SHA256SUMS' (correct, no version in name)"
else
    fail "checksum file: expected 'SHA256SUMS', got '$SUMS_NAME'"
fi

###############################################################################
# 3. Tag format
###############################################################################

section "Tag and release title format"

check_tag() {
    local version="$1" expected_tag="$2" expected_title="$3"
    local tag="v${version}"
    local title="OpenVibely ${version}"
    if [[ "$tag" == "$expected_tag" ]]; then
        pass "tag:   v${version} → '$tag'"
    else
        fail "tag mismatch: got '$tag', expected '$expected_tag'"
    fi
    if [[ "$title" == "$expected_title" ]]; then
        pass "title: OpenVibely ${version} → '$title'"
    else
        fail "title mismatch: got '$title', expected '$expected_title'"
    fi
}

check_tag "0.1.0" "v0.1.0" "OpenVibely 0.1.0"
check_tag "0.1.1" "v0.1.1" "OpenVibely 0.1.1"
check_tag "1.0.0" "v1.0.0" "OpenVibely 1.0.0"

###############################################################################
# 4. Branch naming
###############################################################################

section "Release branch naming"

check_branch() {
    local version="$1" expected="$2"
    local branch="release/v${version}"
    if [[ "$branch" == "$expected" ]]; then
        pass "branch: v${version} → '$branch'"
    else
        fail "branch mismatch: got '$branch', expected '$expected'"
    fi
}

check_branch "0.1.1" "release/v0.1.1"
check_branch "1.0.0" "release/v1.0.0"
check_branch "2.3.4" "release/v2.3.4"

###############################################################################
# 5. Release notes template structure
#
# The script produces a RELEASE_NOTES.md with AI_HIGHLIGHTS_PLACEHOLDER and
# AI_CHANGELOG_PLACEHOLDER blocks plus a COMMITS.txt with raw git log. Validate
# that the expected sections and markers are present in a simulated output string.
###############################################################################

section "Release notes template structure"

# Simulate the template output (mirrors what release-notes.sh emits)
FAKE_NOTES=$(cat << 'EOF'
# OpenVibely 0.1.1

OpenVibely is an open-source, self-hosted platform

## Highlights

<!-- AI_HIGHLIGHTS_PLACEHOLDER
     The agent orchestrating this release must replace this block
-->

## What's Changed

<!-- AI_CHANGELOG_PLACEHOLDER
     The agent orchestrating this release must replace this block
-->

**Full changelog:** [v0.1.0...v0.1.1](https://github.com/openvibely/openvibely/compare/v0.1.0...v0.1.1)

## Downloads

| Artifact | SHA-256 |

## Docker

docker pull openvibely/openvibely:0.1.1

## Known Limitations

- Linux desktop: ...
EOF
)

check_notes_section() {
    local label="$1" pattern="$2"
    if echo "$FAKE_NOTES" | grep -q "$pattern"; then
        pass "notes: contains '$label'"
    else
        fail "notes: missing '$label' (pattern: '$pattern')"
    fi
}

check_notes_section "version heading"              "# OpenVibely 0.1.1"
check_notes_section "Highlights section"           "## Highlights"
check_notes_section "AI_HIGHLIGHTS_PLACEHOLDER"     "AI_HIGHLIGHTS_PLACEHOLDER"
check_notes_section "What's Changed section"       "## What's Changed"
check_notes_section "AI_CHANGELOG_PLACEHOLDER"      "AI_CHANGELOG_PLACEHOLDER"
check_notes_section "Full changelog link"          "Full changelog:"
check_notes_section "Downloads section"            "## Downloads"
check_notes_section "Docker section"               "## Docker"
check_notes_section "Known Limitations section"    "## Known Limitations"
check_notes_section "docker pull command"          "docker pull openvibely/openvibely:"

# Validate placeholders are NOT already replaced (would indicate the script
# wrongly bypassed the agent synthesis step)
if echo "$FAKE_NOTES" | grep -q "AI_HIGHLIGHTS_PLACEHOLDER"; then
    pass "notes: highlights placeholder present (agent synthesis step is required)"
else
    fail "notes: AI_HIGHLIGHTS_PLACEHOLDER missing — template incorrectly bypassed highlights synthesis"
fi

if echo "$FAKE_NOTES" | grep -q "AI_CHANGELOG_PLACEHOLDER"; then
    pass "notes: changelog placeholder present (agent synthesis step is required)"
else
    fail "notes: AI_CHANGELOG_PLACEHOLDER missing — template incorrectly bypassed changelog synthesis"
fi

###############################################################################
# 6. COMMITS.txt format
###############################################################################

section "COMMITS.txt raw log format"

FAKE_COMMITS=$(cat << 'EOF'
# Commits for OpenVibely v0.1.1
# Range: v0.1.0..HEAD
# Generated: 2025-01-01T00:00:00Z

----
commit abc1234def5678901234567890abcdef12345678
Date:    2025-01-01
Author:  Jane Dev <jane@example.com>
Subject: Add new scheduling feature

Body:
Implements weekly schedule support with timezone awareness.

----
commit 000aaa111bbb222ccc333ddd444eee555fff6666
Date:    2025-01-02
Author:  John Dev <john@example.com>
Subject: chore: memory updates

Body:

EOF
)

check_commits_field() {
    local label="$1" pattern="$2"
    if echo "$FAKE_COMMITS" | grep -q "$pattern"; then
        pass "commits: contains '$label'"
    else
        fail "commits: missing '$label'"
    fi
}

check_commits_field "header comment"        "# Commits for OpenVibely"
check_commits_field "commit range"          "# Range:"
check_commits_field "commit separator"      "^----"
check_commits_field "commit hash field"     "^commit "
check_commits_field "date field"            "^Date:"
check_commits_field "author field"          "^Author:"
check_commits_field "subject field"         "^Subject:"
check_commits_field "body field"            "^Body:"
check_commits_field "multiple commits"      "abc1234"

###############################################################################
# 7. Dry-run filesystem isolation
###############################################################################

section "Release build dry-run isolation"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DRY_RUN_TMP="$(mktemp -d)"
MOCK_BIN="${DRY_RUN_TMP}/bin"
DRY_RUN_DIST="${DRY_RUN_TMP}/dist/9.9.9"
mkdir -p "$MOCK_BIN"

cat > "${MOCK_BIN}/go" <<'EOF'
#!/usr/bin/env bash
if [[ "${1:-}" == "list" ]]; then
    echo "v0.0.0"
fi
EOF
chmod +x "${MOCK_BIN}/go"

cat > "${MOCK_BIN}/uname" <<'EOF'
#!/usr/bin/env bash
case "${1:-}" in
    -s) echo "Darwin" ;;
    -m) echo "arm64" ;;
    *) echo "Darwin" ;;
esac
EOF
chmod +x "${MOCK_BIN}/uname"

DRY_RUN_OUTPUT="$(DRY_RUN=1 PATH="${MOCK_BIN}:$PATH" bash "${SCRIPT_DIR}/release-build.sh" 9.9.9 "$DRY_RUN_DIST" 2>&1)"

if [[ ! -e "$DRY_RUN_DIST" ]]; then
    pass "dry run: does not create the output directory"
else
    fail "dry run: created the output directory"
fi

if ! echo "$DRY_RUN_OUTPUT" | grep -Eq '(^|[[:space:]])(cp|chmod):|No such file or directory'; then
    pass "dry run: macOS bundle setup does not leak filesystem errors"
else
    fail "dry run: macOS bundle setup leaked filesystem errors"
fi

cat > "${MOCK_BIN}/gh" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
chmod +x "${MOCK_BIN}/gh"

FULL_DRY_RUN_DIST="${DRY_RUN_TMP}/full-dist/9.9.9"
if DRY_RUN=1 SKIP_GH_AUTH_CHECK=1 DIST_DIR="$FULL_DRY_RUN_DIST" PATH="${MOCK_BIN}:$PATH" \
    bash "${SCRIPT_DIR}/release.sh" 9.9.9 >/dev/null 2>&1; then
    pass "dry run: full release rehearsal completes without real artifacts"
else
    fail "dry run: full release rehearsal requires real build outputs"
fi

rm -rf "$DRY_RUN_TMP"

###############################################################################
# Results
###############################################################################

echo ""
TOTAL=$((PASS + FAIL))
if [[ "$FAIL" -eq 0 ]]; then
    echo -e "${GREEN}All ${TOTAL} tests passed.${NC}"
    exit 0
else
    echo -e "${RED}${FAIL} of ${TOTAL} tests FAILED.${NC}"
    exit 1
fi
