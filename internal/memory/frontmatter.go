package memory

import (
	"fmt"
	"strings"
)

// ParseFrontmatter splits a memory markdown file into its YAML frontmatter
// metadata and the remaining body. If the file has no frontmatter, an empty
// FileMeta is returned along with the entire file as body.
func ParseFrontmatter(content string) (FileMeta, string) {
	meta := FileMeta{}
	if !strings.HasPrefix(content, "---") {
		return meta, content
	}
	rest := strings.TrimPrefix(content, "---")
	rest = strings.TrimPrefix(rest, "\r")
	rest = strings.TrimPrefix(rest, "\n")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return meta, content
	}
	header := rest[:end]
	body := rest[end+len("\n---"):]
	body = strings.TrimPrefix(body, "\r")
	body = strings.TrimPrefix(body, "\n")
	for _, line := range strings.Split(header, "\n") {
		line = strings.TrimRight(line, "\r")
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		val = strings.Trim(val, `"'`)
		switch key {
		case "name":
			meta.Name = val
		case "type":
			meta.Type = MemoryType(val)
		case "created":
			meta.Created = val
		case "updated":
			meta.Updated = val
		case "source":
			meta.Source = val
		case "source_id":
			meta.SourceID = val
		case "confidence":
			meta.Confidence = val
		case "title":
			meta.Title = val
		}
	}
	return meta, body
}

// RenderFrontmatter renders a YAML frontmatter block (terminated by "---\n\n")
// for a memory file. Empty fields are omitted.
func RenderFrontmatter(meta FileMeta) string {
	var b strings.Builder
	b.WriteString("---\n")
	writeKV := func(k, v string) {
		if v == "" {
			return
		}
		fmt.Fprintf(&b, "%s: %s\n", k, v)
	}
	writeKV("name", meta.Name)
	writeKV("type", string(meta.Type))
	writeKV("created", meta.Created)
	writeKV("updated", meta.Updated)
	writeKV("source", meta.Source)
	writeKV("source_id", meta.SourceID)
	writeKV("confidence", meta.Confidence)
	writeKV("title", meta.Title)
	b.WriteString("---\n\n")
	return b.String()
}
