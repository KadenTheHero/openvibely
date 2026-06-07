package agentskills

import (
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// SkillsIndexMeta holds optional top-level metadata for a standalone SKILLS.md.
// It lives in the YAML frontmatter block at the top of the file. Only add this
// block when non-default metadata exists; omitting it keeps skill index entries
// clean. Per-skill always_use preferences belong here rather than in SKILL.md
// so individual skill files are not cluttered with routing policy.
type SkillsIndexMeta struct {
	AlwaysUse []string `yaml:"always_use,omitempty"`
}

// IsEmpty reports whether the meta block has any non-default (non-zero) values.
// Used to decide whether to write or strip a frontmatter block.
func (m *SkillsIndexMeta) IsEmpty() bool {
	return m == nil || len(m.AlwaysUse) == 0
}

// ParseSkillsIndexMeta extracts and parses the YAML frontmatter block from a
// SKILLS.md content string. A missing, malformed, or empty block returns a zero
// SkillsIndexMeta rather than an error, since SKILLS.md files are commonly
// frontmatter-free.
func ParseSkillsIndexMeta(content string) SkillsIndexMeta {
	if !strings.HasPrefix(content, "---") {
		return SkillsIndexMeta{}
	}
	rest := strings.TrimPrefix(content, "---")
	rest = strings.TrimPrefix(rest, "\r")
	rest = strings.TrimPrefix(rest, "\n")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return SkillsIndexMeta{}
	}
	front := rest[:end]
	var meta SkillsIndexMeta
	if err := yaml.Unmarshal([]byte(front), &meta); err != nil {
		return SkillsIndexMeta{}
	}
	// Normalize and de-duplicate handles; drop invalid slugs silently.
	seen := make(map[string]struct{}, len(meta.AlwaysUse))
	cleaned := make([]string, 0, len(meta.AlwaysUse))
	for _, handle := range meta.AlwaysUse {
		handle = strings.TrimSpace(handle)
		if handle == "" || !isValidSlug(handle) {
			continue
		}
		if _, ok := seen[handle]; ok {
			continue
		}
		seen[handle] = struct{}{}
		cleaned = append(cleaned, handle)
	}
	meta.AlwaysUse = cleaned
	return meta
}

// ReadSkillsIndexMeta reads SKILLS.md at path and returns its frontmatter
// metadata. Missing or unreadable files return a zero SkillsIndexMeta.
func ReadSkillsIndexMeta(path string) SkillsIndexMeta {
	data, err := os.ReadFile(path)
	if err != nil {
		return SkillsIndexMeta{}
	}
	return ParseSkillsIndexMeta(string(data))
}

// AlwaysUseHandlesFromRoots returns the deduplicated always_use handles from the
// SKILLS.md frontmatter of globalRoot and projectRoot. Project entries are
// appended after global entries; duplicates are removed keeping first occurrence.
// Handles are NOT filtered for enabled/disabled here; callers must verify against
// the runtime catalog to exclude disabled skills.
func AlwaysUseHandlesFromRoots(globalRoot, projectRoot string) []string {
	var raw []string
	if globalRoot != "" {
		meta := ReadSkillsIndexMeta(SkillsIndexPath(globalRoot))
		raw = append(raw, meta.AlwaysUse...)
	}
	if projectRoot != "" {
		meta := ReadSkillsIndexMeta(SkillsIndexPath(projectRoot))
		raw = append(raw, meta.AlwaysUse...)
	}
	if len(raw) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, h := range raw {
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		out = append(out, h)
	}
	return out
}

// SkillSelectionProvenance maps each selected skill handle to the source that
// caused it to be included for the current turn.
type SkillSelectionProvenance map[string]string

const (
	// ProvenanceAlwaysUse marks handles forced-included by the always_use index list.
	ProvenanceAlwaysUse = "always_use"
	// ProvenanceSkillCurator marks handles selected by the Skill Curator route hook.
	ProvenanceSkillCurator = "skill_curator"
	// ProvenanceBoth marks handles that appear in both always_use and skill_curator selection.
	ProvenanceBoth = "always_use,skill_curator"
)

// MergeAlwaysUseIntoSelected merges always-use handles from the SKILLS.md
// frontmatter of globalRoot and projectRoot into selectedHandles, verifying each
// against catalog so that disabled or unindexed skills are excluded.
//
// Returns the merged, deduplicated handle slice and a provenance map. This must
// only be called for standalone (non-agent-owned) task catalogs; assigned-agent
// tasks use their agent's skill catalog and must not be polluted by top-level
// standalone always-use settings.
func MergeAlwaysUseIntoSelected(catalog *Catalog, globalRoot, projectRoot string, selectedHandles []string) ([]string, SkillSelectionProvenance) {
	provenance := make(SkillSelectionProvenance, len(selectedHandles))

	// Seed provenance from skill_curator-selected handles.
	curatorSet := make(map[string]struct{}, len(selectedHandles))
	for _, h := range selectedHandles {
		curatorSet[h] = struct{}{}
		provenance[h] = ProvenanceSkillCurator
	}

	// Collect always-use handles from index frontmatter.
	alwaysUse := AlwaysUseHandlesFromRoots(globalRoot, projectRoot)
	if len(alwaysUse) == 0 {
		return selectedHandles, provenance
	}

	merged := append([]string(nil), selectedHandles...)
	seen := make(map[string]struct{}, len(merged))
	for _, h := range merged {
		seen[h] = struct{}{}
	}

	for _, h := range alwaysUse {
		// Verify handle exists in the runtime catalog (excludes disabled/unindexed skills).
		if _, ok := catalog.Lookup(h); !ok {
			continue
		}
		if _, inCurator := curatorSet[h]; inCurator {
			// Handle was already selected by skill_curator; note combined provenance.
			provenance[h] = ProvenanceBoth
			continue
		}
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		merged = append(merged, h)
		provenance[h] = ProvenanceAlwaysUse
	}

	return merged, provenance
}
