package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openvibely/openvibely/internal/repository"
	"github.com/openvibely/openvibely/internal/testutil"
)

func TestParseGitHubRepoURL(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{name: "https", raw: "https://github.com/openvibely/openvibely", wantOwner: "openvibely", wantRepo: "openvibely"},
		{name: "https .git", raw: "https://github.com/openvibely/openvibely.git", wantOwner: "openvibely", wantRepo: "openvibely"},
		{name: "ssh short", raw: "git@github.com:openvibely/openvibely.git", wantOwner: "openvibely", wantRepo: "openvibely"},
		{name: "ssh url", raw: "ssh://git@github.com/openvibely/openvibely.git", wantOwner: "openvibely", wantRepo: "openvibely"},
		{name: "owner repo", raw: "openvibely/openvibely", wantOwner: "openvibely", wantRepo: "openvibely"},
		{name: "invalid host", raw: "https://gitlab.com/openvibely/openvibely", wantErr: true},
		{name: "invalid shape", raw: "https://github.com/openvibely", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseGitHubRepoURL(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tt.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.raw, err)
			}
			if got.Owner != tt.wantOwner || got.Name != tt.wantRepo {
				t.Fatalf("unexpected parse result: owner=%q repo=%q", got.Owner, got.Name)
			}
			if got.HTMLURL != "https://github.com/"+tt.wantOwner+"/"+tt.wantRepo {
				t.Fatalf("unexpected HTML URL: %s", got.HTMLURL)
			}
			if got.CloneURL != got.HTMLURL+".git" {
				t.Fatalf("unexpected clone URL: %s", got.CloneURL)
			}
		})
	}
}

func TestNormalizeGitHubAuthMode(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "pat", want: GitHubAuthModePAT},
		{in: "PAT", want: GitHubAuthModePAT},
		{in: "app", want: GitHubAuthModeApp},
		{in: "APP", want: GitHubAuthModeApp},
		{in: "", want: GitHubAuthModePAT},
		{in: "unknown", want: GitHubAuthModePAT},
	}

	for _, tt := range tests {
		if got := NormalizeGitHubAuthMode(tt.in); got != tt.want {
			t.Fatalf("NormalizeGitHubAuthMode(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestGitHubTokenEnv_UsesBasicAuthHeader(t *testing.T) {
	env := gitHubTokenEnv("ghp_example")
	var headerVal string
	for _, item := range env {
		if strings.HasPrefix(item, "GIT_CONFIG_VALUE_0=") {
			headerVal = strings.TrimPrefix(item, "GIT_CONFIG_VALUE_0=")
			break
		}
	}
	if headerVal == "" {
		t.Fatal("expected GIT_CONFIG_VALUE_0 to be set")
	}
	if !strings.HasPrefix(headerVal, "AUTHORIZATION: Basic ") {
		t.Fatalf("expected Basic auth header, got %q", headerVal)
	}
	encoded := strings.TrimPrefix(headerVal, "AUTHORIZATION: Basic ")
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("failed decoding header token: %v", err)
	}
	if string(raw) != "x-access-token:ghp_example" {
		t.Fatalf("unexpected decoded auth payload: %q", string(raw))
	}
}

func TestGitAuthEnvForRepo_PATMode(t *testing.T) {
	db := testutil.NewTestDB(t)
	settingsRepo := repository.NewSettingsRepo(db)
	ctx := context.Background()

	if err := settingsRepo.Set(ctx, GitHubSettingAuthMode, GitHubAuthModePAT); err != nil {
		t.Fatalf("set auth mode: %v", err)
	}
	if err := settingsRepo.Set(ctx, GitHubSettingPAT, "ghp_repo_scoped"); err != nil {
		t.Fatalf("set pat: %v", err)
	}

	repoDir := createTestGitRepo(t)
	cmd := exec.Command("git", "remote", "add", "origin", "https://github.com/openvibely/openvibely.git")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git remote add origin failed: %v\n%s", err, out)
	}

	svc := NewGitHubService(settingsRepo, "", "", "", "")
	env := svc.GitAuthEnvForRepo(ctx, repoDir)
	if len(env) == 0 {
		t.Fatal("expected auth env for github repo in PAT mode")
	}

	var headerVal string
	for _, item := range env {
		if strings.HasPrefix(item, "GIT_CONFIG_VALUE_0=") {
			headerVal = strings.TrimPrefix(item, "GIT_CONFIG_VALUE_0=")
			break
		}
	}
	if !strings.Contains(headerVal, "Basic ") {
		t.Fatalf("expected Basic header, got %q", headerVal)
	}
}

