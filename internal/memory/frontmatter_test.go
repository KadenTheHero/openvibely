package memory

import (
	"strings"
	"testing"
)

func TestFrontmatterRoundTrip(t *testing.T) {
	meta := FileMeta{
		Name:       "naming_preferences",
		Type:       TypeUser,
		Created:    "2026-05-09",
		Updated:    "2026-05-09",
		Source:     "chat",
		SourceID:   "exec-123",
		Confidence: "high",
		Title:      "Naming preferences",
	}
	body := "User dislikes dream terminology for memory features.\n"
	rendered := RenderFrontmatter(meta) + body
	gotMeta, gotBody := ParseFrontmatter(rendered)
	if gotMeta != meta {
		t.Fatalf("meta mismatch: got=%+v want=%+v", gotMeta, meta)
	}
	if strings.TrimSpace(gotBody) != strings.TrimSpace(body) {
		t.Fatalf("body mismatch: got=%q want=%q", gotBody, body)
	}
}

func TestParseFrontmatter_NoFrontmatter(t *testing.T) {
	body := "Just a body without frontmatter."
	meta, gotBody := ParseFrontmatter(body)
	if meta != (FileMeta{}) {
		t.Fatalf("expected empty meta, got %+v", meta)
	}
	if gotBody != body {
		t.Fatalf("body should pass through unchanged")
	}
}

func TestRenderFrontmatter_OmitsEmpty(t *testing.T) {
	out := RenderFrontmatter(FileMeta{Name: "x", Type: TypeProject})
	if strings.Contains(out, "source:") {
		t.Fatalf("empty source should be omitted: %s", out)
	}
}
