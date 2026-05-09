package memory

import (
	"strings"
	"testing"
)

func TestContextBuilder_DisabledReturnsEmpty(t *testing.T) {
	s := newStore(t)
	cb := NewContextBuilder(s)
	out, err := cb.Build("p1", false, ContextOptions{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if out != "" {
		t.Fatalf("expected empty when disabled, got %q", out)
	}
}

func TestContextBuilder_IsolatesProjects(t *testing.T) {
	s := newStore(t)
	if _, err := s.EnsureProject("a"); err != nil {
		t.Fatalf("ensure a: %v", err)
	}
	if _, err := s.EnsureProject("b"); err != nil {
		t.Fatalf("ensure b: %v", err)
	}
	if err := s.WriteFile("a", "naming.md", FileMeta{Name: "naming", Type: TypeUser}, "PROJ A SECRET"); err != nil {
		t.Fatalf("write: %v", err)
	}
	cb := NewContextBuilder(s)
	out, err := cb.Build("b", true, ContextOptions{IncludeIndex: true})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if strings.Contains(out, "PROJ A SECRET") {
		t.Fatalf("memory leaked across projects: %q", out)
	}
}

func TestContextBuilder_PrioritizesFeedbackAndUser(t *testing.T) {
	s := newStore(t)
	_, _ = s.EnsureProject("p1")
	if err := s.WriteFile("p1", "big.md", FileMeta{Name: "big", Type: TypeProject}, "Project body content here"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := s.WriteFile("p1", "feed.md", FileMeta{Name: "feed", Type: TypeFeedback}, "Feedback body content here"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := s.WriteFile("p1", "me.md", FileMeta{Name: "me", Type: TypeUser}, "User body content here"); err != nil {
		t.Fatalf("write: %v", err)
	}
	cb := NewContextBuilder(s)
	out, err := cb.Build("p1", true, ContextOptions{MaxFiles: 2, IncludeIndex: false})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !strings.Contains(out, "Feedback body") {
		t.Fatalf("feedback should be included first: %s", out)
	}
	if !strings.Contains(out, "User body") {
		t.Fatalf("user should be included second: %s", out)
	}
	if strings.Contains(out, "Project body") {
		t.Fatalf("MaxFiles=2 should exclude project: %s", out)
	}
}

func TestContextBuilder_HonoursMaxBytes(t *testing.T) {
	s := newStore(t)
	_, _ = s.EnsureProject("p1")
	big := strings.Repeat("AAAA ", 4000) // ~20kb
	if err := s.WriteFile("p1", "a.md", FileMeta{Name: "a", Type: TypeProject}, big); err != nil {
		t.Fatalf("write: %v", err)
	}
	cb := NewContextBuilder(s)
	out, err := cb.Build("p1", true, ContextOptions{MaxFiles: 5, MaxBytes: 256})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(out) > 1024 {
		t.Fatalf("MaxBytes not honored: out=%d bytes", len(out))
	}
}