func TestEnsureGitSSLConfig(t *testing.T) {
	t.Run("already configured with GIT_SSL_CAINFO", func(t *testing.T) {
		env := []string{"GIT_SSL_CAINFO=/custom/ca.pem", "OTHER=value"}
		result := ensureGitSSLConfig(env)
		if len(result) != 2 {
			t.Fatalf("expected env unchanged when GIT_SSL_CAINFO already set, got %d items", len(result))
		}
		if result[0] != "GIT_SSL_CAINFO=/custom/ca.pem" {
			t.Fatalf("expected GIT_SSL_CAINFO preserved")
		}
	})

	t.Run("already configured with SSL_CERT_FILE", func(t *testing.T) {
		env := []string{"SSL_CERT_FILE=/custom/cert.pem"}
		result := ensureGitSSLConfig(env)
		if len(result) != 1 {
			t.Fatalf("expected env unchanged when SSL_CERT_FILE already set")
		}
	})

	t.Run("adds CA bundle if found or falls back to no-verify", func(t *testing.T) {
		env := []string{"PATH=/usr/bin"}
		result := ensureGitSSLConfig(env)
		// Should either add GIT_SSL_CAINFO or GIT_SSL_NO_VERIFY
		// We can't predict which CA bundle exists on the test system
		foundCAInfo := false
		foundNoVerify := false
		for _, e := range result {
			if strings.HasPrefix(e, "GIT_SSL_CAINFO=") {
				foundCAInfo = true
			}
			if strings.HasPrefix(e, "GIT_SSL_NO_VERIFY=") {
				foundNoVerify = true
			}
		}
		// One of them must be set
		if !foundCAInfo && !foundNoVerify {
			t.Fatal("expected either GIT_SSL_CAINFO or GIT_SSL_NO_VERIFY to be set")
		}
		if foundCAInfo {
			t.Logf("CA bundle found and configured: %v", result)
		} else {
			t.Logf("No CA bundle found, falling back to GIT_SSL_NO_VERIFY")
		}
	})

	t.Run("respects existing GIT_SSL_NO_VERIFY in env", func(t *testing.T) {
		env := []string{"GIT_SSL_NO_VERIFY=false"}
		result := ensureGitSSLConfig(env)
		if len(result) != 1 {
			t.Fatalf("expected env unchanged when GIT_SSL_NO_VERIFY already set")
		}
		if result[0] != "GIT_SSL_NO_VERIFY=false" {
			t.Fatalf("expected GIT_SSL_NO_VERIFY preserved")
		}
	})
}

func TestFormatGitHubAPIError(t *testing.T) {
	t.Run("formats message and nested errors", func(t *testing.T) {
		body := []byte(`{"message":"Validation Failed","errors":[{"resource":"PullRequest","field":"head","code":"invalid","message":"A pull request already exists for openvibely:task/x."}]}`)
		got := formatGitHubAPIError(body)
		if !strings.Contains(got, "Validation Failed") {
			t.Fatalf("expected top-level message, got %q", got)
		}
		if !strings.Contains(got, "A pull request already exists") {
			t.Fatalf("expected nested error detail, got %q", got)
		}
	})

	t.Run("falls back to raw body when not json", func(t *testing.T) {
		got := formatGitHubAPIError([]byte("plain-text-error"))
		if got != "plain-text-error" {
			t.Fatalf("expected raw body fallback, got %q", got)
		}
	})
}

