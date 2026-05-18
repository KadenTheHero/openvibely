package agentskills

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	llmcontracts "github.com/openvibely/openvibely/internal/llm/contracts"
)

// handleShape matches the canonical standalone skill handle form. We deliberately
// reject paths, parents, agent prefixes, and trailing /SKILL.md.
var handleShape = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// agentKeyShape matches the slug constraints we apply elsewhere. agent_view
// rejects keys that don't match before calling the inspector.
var agentKeyShape = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// AgentDetails is the prompt-safe agent record returned by agent_view.
type AgentDetails struct {
	Key             string          `json:"key"`
	Name            string          `json:"name,omitempty"`
	Description     string          `json:"description,omitempty"`
	SystemPrompt    string          `json:"system_prompt,omitempty"`
	Scope           string          `json:"scope,omitempty"`
	Enabled         bool            `json:"enabled"`
	Selectable      bool            `json:"selectable_as_primary,omitempty"`
	ToolGrants      []string        `json:"tool_grants,omitempty"`
	Permissions     map[string]any  `json:"permissions,omitempty"`
	ModelDefaults   map[string]any  `json:"model_defaults,omitempty"`
	Hooks           []AgentHookView `json:"hooks,omitempty"`
	AttachedSkills  []string        `json:"attached_skills,omitempty"`
	GeneratedStatus string          `json:"generated_status,omitempty"`
}

type AgentHookView struct {
	When           string `json:"when"`
	SkillKey       string `json:"skill_key"`
	OutputContract string `json:"output_contract,omitempty"`
	Blocking       bool   `json:"blocking,omitempty"`
	Enabled        bool   `json:"enabled"`
}

type AgentInspector interface {
	InspectAgent(ctx context.Context, agentKey string) (*AgentDetails, error)
}

// SkillRuntimeTools returns request-scoped runtime tools lifecycle hooks use for
// read-only standalone skill discovery.
func SkillRuntimeTools(catalog *Catalog, globalRoot, projectRoot string, inspector AgentInspector) *llmcontracts.RuntimeTools {
	return skillRuntimeTools(catalog, globalRoot, projectRoot, inspector, true)
}

// SelectedSkillRuntimeTools returns only skill_view scoped to the already
// selected task skill catalog. It intentionally omits skills_list so normal task
// turns cannot discover or load skills the lifecycle router did not select.
func SelectedSkillRuntimeTools(catalog *Catalog) *llmcontracts.RuntimeTools {
	return skillRuntimeTools(catalog, "", "", nil, false)
}

func skillRuntimeTools(catalog *Catalog, globalRoot, projectRoot string, inspector AgentInspector, includeLibraryTools bool) *llmcontracts.RuntimeTools {
	if catalog == nil {
		return nil
	}
	skillView := llmcontracts.RuntimeToolDefinition{
		Name:        "skill_view",
		Description: "Load a skill selected for this turn. With only handle, returns SKILL.md plus linked support-file metadata and absolute skill_dir/scripts_dir paths. With file_path, loads one support file under references/, templates/, scripts/, or assets/. Filesystem paths and agent-prefixed handles are not accepted; use the exact handle listed in <selected_skills>.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"handle":{"type":"string","description":"Selected skill handle, e.g. debug_go_tests or maintain_skill_library"},"file_path":{"type":"string","description":"Optional support file path, e.g. references/common-failures.md or scripts/check.sh"}},"required":["handle"],"additionalProperties":false}`),
	}
	definitions := []llmcontracts.RuntimeToolDefinition{skillView}
	tools := map[string]struct{}{"skill_view": {}}
	if includeLibraryTools {
		skillsList := llmcontracts.RuntimeToolDefinition{
			Name:        "skills_list",
			Description: "Return the raw top-level skills/SKILLS.md contents. Optional 'scope' ('global'|'project'|'all', default 'all') filters which roots to include.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string","enum":["global","project","all"]}},"additionalProperties":false}`),
		}
		definitions = append(definitions, skillsList)
		tools["skills_list"] = struct{}{}
		if inspector != nil {
			agentView := llmcontracts.RuntimeToolDefinition{
				Name:        "agent_view",
				Description: "Inspect one manually assigned agent: prompt, permissions, tool grants, lifecycle hooks, and attached/manual skills. Accepts an agent key/slug.",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"key":{"type":"string","description":"Agent key/slug to inspect"}},"required":["key"],"additionalProperties":false}`),
			}
			definitions = append(definitions, agentView)
			tools["agent_view"] = struct{}{}
		}
	}
	isMine := func(name string) bool {
		_, ok := tools[strings.ToLower(strings.TrimSpace(name))]
		return ok
	}
	return &llmcontracts.RuntimeTools{
		Definitions: definitions,
		Executor: func(ctx context.Context, name string, input json.RawMessage) (string, bool, bool, error) {
			switch strings.ToLower(strings.TrimSpace(name)) {
			case "skill_view":
				body, err := resolveSkillView(catalog, input)
				if err != nil {
					return err.Error(), true, true, nil
				}
				return body, true, false, nil
			case "skills_list":
				if !includeLibraryTools {
					break
				}
				body, err := resolveSkillsList(globalRoot, projectRoot, input)
				if err != nil {
					return err.Error(), true, true, nil
				}
				return body, true, false, nil
			case "agent_view":
				if !includeLibraryTools {
					break
				}
				if inspector == nil {
					return "agent_view: no inspector configured", true, true, nil
				}
				body, err := resolveAgentView(ctx, inspector, input)
				if err != nil {
					return err.Error(), true, true, nil
				}
				return body, true, false, nil
			}
			return "", false, false, nil
		},
		Filter: func(toolName string) (bool, bool) {
			if isMine(toolName) {
				return true, true
			}
			return false, false
		},
	}
}

