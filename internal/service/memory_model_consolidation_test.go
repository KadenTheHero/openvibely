package service

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openvibely/openvibely/internal/memory"
	"github.com/openvibely/openvibely/internal/models"
)

func TestBuildMemoryExtractionPromptUsesToolDrivenInstructions(t *testing.T) {
	prompt := buildMemoryExtractionPrompt(memory.Interaction{
		ProjectID:    "p1",
		SourceKind:   memory.SourceThread,
		SourceID:     "exec1",
		Title:        "Follow-up",
		UserText:     "I hate the separate schedule card; use normal scheduled tasks.",
		AssistantOut: "Implemented the change.",
		ChangedFiles: []string{"internal/service/memory_service.go"},
	}, "/tmp/memory/projects/p1")
	for _, want := range []string{
		"Work directly in the managed memory directory",
		"root-relative memory paths",
		"List the memory directory",
		"Read MEMORY.md",
		"Do not save raw complaints as memory",
		"procedure-only runbooks",
		"only an operational procedure",
		"User text:",
		"use normal scheduled tasks",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
	for _, bad := range []string{"strict JSON", "proposals", "rel_path", "/tmp/memory/projects/p1"} {
		if strings.Contains(prompt, bad) {
			t.Fatalf("prompt should not contain rule/proposal wording %q:\n%s", bad, prompt)
		}
	}
}

func TestBuildMemoryConsolidationPromptUsesDynamicRunContext(t *testing.T) {
	prompt := buildMemoryConsolidationPrompt("p1", "/tmp/memory/projects/p1", []models.Execution{{
		ID:         "exec1",
		TaskID:     "task1",
		Status:     models.ExecCompleted,
		PromptSent: "Use app-managed project memory outside repos.",
		Output:     "confirmed",
	}})
	for _, want := range []string{
		"# Memory Consolidation",
		"Project ID: p1",
		"root-relative memory paths",
		"MEMORY.md",
		"Memory is durable context, not a procedural skill library",
		"Recent Execution Snippets",
		"Use app-managed project memory outside repos.",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
	for _, bad := range []string{"strict JSON", "proposals", "rel_path", "Output Contract"} {
		if strings.Contains(prompt, bad) {
			t.Fatalf("prompt should not contain JSON proposal contract %q:\n%s", bad, prompt)
		}
	}
}

func TestMemoryConsolidationToolsRejectPathEscape(t *testing.T) {
	store := newServiceMemoryStore(t)
	dir, err := store.EnsureProject("p1")
	if err != nil {
		t.Fatal(err)
	}
	session := newMemoryScopedFileSession(dir)
	input, _ := json.Marshal(map[string]string{"file_path": "../escape.md", "content": "nope"})
	_, handled, isErr, err := session.execute(context.Background(), "write_file", input)
	if !handled || !isErr || err == nil {
		t.Fatalf("expected handled path escape error, handled=%v isErr=%v err=%v", handled, isErr, err)
	}
}

func TestMemoryConsolidationToolsWriteAndTrackTouch(t *testing.T) {
	store := newServiceMemoryStore(t)
	dir, err := store.EnsureProject("p1")
	if err != nil {
		t.Fatal(err)
	}
	session := newMemoryScopedFileSession(dir)
	input, _ := json.Marshal(map[string]string{"file_path": "example.md", "content": "---\nname: example\ntype: project\ntitle: Example\n---\n\nUseful memory."})
	out, handled, isErr, err := session.execute(context.Background(), "write_file", input)
	if err != nil || isErr || !handled {
		t.Fatalf("write failed handled=%v isErr=%v err=%v out=%s", handled, isErr, err, out)
	}
	res := session.result()
	if len(res.TouchedPaths) != 1 || res.TouchedPaths[0] != "example.md" {
		t.Fatalf("unexpected result: %+v", res)
	}
	mf, err := store.ReadFile("p1", "example.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mf.Body, "Useful memory") {
		t.Fatalf("unexpected file body: %q", mf.Body)
	}
}

func newServiceMemoryStore(t *testing.T) *memory.FileStore {
	t.Helper()
	resolver, err := memory.NewPathResolver("", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := resolver.SetProjectDirOverride("p1", filepath.Join(t.TempDir(), ".openvibely", "memory")); err != nil {
		t.Fatal(err)
	}
	return memory.NewFileStore(resolver)
}