func TestCloneProjectRepo_NoPATFallsBackToLocalGitCLI(t *testing.T) {
	db := testutil.NewTestDB(t)
	settingsRepo := repository.NewSettingsRepo(db)
	ctx := context.Background()
	root := t.TempDir()
	if err := settingsRepo.Set(ctx, GitHubSettingAuthMode, GitHubAuthModePAT); err != nil {
		t.Fatalf("set auth mode: %v", err)
	}

	svc := NewGitHubService(settingsRepo, "", "", "", root)
	var calls []struct {
		env  []string
		args []string
	}
	svc.runGit = func(_ context.Context, _ string, extraEnv []string, args ...string) ([]byte, error) {
		calls = append(calls, struct {
			env  []string
			args []string
		}{append([]string(nil), extraEnv...), append([]string(nil), args...)})
		return nil, nil
	}

	clonedPath, normalizedURL, err := svc.CloneProjectRepo(ctx, "project-1", "https://github.com/openvibely/openvibely")
	if err != nil {
		t.Fatalf("CloneProjectRepo returned error: %v", err)
	}
	if clonedPath == "" || !strings.HasSuffix(clonedPath, "project-1") {
		t.Fatalf("expected managed clone destination for project id, got %q", clonedPath)
	}
	if normalizedURL != "https://github.com/openvibely/openvibely" {
		t.Fatalf("unexpected normalized URL: %q", normalizedURL)
	}
	if len(calls) != 1 {
		t.Fatalf("expected one local git clone call, got %d", len(calls))
	}
	if got := strings.Join(calls[0].args, " "); got != "clone https://github.com/openvibely/openvibely "+clonedPath {
		t.Fatalf("unexpected git args: %q", got)
	}
	if envContainsPrefix(calls[0].env, "GIT_CONFIG_VALUE_0=") {
		t.Fatalf("local fallback should not inject GitHub auth header, got env %v", calls[0].env)
	}
	assertLocalGitCloneEnv(t, calls[0].env)
}

func TestCloneProjectRepo_NoPATFallbackPreservesSSHCloneURL(t *testing.T) {
	db := testutil.NewTestDB(t)
	settingsRepo := repository.NewSettingsRepo(db)
	ctx := context.Background()
	root := t.TempDir()
	if err := settingsRepo.Set(ctx, GitHubSettingAuthMode, GitHubAuthModePAT); err != nil {
		t.Fatalf("set auth mode: %v", err)
	}

	svc := NewGitHubService(settingsRepo, "", "", "", root)
	var gotArgs []string
	var gotEnv []string
	svc.runGit = func(_ context.Context, _ string, extraEnv []string, args ...string) ([]byte, error) {
		gotEnv = append([]string(nil), extraEnv...)
		gotArgs = append([]string(nil), args...)
		return nil, nil
	}

	_, normalizedURL, err := svc.CloneProjectRepo(ctx, "project-ssh", "git@github.com:openvibely/openvibely.git")
	if err != nil {
		t.Fatalf("CloneProjectRepo returned error: %v", err)
	}
	if normalizedURL != "https://github.com/openvibely/openvibely" {
		t.Fatalf("unexpected normalized URL: %q", normalizedURL)
	}
	if len(gotArgs) < 2 || gotArgs[0] != "clone" || gotArgs[1] != "git@github.com:openvibely/openvibely.git" {
		t.Fatalf("expected local fallback to preserve SSH clone URL, got args %v", gotArgs)
	}
	assertLocalGitCloneEnv(t, gotEnv)
}

func TestCloneProjectRepo_PATConfiguredUsesTokenClone(t *testing.T) {
	db := testutil.NewTestDB(t)
	settingsRepo := repository.NewSettingsRepo(db)
	ctx := context.Background()
	root := t.TempDir()
	if err := settingsRepo.Set(ctx, GitHubSettingAuthMode, GitHubAuthModePAT); err != nil {
		t.Fatalf("set auth mode: %v", err)
	}
	if err := settingsRepo.Set(ctx, GitHubSettingPAT, "ghp_configured"); err != nil {
		t.Fatalf("set pat: %v", err)
	}

	svc := NewGitHubService(settingsRepo, "", "", "", root)
	var gotEnv []string
	svc.runGit = func(_ context.Context, _ string, extraEnv []string, args ...string) ([]byte, error) {
		gotEnv = append([]string(nil), extraEnv...)
		if len(args) == 0 || args[0] != "clone" {
			t.Fatalf("expected git clone, got %v", args)
		}
		return nil, nil
	}

	if _, _, err := svc.CloneProjectRepo(ctx, "project-token", "https://github.com/openvibely/openvibely"); err != nil {
		t.Fatalf("CloneProjectRepo returned error: %v", err)
	}
	if !envContainsPrefix(gotEnv, "GIT_CONFIG_VALUE_0=AUTHORIZATION: Basic ") {
		t.Fatalf("expected token auth header env, got %v", gotEnv)
	}
}