func resolveSkillsList(globalRoot, projectRoot string, input json.RawMessage) (string, error) {
	scope := parseScope(input)
	var out strings.Builder
	hits := 0
	if (scope == "global" || scope == "all") && globalRoot != "" {
		if body, ok := readIfExists(SkillsIndexPath(globalRoot)); ok {
			fmt.Fprintf(&out, "=== global ===\n%s\n", strings.TrimRight(body, "\n"))
			hits++
		}
	}
	if (scope == "project" || scope == "all") && projectRoot != "" {
		if body, ok := readIfExists(SkillsIndexPath(projectRoot)); ok {
			if hits > 0 {
				out.WriteString("\n")
			}
			fmt.Fprintf(&out, "=== project ===\n%s\n", strings.TrimRight(body, "\n"))
			hits++
		}
	}
	if hits == 0 {
		return "(no top-level skills/SKILLS.md found for the requested scope)", nil
	}
	return out.String(), nil
}

func parseScope(input json.RawMessage) string {
	var params struct {
		Scope string `json:"scope"`
	}
	if len(input) > 0 {
		_ = json.Unmarshal(input, &params)
	}
	scope := strings.ToLower(strings.TrimSpace(params.Scope))
	if scope == "" {
		scope = "all"
	}
	switch scope {
	case "global", "project", "all":
		return scope
	default:
		return "all"
	}
}

func resolveSkillView(catalog *Catalog, input json.RawMessage) (string, error) {
	var params struct {
		Handle   string `json:"handle"`
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("skill_view: invalid input: %w", err)
	}
	handle := strings.TrimSpace(params.Handle)
	if !handleShape.MatchString(handle) {
		return "", fmt.Errorf("skill_view: handle %q is not a valid standalone skill handle", handle)
	}
	entry, ok := catalog.Lookup(handle)
	if !ok {
		return "", fmt.Errorf("skill_view: handle %q is not in this turn's authorized index", handle)
	}
	if strings.TrimSpace(params.FilePath) != "" {
		return resolveSkillSupportFile(entry, params.FilePath)
	}
	body, err := os.ReadFile(entry.AbsolutePath)
	if err != nil {
		return "", fmt.Errorf("skill_view: read %q: %w", handle, err)
	}
	skillDir := filepath.Dir(entry.AbsolutePath)
	view := skillViewResponse{
		Handle:      handle,
		Source:      string(entry.Source),
		AgentKey:    entry.AgentKey,
		Body:        substituteSkillDirTokens(string(body), skillDir),
		SkillDir:    skillDir,
		ScriptsDir:  filepath.Join(skillDir, "scripts"),
		LinkedFiles: listLinkedSupportFiles(skillDir),
	}
	encoded, err := json.MarshalIndent(view, "", "  ")
	if err != nil {
		return "", fmt.Errorf("skill_view: encode %q: %w", handle, err)
	}
	return string(encoded), nil
}

