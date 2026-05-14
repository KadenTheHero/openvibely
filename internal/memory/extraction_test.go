package memory

import (
	"strings"
	"testing"
)

func TestShouldExtract_DisabledShortCircuits(t *testing.T) {
	if got := ShouldExtract(false, Interaction{UserText: "long enough text here"}); got != SkipDisabled {
		t.Fatalf("expected SkipDisabled, got %q", got)
	}
}

func TestShouldExtract_CancelledShortCircuits(t *testing.T) {
	if got := ShouldExtract(true, Interaction{Cancelled: true, UserText: "long enough text here"}); got != SkipCancelled {
		t.Fatalf("expected SkipCancelled, got %q", got)
	}
}

func TestShouldExtract_EmptyMeansSkip(t *testing.T) {
	if got := ShouldExtract(true, Interaction{UserText: "ok"}); got != SkipEmpty {
		t.Fatalf("expected SkipEmpty, got %q", got)
	}
}

func TestShouldExtract_SecretDominated(t *testing.T) {
	in := Interaction{UserText: strings.Repeat("sk-AAAAAAAAAAAAAAAAAAAA ", 6)}
	if got := ShouldExtract(true, in); got != SkipSecretDominated {
		t.Fatalf("expected SkipSecretDominated, got %q", got)
	}
}

func TestShouldExtract_EligibleInteractionPasses(t *testing.T) {
	in := Interaction{SourceKind: SourceTask, UserText: "User wants memory consolidation on Schedule page."}
	if got := ShouldExtract(true, in); got != "" {
		t.Fatalf("expected eligible, got %q", got)
	}
}

// TestShouldExtract_ChatSurfaceAlwaysSkipped guarantees that Chat page prompts
// (interactive /chat and /api/chat/message) are excluded from memory
// extraction even when the prose looks substantive and contains no secrets.
// Chat carries transient orchestration/mode-control text (Orchestrate/Plan,
// "Switch to Orchestrate", <proposed_plan>, etc.) that must never end up in
// durable project memory files.
func TestShouldExtract_ChatSurfaceAlwaysSkipped(t *testing.T) {
	chatLikePrompts := []string{
		"Plan: refactor the providers package end-to-end.",
		"Switch to Orchestrate and run the implementation.",
		"<proposed_plan>\nStep 1: do X\nStep 2: do Y\n</proposed_plan>",
		"Use orchestrate mode to dispatch follow-up tasks.",
		"Hey can you summarize the codebase architecture for me?",
	}
	for _, kind := range []SourceKind{SourceChat, SourceAPIChat} {
		for _, prompt := range chatLikePrompts {
			in := Interaction{SourceKind: kind, UserText: prompt}
			if got := ShouldExtract(true, in); got != SkipChatSurface {
				t.Fatalf("kind=%s prompt=%q: expected SkipChatSurface, got %q", kind, prompt, got)
			}
		}
	}
}

// TestShouldExtract_TaskAndThreadStillEligible ensures the chat-surface
// exclusion does not regress legitimate task/thread extraction inputs.
func TestShouldExtract_TaskAndThreadStillEligible(t *testing.T) {
	for _, kind := range []SourceKind{SourceTask, SourceThread, SourceTelegram, SourceSlack, SourceWebhook} {
		in := Interaction{SourceKind: kind, UserText: "Implement chained task cancellation in worker_service.go."}
		if got := ShouldExtract(true, in); got != "" {
			t.Fatalf("kind=%s: expected eligible, got %q", kind, got)
		}
	}
}

func TestIsChatSurface(t *testing.T) {
	for _, kind := range []SourceKind{SourceChat, SourceAPIChat} {
		if !IsChatSurface(kind) {
			t.Fatalf("%s should be a chat surface", kind)
		}
	}
	for _, kind := range []SourceKind{SourceTask, SourceThread, SourceTelegram, SourceSlack, SourceWebhook} {
		if IsChatSurface(kind) {
			t.Fatalf("%s should not be a chat surface", kind)
		}
	}
}