func TestCloneProjectRepo_LocalGitFallbackFailureIncludesGitFailure(t *testing.T) {
	db := testutil.NewTestDB(t)
	settingsRepo := repository.NewSettingsRepo(db)
	ctx := context.Background()
	root := t.TempDir()
	if err := settingsRepo.Set(ctx, GitHubSettingAuthMode, GitHubAuthModePAT); err != nil {
		t.Fatalf("set auth mode: %v", err)
	}

	svc := NewGitHubService(settingsRepo, "", "", "", root)
	svc.runGit = func(_ context.Context, _ string, _ []string, _ ...string) ([]byte, error) {
		return nil, fmt.Errorf("git clone failed: authentication required")
	}

	_, _, err := svc.CloneProjectRepo(ctx, "project-fail", "https://github.com/openvibely/openvibely")
	if err == nil {
		t.Fatal("expected clone error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "GitHub auth was unavailable") {
		t.Fatalf("expected auth fallback context in error, got %q", msg)
	}
	if !strings.Contains(msg, "github personal access token is not configured") {
		t.Fatalf("expected missing PAT context in error, got %q", msg)
	}
	if !strings.Contains(msg, "local git clone failed") || !strings.Contains(msg, "authentication required") {
		t.Fatalf("expected underlying local git failure in error, got %q", msg)
	}
}

func TestCloneProjectRepo_LocalGitNonCredentialFailureOmitsPATContext(t *testing.T) {
	db := testutil.NewTestDB(t)
	settingsRepo := repository.NewSettingsRepo(db)
	ctx := context.Background()
	root := t.TempDir()
	if err := settingsRepo.Set(ctx, GitHubSettingAuthMode, GitHubAuthModePAT); err != nil {
		t.Fatalf("set auth mode: %v", err)
	}

	svc := NewGitHubService(settingsRepo, "", "", "", root)
	svc.runGit = func(_ context.Context, _ string, _ []string, _ ...string) ([]byte, error) {
		return nil, fmt.Errorf("git clone failed: unable to access remote: connection timed out")
	}

	_, _, err := svc.CloneProjectRepo(ctx, "project-network-fail", "https://github.com/openvibely/openvibely")
	if err == nil {
		t.Fatal("expected clone error")
	}
	msg := err.Error()
	if strings.Contains(msg, "github personal access token is not configured") {
		t.Fatalf("non-credential local git failure should not surface missing PAT context, got %q", msg)
	}
	if !strings.Contains(msg, "local git clone failed") || !strings.Contains(msg, "connection timed out") {
		t.Fatalf("expected underlying local git failure in error, got %q", msg)
	}
}

func assertLocalGitCloneEnv(t *testing.T, env []string) {
	t.Helper()
	for _, value := range []string{"GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=true", "SSH_ASKPASS=true"} {
		if !envContainsValue(env, value) {
			t.Fatalf("expected non-interactive local git fallback env %q, got %v", value, env)
		}
	}
}

func envContainsPrefix(env []string, prefix string) bool {
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			return true
		}
	}
	return false
}

func envContainsValue(env []string, value string) bool {
	for _, item := range env {
		if item == value {
			return true
		}
	}
	return false
}

func TestDefaultGitHubSDLCLabelsDoNotUseProductPrefix(t *testing.T) {
	for _, label := range DefaultGitHubSDLCLabels {
		if strings.HasPrefix(label, "openvibely:") {
			t.Fatalf("default GitHub SDLC label must not use openvibely prefix: %q", label)
		}
	}
}

