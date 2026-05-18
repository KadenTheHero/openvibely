package agentskills

import (
	"fmt"
	"os"
	"strings"
)

// RenderAvailableSkillsMarkdown returns the literal contents of top-level
// skills/SKILLS.md index files (global + project), wrapped with a short
// instruction header and fenced inside <available_skills>...</available_skills>.
// The route hook reads this block to choose relevant standalone skills; normal
// task turns receive only the selected subset rendered by
// RenderSelectedSkillsMarkdown.
func RenderAvailableSkillsMarkdown(globalRoot, projectRoot string) string {
	var sb strings.Builder
	sb.WriteString("## Available Standalone Skills\n\n")
	sb.WriteString("Review the standalone skill index below and select skill handles relevant to the user prompt.\n")
	sb.WriteString("Agents are manually assigned to tasks; do not choose or switch agents. When a\n")
	sb.WriteString("listed standalone skill is relevant, return its skill handle, for example `debug_go_tests`.\n")
	sb.WriteString("Use `skills_list` or `skill_view` only to inspect available skills. Use `agent_view` only to understand a manually assigned agent.\n\n")
	sb.WriteString("<available_skills>\n")

	wrote := false
	if section := readStandaloneSkillsSection(globalRoot, "global"); section != "" {
		sb.WriteString(section)
		wrote = true
	}
	if section := readStandaloneSkillsSection(projectRoot, "project"); section != "" {
		if wrote {
			sb.WriteString("\n")
		}
		sb.WriteString(section)
		wrote = true
	}
	if !wrote {
		sb.WriteString("_No standalone skills indexed in this turn._\n")
	}
	sb.WriteString("</available_skills>\n")
	return sb.String()
}

// RenderAvailableAgentSkillsMarkdown returns the assigned agent's SKILLS.md
// index from global/project roots for router selection.
func RenderAvailableAgentSkillsMarkdown(globalRoot, projectRoot, agentKey string) string {
	var sb strings.Builder
	sb.WriteString("## Available Assigned-Agent Skills\n\n")
	fmt.Fprintf(&sb, "Review the skills owned by assigned agent `%s` below and select skill handles relevant to the user prompt.\n", strings.TrimSpace(agentKey))
	sb.WriteString("Do not choose or switch agents. When a listed assigned-agent skill is relevant, return only its skill key, for example `maintain_skill_library`, not `agent/skill`.\n")
	sb.WriteString("Use `skill_view` only to inspect skills listed for this assigned agent.\n\n")
	sb.WriteString("<available_skills>\n")

	wrote := false
	if section := readAgentSkillsSection(globalRoot, agentKey, "global"); section != "" {
		sb.WriteString(section)
		wrote = true
	}
	if section := readAgentSkillsSection(projectRoot, agentKey, "project"); section != "" {
		if wrote {
			sb.WriteString("\n")
		}
		sb.WriteString(section)
		wrote = true
	}
	if !wrote {
		fmt.Fprintf(&sb, "_No skills indexed for assigned agent `%s` in this turn._\n", strings.TrimSpace(agentKey))
	}
	sb.WriteString("</available_skills>\n")
	return sb.String()
}

// RenderSelectedSkillsMarkdown renders the exact catalog skill handles selected
// for the current task turn. The model may load these skills with skill_view;
// unrelated catalog handles are intentionally omitted from this prompt block.
func RenderSelectedSkillsMarkdown(catalog *Catalog, handles []string) string {
	if catalog == nil || len(handles) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Selected Skills For This Task\n\n")
	if catalog.IsAgentOwned() {
		sb.WriteString("The lifecycle router selected these assigned-agent skills for this turn. Load any needed full body with `skill_view(\"<skill>\")`.\n\n")
	} else {
		sb.WriteString("The task keeps its assigned/default agent. The lifecycle router selected these standalone skills for this turn. Load any needed full body with `skill_view(\"<skill>\")`.\n\n")
	}
	sb.WriteString("<selected_skills>\n")
	wrote := false
	seen := map[string]struct{}{}
	for _, handle := range handles {
		handle = strings.TrimSpace(handle)
		if handle == "" {
			continue
		}
		if _, ok := seen[handle]; ok {
			continue
		}
		entry, ok := catalog.Lookup(handle)
		if !ok {
			continue
		}
		seen[handle] = struct{}{}
		if entry.AgentKey != "" {
			fmt.Fprintf(&sb, "- `%s` (agent:%s)\n", entry.Handle, entry.AgentKey)
		} else {
			fmt.Fprintf(&sb, "- `%s` (%s)\n", entry.Handle, entry.Source)
		}
		wrote = true
	}
	if !wrote {
		return ""
	}
	sb.WriteString("</selected_skills>\n")
	return sb.String()
}

func readStandaloneSkillsSection(root, scope string) string {
	if root == "" {
		return ""
	}
	return readSkillsIndexSection(SkillsIndexPath(root), scope)
}

func readAgentSkillsSection(root, agentKey, scope string) string {
	if root == "" || strings.TrimSpace(agentKey) == "" {
		return ""
	}
	return readSkillsIndexSection(AgentSkillsIndexPath(root, strings.TrimSpace(agentKey)), scope)
}

func readSkillsIndexSection(path, scope string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "### Scope: %s\n\n", scope)
	sb.WriteString(strings.TrimSpace(string(data)))
	sb.WriteString("\n")
	return sb.String()
}
