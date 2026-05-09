package memory

import (
	"strings"
	"testing"
)

func TestRedact_CommonSecretShapes(t *testing.T) {
	cases := map[string]string{
		"OpenAI key":      "Use sk-AAAAAAAAAAAAAAAAAAAAAAAA in tests",
		"Anthropic key":   "ANTHROPIC=sk-ant-AAAAAAAAAAAAAAAAAAAAAAAA",
		"GitHub PAT":      "auth ghp_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA next",
		"AWS access key":  "AKIAABCDEFGHIJKLMNOP go",
		"key=value style": "API_KEY: sk_test_AAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"Slack token":     "xoxb-1234567890-abcdef-aBcDeF123",
		"JWT":             "Bearer eyJhbGci.eyJzdWIi.signaturePart",
		"PEM private key": "-----BEGIN RSA PRIVATE KEY-----\nMIIB...AB\n-----END RSA PRIVATE KEY-----",
	}
	for label, in := range cases {
		out := Redact(in)
		if !strings.Contains(out, "[REDACTED]") {
			t.Errorf("%s: expected redaction, got %q", label, out)
		}
	}
}

func TestRedact_LeavesNormalTextAlone(t *testing.T) {
	in := "User wants to rename memory dream to consolidation."
	if Redact(in) != in {
		t.Fatalf("redaction altered benign text: %q", Redact(in))
	}
}

func TestLooksDominatedBySecrets(t *testing.T) {
	if LooksDominatedBySecrets("hello world") {
		t.Fatal("benign text flagged as secret-dominated")
	}
	heavy := strings.Repeat("sk-AAAAAAAAAAAAAAAAAAAAAA ", 5)
	if !LooksDominatedBySecrets(heavy) {
		t.Fatal("expected secret-dominated to trip")
	}
}
