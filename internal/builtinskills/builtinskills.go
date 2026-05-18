package builtinskills

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// FS contains the bundled lifecycle system-agent skills and the default
// AGENTS.md / SKILLS.md index files. Startup syncs this tree into the
// configured global skills root so the normal filesystem catalog path
// discovers the built-ins exactly like user-created skills.
//
//go:embed builtin/agents/AGENTS.md
//go:embed builtin/agents/*/SKILLS.md
//go:embed builtin/agents/*/skills/*/SKILL.md
var FS embed.FS

// SyncTo writes the embedded built-in files under root.
//
// SKILL.md bodies are always overwritten: those are the app's source of
// truth and travel with the binary. AGENTS.md and user-managed per-agent
// SKILLS.md files are only written when missing. The protected built-in
// skill_curator root declaration is also overwritten because it carries system
// policy and permission grants, not user narrative.
func SyncTo(root string) error {
	if root == "" {
		return nil
	}
	return fs.WalkDir(FS, "builtin", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel("builtin", path)
		if err != nil {
			return err
		}
		data, err := FS.ReadFile(path)
		if err != nil {
			return err
		}
		dst := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", filepath.Dir(dst), err)
		}
		base := filepath.Base(dst)
		isIndex := base == "AGENTS.md" || base == "SKILLS.md"
		isProtectedSystemDeclaration := filepath.ToSlash(rel) == "agents/skill_curator/SKILLS.md"
		if isIndex && !isProtectedSystemDeclaration {
			if _, err := os.Stat(dst); err == nil {
				// User-managed once it exists: don't clobber hand edits.
				return nil
			} else if !os.IsNotExist(err) {
				return fmt.Errorf("stat %s: %w", dst, err)
			}
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", dst, err)
		}
		return nil
	})
}