type skillViewResponse struct {
	Handle      string              `json:"handle"`
	Source      string              `json:"source,omitempty"`
	AgentKey    string              `json:"agent_key,omitempty"`
	Body        string              `json:"body"`
	SkillDir    string              `json:"skill_dir"`
	ScriptsDir  string              `json:"scripts_dir"`
	LinkedFiles map[string][]string `json:"linked_files,omitempty"`
}

func substituteSkillDirTokens(body, skillDir string) string {
	if skillDir == "" || body == "" {
		return body
	}
	body = strings.ReplaceAll(body, "${OPENVIBELY_SKILL_DIR}", skillDir)
	body = strings.ReplaceAll(body, "$OPENVIBELY_SKILL_DIR", skillDir)
	return body
}

type skillSupportFileResponse struct {
	Handle   string `json:"handle"`
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
}

func resolveSkillSupportFile(entry Entry, requested string) (string, error) {
	rel, err := cleanSupportFilePath(requested)
	if err != nil {
		return "", fmt.Errorf("skill_view: %w", err)
	}
	skillDir := filepath.Dir(entry.AbsolutePath)
	abs := filepath.Join(skillDir, rel)
	if err := ensureInsideSkillDir(skillDir, abs); err != nil {
		return "", fmt.Errorf("skill_view: %w", err)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", fmt.Errorf("skill_view: read support file %q: %w", rel, err)
	}
	encoded, err := json.MarshalIndent(skillSupportFileResponse{Handle: entry.Handle, FilePath: rel, Content: string(data)}, "", "  ")
	if err != nil {
		return "", fmt.Errorf("skill_view: encode support file %q: %w", rel, err)
	}
	return string(encoded), nil
}

func listLinkedSupportFiles(skillDir string) map[string][]string {
	out := map[string][]string{}
	for _, kind := range []string{"references", "templates", "scripts", "assets"} {
		root := filepath.Join(skillDir, kind)
		var files []string
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return nil
			}
			rel = filepath.ToSlash(rel)
			if _, err := cleanSupportFilePath(kind + "/" + rel); err != nil {
				return nil
			}
			files = append(files, rel)
			return nil
		})
		if len(files) > 0 {
			sort.Strings(files)
			out[kind] = files
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func cleanSupportFilePath(requested string) (string, error) {
	req := filepath.ToSlash(strings.TrimSpace(requested))
	if req == "" || strings.HasPrefix(req, "/") || strings.Contains(req, "\\") {
		return "", fmt.Errorf("invalid support file path %q", requested)
	}
	clean := filepath.Clean(req)
	clean = filepath.ToSlash(clean)
	if clean == "." || strings.HasPrefix(clean, "../") || clean == ".." || strings.Contains(clean, "/../") {
		return "", fmt.Errorf("invalid support file path %q", requested)
	}
	parts := strings.Split(clean, "/")
	if len(parts) < 2 || !validSupportDir(parts[0]) {
		return "", fmt.Errorf("support file path %q must start with references/, templates/, scripts/, or assets/", requested)
	}
	for _, part := range parts[1:] {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("invalid support file path %q", requested)
		}
	}
	return clean, nil
}

func validSupportDir(kind string) bool {
	switch kind {
	case "references", "templates", "scripts", "assets":
		return true
	default:
		return false
	}
}

func ensureInsideSkillDir(skillDir, abs string) error {
	skillDirAbs, err := filepath.Abs(skillDir)
	if err != nil {
		return err
	}
	absResolved, err := filepath.Abs(abs)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(absResolved, skillDirAbs+string(filepath.Separator)) {
		return fmt.Errorf("support path escapes skill folder")
	}
	return nil
}

func resolveAgentView(ctx context.Context, inspector AgentInspector, input json.RawMessage) (string, error) {
	var params struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("agent_view: invalid input: %w", err)
	}
	key := strings.TrimSpace(params.Key)
	if !agentKeyShape.MatchString(key) {
		return "", fmt.Errorf("agent_view: key %q is not a valid slug", key)
	}
	details, err := inspector.InspectAgent(ctx, key)
	if err != nil {
		return "", fmt.Errorf("agent_view: %w", err)
	}
	if details == nil {
		return "", fmt.Errorf("agent_view: agent %q not found", key)
	}
	b, err := json.MarshalIndent(details, "", "  ")
	if err != nil {
		return "", fmt.Errorf("agent_view: encode: %w", err)
	}
	return string(b), nil
}

func readIfExists(path string) (string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(b), true
}