func TestListAssignedIssuesWithPullRequestsSkipsIssuesWithoutAssociatedPR(t *testing.T) {
	db := testutil.NewTestDB(t)
	settingsRepo := repository.NewSettingsRepo(db)
	ctx := context.Background()
	if err := settingsRepo.Set(ctx, GitHubSettingAuthMode, GitHubAuthModePAT); err != nil {
		t.Fatalf("set auth mode: %v", err)
	}
	if err := settingsRepo.Set(ctx, GitHubSettingPAT, "ghp_test"); err != nil {
		t.Fatalf("set pat: %v", err)
	}

	var issueListPath string
	var timelinePaths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/openvibely/openvibely/issues":
			issueListPath = r.URL.RawQuery
			if r.URL.Query().Get("assignee") != "dev-bot" {
				t.Fatalf("expected assignee query dev-bot, got %q", r.URL.Query().Get("assignee"))
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
				{"number":1,"html_url":"https://github.com/openvibely/openvibely/issues/1","title":"No PR","state":"open","user":{"login":"alice"},"assignees":[{"login":"dev-bot"}],"labels":[{"name":"bug"}]},
				{"number":2,"html_url":"https://github.com/openvibely/openvibely/issues/2","title":"Has PR","state":"open","user":{"login":"alice"},"assignees":[{"login":"dev-bot"}],"labels":[{"name":"approved"}]},
				{"number":3,"html_url":"https://github.com/openvibely/openvibely/pull/3","title":"PR issue object","state":"open","pull_request":{}}
			]`))
		case "/repos/openvibely/openvibely/issues/1/timeline":
			timelinePaths = append(timelinePaths, r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		case "/repos/openvibely/openvibely/issues/2/timeline":
			timelinePaths = append(timelinePaths, r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
				{"source":{"issue":{"number":42,"html_url":"https://github.com/openvibely/openvibely/pull/42","state":"open","pull_request":{}}}}
			]`))
		default:
			t.Fatalf("unexpected GitHub API path: %s", r.URL.String())
		}
	}))
	defer server.Close()

	svc := NewGitHubService(settingsRepo, "", "", "", "")
	svc.apiBaseURL = server.URL
	repo := &GitHubRepoRef{Owner: "openvibely", Name: "openvibely"}

	items, err := svc.ListAssignedIssuesWithPullRequests(ctx, repo, "dev-bot")
	if err != nil {
		t.Fatalf("ListAssignedIssuesWithPullRequests returned error: %v", err)
	}
	if issueListPath == "" {
		t.Fatal("expected assigned issues endpoint to be called")
	}
	if len(timelinePaths) != 2 {
		t.Fatalf("expected timeline lookup only for two non-PR issues, got %d paths %v", len(timelinePaths), timelinePaths)
	}
	if len(items) != 1 {
		t.Fatalf("expected only issues with associated PRs, got %d: %#v", len(items), items)
	}
	if items[0].Issue.Number != 2 {
		t.Fatalf("expected issue 2 to be returned, got issue %d", items[0].Issue.Number)
	}
	if items[0].PullRequest.Number != 42 {
		t.Fatalf("expected associated PR #42, got #%d", items[0].PullRequest.Number)
	}
}

func TestListAuthenticatedAssignedIssuesUsesConfiguredTokenUser(t *testing.T) {
	db := testutil.NewTestDB(t)
	settingsRepo := repository.NewSettingsRepo(db)
	ctx := context.Background()
	if err := settingsRepo.Set(ctx, GitHubSettingAuthMode, GitHubAuthModePAT); err != nil {
		t.Fatalf("set auth mode: %v", err)
	}
	if err := settingsRepo.Set(ctx, GitHubSettingPAT, "ghp_test"); err != nil {
		t.Fatalf("set pat: %v", err)
	}

	var sawUser bool
	var issueListQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/user":
			sawUser = true
			if got := r.Header.Get("Authorization"); got != "Bearer ghp_test" {
				t.Fatalf("expected PAT bearer auth for /user, got %q", got)
			}
			_, _ = w.Write([]byte(`{"login":"channel-user"}`))
		case "/repos/openvibely/openvibely/issues":
			issueListQuery = r.URL.RawQuery
			if got := r.URL.Query().Get("assignee"); got != "channel-user" {
				t.Fatalf("expected authenticated channel user assignee, got %q", got)
			}
			if got := r.URL.Query().Get("state"); got != "open" {
				t.Fatalf("expected open issue query, got state=%q", got)
			}
			_, _ = w.Write([]byte(`[
				{"number":5,"html_url":"https://github.com/openvibely/openvibely/issues/5","title":"Testing","state":"open","user":{"login":"alice"},"assignees":[{"login":"channel-user"}],"labels":[{"name":"bug"}]},
				{"number":6,"html_url":"https://github.com/openvibely/openvibely/pull/6","title":"PR object","state":"open","pull_request":{}}
			]`))
		default:
			t.Fatalf("unexpected GitHub API path: %s", r.URL.String())
		}
	}))
	defer server.Close()

	svc := NewGitHubService(settingsRepo, "", "", "", "")
	svc.apiBaseURL = server.URL
	repo := &GitHubRepoRef{Owner: "openvibely", Name: "openvibely"}

	user, issues, err := svc.ListAuthenticatedAssignedIssues(ctx, repo)
	if err != nil {
		t.Fatalf("ListAuthenticatedAssignedIssues returned error: %v", err)
	}
	if !sawUser || issueListQuery == "" {
		t.Fatalf("expected /user and assigned issues endpoints, sawUser=%v query=%q", sawUser, issueListQuery)
	}
	if user == nil || user.Login != "channel-user" || user.Source != GitHubAuthModePAT {
		t.Fatalf("unexpected authenticated user: %#v", user)
	}
	if len(issues) != 1 || issues[0].Number != 5 || issues[0].Title != "Testing" {
		t.Fatalf("expected only real open issue assigned to token user, got %#v", issues)
	}
	cachedLogin, err := settingsRepo.Get(ctx, GitHubSettingPATUserLogin)
	if err != nil {
		t.Fatalf("get cached PAT login: %v", err)
	}
	if cachedLogin != "channel-user" {
		t.Fatalf("expected PAT login cache to be updated, got %q", cachedLogin)
	}
}

