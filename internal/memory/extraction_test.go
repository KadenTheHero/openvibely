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
	in := Interaction{UserText: "User wants memory consolidation on Schedule page."}
	if got := ShouldExtract(true, in); got != "" {
		t.Fatalf("expected eligible, got %q", got)
	}
}
