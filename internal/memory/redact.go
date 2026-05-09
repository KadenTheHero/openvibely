package memory

import (
	"regexp"
	"strings"
)

// redactionPatterns matches common secret-like values in extracted memory text.
// These are intentionally conservative — better to over-redact than to leak.
var redactionPatterns = []*regexp.Regexp{
	// Generic API keys / tokens (key=value, key: value forms).
	regexp.MustCompile(`(?i)\b(api[_-]?key|secret|token|password|passwd|bearer)\s*[:=]\s*["']?[A-Za-z0-9_\-./+=]{12,}["']?`),
	// AWS access key id / secret access key.
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	regexp.MustCompile(`(?i)aws_secret_access_key\s*[:=]\s*[A-Za-z0-9/+=]{20,}`),
	// GitHub PATs (classic + fine-grained).
	regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{20,}`),
	// OpenAI API keys.
	regexp.MustCompile(`sk-[A-Za-z0-9_\-]{20,}`),
	// Anthropic API keys.
	regexp.MustCompile(`sk-ant-[A-Za-z0-9_\-]{20,}`),
	// Slack tokens.
	regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`),
	// JWT-like.
	regexp.MustCompile(`\beyJ[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+\b`),
	// PEM-style private keys.
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]+?-----END [A-Z ]*PRIVATE KEY-----`),
}

// Redact returns a copy of input with likely secrets replaced by "[REDACTED]".
// It is intentionally lossy: callers must not rely on round-tripping the
// redacted text. Memory writers run all extraction output through Redact
// before persisting.
func Redact(input string) string {
	if input == "" {
		return ""
	}
	out := input
	for _, re := range redactionPatterns {
		out = re.ReplaceAllString(out, "[REDACTED]")
	}
	return out
}

// LooksDominatedBySecrets returns true when, after redaction, the input is
// mostly redaction markers or leftover whitespace. This is used to skip
// extraction runs whose source content collapses to nothing useful.
func LooksDominatedBySecrets(input string) bool {
	if strings.TrimSpace(input) == "" {
		return false
	}
	redacted := Redact(input)
	// If 60% of the surviving non-whitespace text is the [REDACTED] marker,
	// treat the interaction as secret-dominated.
	stripped := strings.Join(strings.Fields(redacted), " ")
	if stripped == "" {
		return true
	}
	count := strings.Count(stripped, "[REDACTED]")
	if count == 0 {
		return false
	}
	markerLen := count * len("[REDACTED]")
	return float64(markerLen)/float64(len(stripped)) >= 0.6
}
