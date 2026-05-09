package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEnvFileLoadsValuesWithoutOverridingExistingEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.env")
	if err := os.WriteFile(path, []byte("# comment\nOPENVIBELY_APP_DATA_DIR=/tmp/openvibely\nEXISTING=from-file\nQUOTED='hello world'\n"), 0o644); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	t.Setenv("EXISTING", "from-env")
	unsetEnvForTest(t, "OPENVIBELY_APP_DATA_DIR")
	unsetEnvForTest(t, "QUOTED")

	if err := LoadEnvFile(path); err != nil {
		t.Fatalf("LoadEnvFile: %v", err)
	}
	if got := os.Getenv("OPENVIBELY_APP_DATA_DIR"); got != "/tmp/openvibely" {
		t.Fatalf("OPENVIBELY_APP_DATA_DIR=%q", got)
	}
	if got := os.Getenv("EXISTING"); got != "from-env" {
		t.Fatalf("EXISTING should not be overridden, got %q", got)
	}
	if got := os.Getenv("QUOTED"); got != "hello world" {
		t.Fatalf("QUOTED=%q", got)
	}
}

func TestLoadEnvFileRejectsMalformedLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.env")
	if err := os.WriteFile(path, []byte("NOT_VALID\n"), 0o644); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	if err := LoadEnvFile(path); err == nil {
		t.Fatal("expected malformed env file error")
	}
}

func unsetEnvForTest(t *testing.T, key string) {
	t.Helper()
	old, had := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset %s: %v", key, err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, old)
			return
		}
		_ = os.Unsetenv(key)
	})
}