func TestListAuthenticatedAssignedIssuesRejectsGitHubAppInstallationAccount(t *testing.T) {
	db := testutil.NewTestDB(t)
	settingsRepo := repository.NewSettingsRepo(db)
	ctx := context.Background()
	if err := settingsRepo.Set(ctx, GitHubSettingAuthMode, GitHubAuthModeApp); err != nil {
		t.Fatalf("set auth mode: %v", err)
	}
	if err := settingsRepo.Set(ctx, githubSettingAccountLogin, "openvibely-org"); err != nil {
		t.Fatalf("set app account login: %v", err)
	}
	if err := settingsRepo.Set(ctx, githubSettingAccountType, "Organization"); err != nil {
		t.Fatalf("set app account type: %v", err)
	}
	if err := settingsRepo.Set(ctx, githubSettingInstallationID, "123"); err != nil {
		t.Fatalf("set installation id: %v", err)
	}

	var sawIssueList bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawIssueList = true
		t.Fatalf("github_list_my_assigned_issues must not query issues with GitHub App installation account %s", r.URL.String())
	}))
	defer server.Close()

	svc := NewGitHubService(settingsRepo, "", "", "", "")
	svc.apiBaseURL = server.URL
	repo := &GitHubRepoRef{Owner: "openvibely", Name: "openvibely"}

	_, _, err := svc.ListAuthenticatedAssignedIssues(ctx, repo)
	if err == nil || !strings.Contains(err.Error(), "requires a PAT user token") || !strings.Contains(err.Error(), "github_list_assigned_issues") {
		t.Fatalf("expected GitHub App guidance error, got %v", err)
	}
	if sawIssueList {
		t.Fatalf("expected no GitHub issue-list request for GitHub App installation account")
	}
}

func TestGitHubIssueLabelsRejectOpenVibelyPrefixBeforeTransport(t *testing.T) {
	db := testutil.NewTestDB(t)
	settingsRepo := repository.NewSettingsRepo(db)
	ctx := context.Background()
	if err := settingsRepo.Set(ctx, GitHubSettingAuthMode, GitHubAuthModePAT); err != nil {
		t.Fatalf("set auth mode: %v", err)
	}
	if err := settingsRepo.Set(ctx, GitHubSettingPAT, "ghp_test"); err != nil {
		t.Fatalf("set pat: %v", err)
	}

	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		t.Fatalf("prefixed labels must be rejected before GitHub transport, got %s %s", r.Method, r.URL.String())
	}))
	defer server.Close()

	svc := NewGitHubService(settingsRepo, "", "", "", "")
	svc.apiBaseURL = server.URL
	repo := &GitHubRepoRef{Owner: "openvibely", Name: "openvibely"}

	if _, err := svc.CreateIssue(ctx, repo, GitHubCreateIssueRequest{Title: "Bug", Labels: []string{"bug", " openvibely:bug "}}); err == nil || !strings.Contains(err.Error(), "openvibely:") {
		t.Fatalf("expected prefixed create label rejection, got %v", err)
	}
	if err := svc.AddLabelsToIssue(ctx, repo, 7, []string{"OpenVibely:approved"}); err == nil || !strings.Contains(err.Error(), "openvibely:") {
		t.Fatalf("expected prefixed add-label rejection, got %v", err)
	}
	if calls != 0 {
		t.Fatalf("expected no GitHub API calls for rejected labels, got %d", calls)
	}
}

