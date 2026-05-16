package transcript

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	reLegacyThinkingOpenTag  = regexp.MustCompile(`(?i)<\s*thinking\s*>`)
	reLegacyThinkingCloseTag = regexp.MustCompile(`(?i)</\s*thinking\s*>`)
	reMalformedToolInvoke    = regexp.MustCompile(`(?s)\[Using tool:\s*([A-Za-z0-9_.-]+)"(?:\]|>)\s*<parameter\s+name="command">(.*?)</parameter>\s*</invoke>\s*`)
)

// NormalizeMarkers canonicalizes provider/client marker variants before
// persistence or rendering. Historical rows may contain XML-ish thinking/tool tags
// produced by older stream parsers; converting them here keeps thinking and normal
// assistant content in separate renderable sections without changing the message text.
func NormalizeMarkers(text string) string {
	if text == "" {
		return text
	}
	text = reMalformedToolInvoke.ReplaceAllStringFunc(text, func(match string) string {
		parts := reMalformedToolInvoke.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}
		tool := sanitizeMarkerPart(parts[1])
		cmd := sanitizeMarkerPart(parts[2])
		if tool == "" {
			return ""
		}
		if cmd == "" {
			return fmt.Sprintf("\n[Using tool: %s]\n", tool)
		}
		return fmt.Sprintf("\n[Using tool: %s | %s]\n", tool, cmd)
	})
	text = reLegacyThinkingOpenTag.ReplaceAllString(text, "\n[Thinking]\n")
	text = reLegacyThinkingCloseTag.ReplaceAllString(text, "\n[/Thinking]\n")
	return text
}

func sanitizeMarkerPart(value string) string {
	if value == "" {
		return ""
	}
	value = strings.ReplaceAll(value, "]", ")")
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	return strings.TrimSpace(value)
}
