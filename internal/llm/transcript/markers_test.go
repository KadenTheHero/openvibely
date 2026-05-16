package transcript

import (
	"strings"
	"testing"
)

func TestNormalizeMarkers_CanonicalizesLegacyThinkingAndMalformedToolInvoke(t *testing.T) {
	for _, opener := range []string{`[Using tool: bash"]`, `[Using tool: bash">`} {
		input := "[Thinking]\nreviewing\n</thinking>Normal text\n" + opener + "\n<parameter name=\"command\">echo one\n echo two</parameter>\n</invoke>done"
		got := NormalizeMarkers(input)

		if strings.Contains(got, "</thinking>") || strings.Contains(got, "<parameter") || strings.Contains(got, "</invoke>") || strings.Contains(got, `bash\"]`) || strings.Contains(got, `bash\">`) {
			t.Fatalf("expected legacy markers removed, got: %q", got)
		}
		if !strings.Contains(got, "[Thinking]\nreviewing\n\n[/Thinking]\nNormal text") {
			t.Fatalf("expected thinking close tag canonicalized before normal text, got: %q", got)
		}
		if !strings.Contains(got, "[Using tool: bash | echo one  echo two]") {
			t.Fatalf("expected malformed tool invocation canonicalized, got: %q", got)
		}
	}
}

func TestNormalizeMarkers_LeavesIncompleteMalformedToolInvokeForNextDelta(t *testing.T) {
	input := "before\n[Using tool: bash\">\n<parameter name=\"command\">go test ./internal/..."
	got := NormalizeMarkers(input)

	if got != input {
		t.Fatalf("expected incomplete malformed tool invocation left intact for future stream deltas, got: %q", got)
	}
}