func TestPublishBranchUsesGitHubAPIWithoutGitPush(t *testing.T) {
	db := testutil.NewTestDB(t)
	settingsRepo := repository.NewSettingsRepo(db)
	ctx := context.Background()
	if err := settingsRepo.Set(ctx, GitHubSettingAuthMode, GitHubAuthModePAT); err != nil {
		t.Fatalf("set auth mode: %v", err)
	}
	if err := settingsRepo.Set(ctx, GitHubSettingPAT, "ghp_test"); err != nil {
		t.Fatalf("set pat: %v", err)
	}

	repoDir := createTestGitRepo(t)
	baseCmd := exec.Command("git", "rev-parse", "main")
	baseCmd.Dir = repoDir
	baseOut, err := baseCmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse main: %v", err)
	}
	localBaseSHA := strings.TrimSpace(string(baseOut))
	remoteBaseSHA := "1111111111111111111111111111111111111111"
	if remoteBaseSHA == localBaseSHA {
		t.Fatal("test requires distinct local and remote base shas")
	}
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("updated\n"), 0o644); err != nil {
		t.Fatalf("write tracked file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "new.txt"), []byte("new file\n"), 0o644); err != nil {
		t.Fatalf("write new file: %v", err)
	}

	var paths []string
	var treePayload string
	var commitPayload string
	var refPayload string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		if got := r.Header.Get("Authorization"); got != "Bearer ghp_test" {
			t.Fatalf("expected PAT bearer auth, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/openvibely/openvibely/git/ref/heads/main":
			_, _ = w.Write([]byte(`{"object":{"sha":"` + remoteBaseSHA + `"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/openvibely/openvibely/git/commits/"+remoteBaseSHA:
			_, _ = w.Write([]byte(`{"tree":{"sha":"base-tree"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/openvibely/openvibely/git/blobs":
			_, _ = io.Copy(io.Discard, r.Body)
			_, _ = w.Write([]byte(fmt.Sprintf(`{"sha":"blob-%d"}`, len(paths))))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/openvibely/openvibely/git/trees":
			body, _ := io.ReadAll(r.Body)
			treePayload = string(body)
			if !strings.Contains(treePayload, `"base_tree":"base-tree"`) || !strings.Contains(treePayload, `"path":"README.md"`) || !strings.Contains(treePayload, `"path":"new.txt"`) {
				t.Fatalf("unexpected tree payload: %s", treePayload)
			}
			_, _ = w.Write([]byte(`{"sha":"new-tree"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/openvibely/openvibely/git/commits":
			body, _ := io.ReadAll(r.Body)
			commitPayload = string(body)
			if !strings.Contains(commitPayload, `"message":"Publish via API"`) || !strings.Contains(commitPayload, `"tree":"new-tree"`) || !strings.Contains(commitPayload, remoteBaseSHA) || strings.Contains(commitPayload, localBaseSHA) {
				t.Fatalf("unexpected commit payload: %s", commitPayload)
			}
			_, _ = w.Write([]byte(`{"sha":"new-commit"}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/repos/openvibely/openvibely/git/refs/heads/task/api-publish":
			body, _ := io.ReadAll(r.Body)
			refPayload = string(body)
			if !strings.Contains(refPayload, `"sha":"new-commit"`) || !strings.Contains(refPayload, `"force":false`) {
				t.Fatalf("unexpected ref payload: %s", refPayload)
			}
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Reference does not exist"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/openvibely/openvibely/git/refs":
			body, _ := io.ReadAll(r.Body)
			refPayload = string(body)
			if !strings.Contains(refPayload, `"ref":"refs/heads/task/api-publish"`) || !strings.Contains(refPayload, `"sha":"new-commit"`) {
				t.Fatalf("unexpected create-ref payload: %s", refPayload)
			}
			_, _ = w.Write([]byte(`{"ref":"refs/heads/task/api-publish","object":{"sha":"new-commit"}}`))
		default:
			t.Fatalf("unexpected GitHub API request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	svc := NewGitHubService(settingsRepo, "", "", "", "")
	svc.apiBaseURL = server.URL
	svc.runGit = func(ctx context.Context, dir string, extraEnv []string, args ...string) ([]byte, error) {
		if len(args) > 0 && (args[0] == "add" || args[0] == "commit" || args[0] == "push") {
			t.Fatalf("PublishBranch must not invoke git %s", args[0])
		}
		return defaultRunGit(ctx, dir, extraEnv, args...)
	}
	err = svc.PublishBranch(ctx, &GitHubRepoRef{Owner: "openvibely", Name: "openvibely"}, GitHubPublishBranchRequest{
		RepoPath:       repoDir,
		Branch:         "task/api-publish",
		BaseBranch:     "main",
		CommitMessage:  "Publish via API",
		CommitterName:  "OpenVibely Bot",
		CommitterEmail: "bot@openvibely.ai",
	})
	if err != nil {
		t.Fatalf("PublishBranch returned error: %v", err)
	}
	if treePayload == "" || commitPayload == "" || refPayload == "" {
		t.Fatalf("expected tree, commit, and ref API calls paths=%v", paths)
	}
}

func TestGitHubIssueAPIMethods(t *testing.T) {
	db := testutil.NewTestDB(t)
	settingsRepo := repository.NewSettingsRepo(db)
	ctx := context.Background()
	if err := settingsRepo.Set(ctx, GitHubSettingAuthMode, GitHubAuthModePAT); err != nil {
		t.Fatalf("set auth mode: %v", err)
	}
	if err := settingsRepo.Set(ctx, GitHubSettingPAT, "ghp_test"); err != nil {
		t.Fatalf("set pat: %v", err)
	}

	var sawCreate, sawGet, sawComment, sawLabels bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/repos/openvibely/openvibely/issues":
			sawCreate = true
			body, _ := io.ReadAll(r.Body)
			text := string(body)
			if strings.Contains(text, "openvibely:") {
				t.Fatalf("issue creation must not send prefixed labels: %s", text)
			}
			if !strings.Contains(text, `"labels":["bug","approved"]`) || !strings.Contains(text, `"assignees":["dev-bot"]`) {
				t.Fatalf("unexpected create issue payload: %s", text)
			}
			_, _ = w.Write([]byte(`{"number":7,"html_url":"https://github.com/openvibely/openvibely/issues/7","title":"Bug","body":"Fix it","state":"open","user":{"login":"alice"},"assignees":[{"login":"dev-bot"}],"labels":[{"name":"bug"},{"name":"approved"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/openvibely/openvibely/issues/7":
			sawGet = true
			_, _ = w.Write([]byte(`{"number":7,"html_url":"https://github.com/openvibely/openvibely/issues/7","title":"Bug","body":"Fix it","state":"open","user":{"login":"alice"},"assignees":[{"login":"dev-bot"}],"labels":[{"name":"bug"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/openvibely/openvibely/issues/7/comments":
			sawComment = true
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), `"body":"working on it"`) {
				t.Fatalf("unexpected comment payload: %s", string(body))
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":1}`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/openvibely/openvibely/issues/7/labels":
			sawLabels = true
			body, _ := io.ReadAll(r.Body)
			text := string(body)
			if text != `{"labels":["in-progress","pr-opened"]}` {
				t.Fatalf("unexpected labels payload: %s", text)
			}
			_, _ = w.Write([]byte(`[{"name":"in-progress"},{"name":"pr-opened"}]`))
		default:
			t.Fatalf("unexpected GitHub API request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	svc := NewGitHubService(settingsRepo, "", "", "", "")
	svc.apiBaseURL = server.URL
	repo := &GitHubRepoRef{Owner: "openvibely", Name: "openvibely"}
	created, err := svc.CreateIssue(ctx, repo, GitHubCreateIssueRequest{Title: "Bug", Body: "Fix it", Labels: []string{"bug", "approved", "bug", ""}, Assignees: []string{"dev-bot"}})
	if err != nil {
		t.Fatalf("CreateIssue returned error: %v", err)
	}
	if created.Number != 7 || created.UserLogin != "alice" || len(created.Labels) != 2 {
		t.Fatalf("unexpected created issue: %#v", created)
	}
	issue, err := svc.GetIssue(ctx, repo, 7)
	if err != nil {
		t.Fatalf("GetIssue returned error: %v", err)
	}
	if issue.Number != 7 || issue.Title != "Bug" {
		t.Fatalf("unexpected issue: %#v", issue)
	}
	if err := svc.CommentOnIssue(ctx, repo, 7, "working on it"); err != nil {
		t.Fatalf("CommentOnIssue returned error: %v", err)
	}
	if err := svc.AddLabelsToIssue(ctx, repo, 7, []string{"in-progress", "pr-opened", "in-progress"}); err != nil {
		t.Fatalf("AddLabelsToIssue returned error: %v", err)
	}
	if !sawCreate || !sawGet || !sawComment || !sawLabels {
		t.Fatalf("expected all issue API endpoints to be called create=%v get=%v comment=%v labels=%v", sawCreate, sawGet, sawComment, sawLabels)
	}
}
