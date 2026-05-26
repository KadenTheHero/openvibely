package agentlibrary

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	llmcontracts "github.com/openvibely/openvibely/internal/llm/contracts"
)

// MutationActor identifies who is invoking the mutation tools. Audit rows
// reference this for traceability.
type MutationActor struct {
	LifecycleExecutionID string
	TaskID               string
	TaskRunID            string
	ProjectID            string
	ActorAgentID         string
}

// MutationRecorder persists agent_config_mutations audit rows. The mutation
// tools always record both applied and blocked proposals so debugging can
// answer why an artifact did or did not change (runbook §Data Model line
// 2452 + §Backend Validation line 1773).
type MutationRecorder interface {
	Record(ctx context.Context, action string, target string, key string, payload []byte, result *ImportResult, blocked error) error
}

// MutationTools returns the request-scoped runtime tool for autonomous
// standalone skill maintenance. Agents are user-managed through the agent
// dialog, so this runtime surface intentionally exposes skill_manage only.
//
// Actor context (lifecycle execution, task, project, actor agent) should be
// baked into the recorder at construction time via NewRepoRecorder so every
// row this runtime tool inserts is correctly attributed.
func MutationTools(importer *Importer, recorder MutationRecorder) *llmcontracts.RuntimeTools {
	return SkillMutationTools(importer, recorder)
}

// AgentSkillMutationTools returns agent_skill_manage for after-complete learning
// hooks. The tool is server-scoped to the task's assigned agent; model input can
// choose only a skill key and action, never an arbitrary agent path.
func AgentSkillMutationTools(importer *Importer, recorder MutationRecorder, assignedAgentKey, scope string) *llmcontracts.RuntimeTools {
	if importer == nil || strings.TrimSpace(assignedAgentKey) == "" {
		return nil
	}
	exec := func(ctx context.Context, name string, input json.RawMessage) (string, bool, bool, error) {
		if strings.ToLower(strings.TrimSpace(name)) != "agent_skill_manage" {
			return "", false, false, nil
		}
		res, err := runAgentSkillManage(ctx, importer, recorder, assignedAgentKey, scope, input)
		return marshalToolResult(res, err)
	}
	return &llmcontracts.RuntimeTools{
		Definitions: []llmcontracts.RuntimeToolDefinition{{
			Name:        "agent_skill_manage",
			Description: agentSkillManageDescription(assignedAgentKey),
			Parameters:  json.RawMessage(agentSkillManageSchema),
		}},
		Executor: exec,
		Filter: func(name string) (bool, bool) {
			if strings.ToLower(strings.TrimSpace(name)) == "agent_skill_manage" {
				return true, true
			}
			return false, false
		},
	}
}

// SkillMutationTools returns skill_manage for generated standalone skills.
func SkillMutationTools(importer *Importer, recorder MutationRecorder) *llmcontracts.RuntimeTools {
	if importer == nil {
		return nil
	}
	exec := func(ctx context.Context, name string, input json.RawMessage) (string, bool, bool, error) {
		if strings.ToLower(strings.TrimSpace(name)) != "skill_manage" {
			return "", false, false, nil
		}
		res, err := runSkillManage(ctx, importer, recorder, input)
		return marshalToolResult(res, err)
	}
	return &llmcontracts.RuntimeTools{
		Definitions: []llmcontracts.RuntimeToolDefinition{{
			Name:        "skill_manage",
			Description: skillManageDescription,
			Parameters:  json.RawMessage(skillManageSchema),
		}},
		Executor: exec,
		Filter: func(name string) (bool, bool) {
			if strings.ToLower(strings.TrimSpace(name)) == "skill_manage" {
				return true, true
			}
			return false, false
		},
	}
}

const skillDeclarationExample = "---\n" +
	"kind: openvibely.agent_skill\n" +
	"version: 1\n" +
	"skill:\n" +
	"  key: <skill_key>\n" +
	"  name: <Skill Name>\n" +
	"  scope: project\n" +
	"  description: <one-line purpose>\n" +
	"---\n" +
	"<instructions for this skill>"

const skillDeclarationFieldDoc = "Full skills/<skill>/SKILL.md content including YAML frontmatter delimited by '---' lines, " +
	"followed by the instructions for that one skill. " +
	"REQUIRED frontmatter fields: kind (must be 'openvibely.agent_skill'), version (integer >= 1), skill.key (lowercase slug, [a-z0-9_]+). " +
	"Do not set agent.key for standalone generated skills. Optional: skill.name, skill.scope (global|project), skill.description, routing. " + "Agent-level tools, permissions, lifecycle_hooks, routing defaults, model_defaults, and tool_config belong in the agent root SKILLS.md declaration, not in an individual SKILL.md unless intentionally patching legacy metadata. " +
	"Example:\n" + skillDeclarationExample

const indexUpdateReminder = " After a successful write, the mutation tool maintains the top-level skills/SKILLS.md skill-link index. " +
	"Use the global skills root for scope=global mutations and the project skills root for scope=project."
const skillManageDescription = "Create, patch, archive, write, or remove a support file for a generated skill. " +
	"Actions: create (new skill), patch (update existing), write_file/remove_file (manage references/templates/scripts/assets), archive (deprecate). " +
	"For create/patch you MUST pass a complete standalone skills/<skill>/SKILL.md in the 'declaration' field (see field description for required frontmatter). " +
	"Path traversal, unknown support directories, and protected artifacts are rejected." + indexUpdateReminder

var skillManageSchema = buildSkillManageSchema()
var agentSkillManageSchema = buildAgentSkillManageSchema()

func agentSkillManageDescription(agentKey string) string {
	return "Create, patch, or manage support files for skills owned by the task's assigned agent `" + agentKey + "`. " +
		"Use this only for learning specific to that assigned agent's role, workflow, or selected agent-owned skills. " +
		"Use skill_manage instead for reusable standalone/project/global skill learning. The backend scopes all writes to agents/" + agentKey + "/skills; do not pass agent paths."
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func buildAgentSkillManageSchema() string {
	return `{
  "type": "object",
  "properties": {
    "action": {"type": "string", "enum": ["create", "patch", "write_file", "remove_file"], "description": "What to do. 'create' and 'patch' require 'declaration'. 'write_file' and 'remove_file' require 'handle' and 'support'."},
    "handle": {"type": "string", "description": "Assigned-agent skill key only, for example 'review_migrations'. Do not include an agent prefix."},
    "declaration": {"type": "string", "description": "REQUIRED for create/patch. Full agent-owned skills/<skill>/SKILL.md content including YAML frontmatter. Required fields: kind=openvibely.agent_skill, version>=1, skill.key. Do not set agent.key; the server scopes the write to the task's assigned agent."},
    "support": {
      "type": "object",
      "description": "Required only for action='write_file' or action='remove_file'. Manages a file under references/, templates/, scripts/, or assets/ of the assigned-agent skill.",
      "properties": {
        "kind": {"type": "string", "enum": ["references", "templates", "scripts", "assets"]},
        "path": {"type": "string", "description": "Path relative to the chosen support directory, for example 'foo.md' or 'nested/foo.md'."},
        "content_base64": {"type": "string", "description": "Base64-encoded file content. Use this OR 'content'."},
        "content": {"type": "string", "description": "Plain-text file content. Use this OR 'content_base64'."}
      }
    },
    "reason": {"type": "string", "description": "Free-text reason. Recorded in the mutation audit log."}
  },
  "required": ["action"],
  "additionalProperties": false
}`
}

func buildSkillManageSchema() string {
	return `{
  "type": "object",
  "properties": {
    "action": {"type": "string", "enum": ["create", "patch", "write_file", "remove_file", "archive"], "description": "What to do. 'create' and 'patch' require 'declaration'. 'write_file' and 'remove_file' require 'handle' and 'support'. 'archive' requires 'handle'."},
    "handle": {"type": "string", "description": "Standalone <skill_key> handle. Required for write_file, remove_file, and archive."},
    "scope":  {"type": "string", "enum": ["global", "project"], "description": "Where to write the skill. Defaults to the scope declared in the declaration's skill.scope or 'project'."},
    "declaration": {"type": "string", "description": ` + jsonString("REQUIRED for create/patch. "+skillDeclarationFieldDoc) + `},
    "support": {
      "type": "object",
      "description": "Required only for action='write_file' or action='remove_file'. Manages a file under references/, templates/, scripts/, or assets/ of the skill.",
      "properties": {
        "kind": {"type": "string", "enum": ["references", "templates", "scripts", "assets"]},
        "path": {"type": "string", "description": "Path relative to the chosen support directory, for example 'foo.md' or 'nested/foo.md'. Do not include references/, templates/, scripts/, or assets/."},
        "content_base64": {"type": "string", "description": "Base64-encoded file content. Use this OR 'content'."},
        "content": {"type": "string", "description": "Plain-text file content. Use this OR 'content_base64'."}
      }
    },
    "absorbed_into": {"type": "string", "description": "Optional. When archiving, the handle of the skill that supersedes this one."},
    "reason": {"type": "string", "description": "Free-text reason. Recorded in the mutation audit log."}
  },
  "required": ["action"],
  "additionalProperties": false
}`
}

type agentSkillManageParams struct {
	Action      string `json:"action"`
	Handle      string `json:"handle"`
	Declaration string `json:"declaration"`
	Support     struct {
		Kind          string `json:"kind"`
		Path          string `json:"path"`
		ContentBase64 string `json:"content_base64"`
		Content       string `json:"content"`
	} `json:"support"`
	Reason string `json:"reason"`
}

type skillManageParams struct {
	Action      string `json:"action"`
	Handle      string `json:"handle"`
	Scope       string `json:"scope"`
	Declaration string `json:"declaration"`
	Support     struct {
		Kind          string `json:"kind"`
		Path          string `json:"path"`
		ContentBase64 string `json:"content_base64"`
		Content       string `json:"content"`
	} `json:"support"`
	AbsorbedInto string `json:"absorbed_into"`
	Reason       string `json:"reason"`
}

func runAgentSkillManage(ctx context.Context, importer *Importer, recorder MutationRecorder, assignedAgentKey, scope string, input json.RawMessage) (*ImportResult, error) {
	var p agentSkillManageParams
	if err := json.Unmarshal(input, &p); err != nil {
		return nil, fmt.Errorf("agent_skill_manage: invalid input: %w", err)
	}
	action := strings.ToLower(strings.TrimSpace(p.Action))
	mutationKey := strings.TrimSpace(assignedAgentKey)
	if p.Handle != "" {
		mutationKey += "/" + p.Handle
	}
	switch action {
	case "create", "patch":
		if p.Declaration == "" {
			return nil, errors.New("agent_skill_manage: declaration is required for create/patch")
		}
		decl, body, err := ParseDeclaration(p.Declaration)
		if err != nil {
			return blockedResult(ctx, recorder, action, "agent_skill", mutationKey, []byte(p.Declaration), err)
		}
		if decl.IsAgentRootDeclaration() {
			return blockedResult(ctx, recorder, action, "agent_skill", assignedAgentKey, []byte(p.Declaration), errors.New("agent_skill_manage requires skill.key and cannot edit agent root declarations"))
		}
		if strings.TrimSpace(decl.Agent.Key) != "" && decl.Agent.Key != assignedAgentKey {
			return blockedResult(ctx, recorder, action, "agent_skill", mutationKey, []byte(p.Declaration), fmt.Errorf("agent_skill_manage is scoped to assigned agent %q", assignedAgentKey))
		}
		if p.Handle != "" && p.Handle != decl.Handle() {
			return blockedResult(ctx, recorder, action, "agent_skill", mutationKey, []byte(p.Declaration), fmt.Errorf("handle mismatch: declaration is %s but handle is %s", decl.Handle(), p.Handle))
		}
		res, err := importer.WriteAgentOwnedSkill(ctx, scope, assignedAgentKey, decl, body)
		recordMutation(ctx, recorder, action, "agent_skill", assignedAgentKey+"/"+decl.Handle(), []byte(p.Declaration), res, err)
		return res, err
	case "write_file", "remove_file":
		if p.Handle == "" {
			return nil, fmt.Errorf("agent_skill_manage: handle is required for %s", action)
		}
		var (
			res   *ImportResult
			err   error
			bytes int
		)
		if action == "write_file" {
			content, decErr := decodeSupportContent(p.Support.Content, p.Support.ContentBase64)
			if decErr != nil {
				return nil, fmt.Errorf("agent_skill_manage: %w", decErr)
			}
			bytes = len(content)
			res, err = importer.WriteAgentOwnedSupportFile(ctx, scope, assignedAgentKey, p.Handle, SupportFileKind(p.Support.Kind), p.Support.Path, content)
		} else {
			res, err = importer.RemoveAgentOwnedSupportFile(ctx, scope, assignedAgentKey, p.Handle, SupportFileKind(p.Support.Kind), p.Support.Path)
		}
		payload, _ := json.Marshal(struct {
			Agent  string `json:"agent"`
			Handle string `json:"handle"`
			Kind   string `json:"kind"`
			Path   string `json:"path"`
			Bytes  int    `json:"bytes,omitempty"`
		}{assignedAgentKey, p.Handle, p.Support.Kind, p.Support.Path, bytes})
		recordMutation(ctx, recorder, action, "agent_support_file", assignedAgentKey+"/"+p.Handle, payload, res, err)
		return res, err
	default:
		return nil, fmt.Errorf("agent_skill_manage: unknown action %q", p.Action)
	}
}

func runSkillManage(ctx context.Context, importer *Importer, recorder MutationRecorder, input json.RawMessage) (*ImportResult, error) {
	var p skillManageParams
	if err := json.Unmarshal(input, &p); err != nil {
		return nil, fmt.Errorf("skill_manage: invalid input: %w", err)
	}
	action := strings.ToLower(strings.TrimSpace(p.Action))
	switch action {
	case "create", "patch":
		if p.Declaration == "" {
			return nil, errors.New("skill_manage: declaration is required for create/patch")
		}
		decl, body, err := ParseDeclaration(p.Declaration)
		if err != nil {
			return blockedResult(ctx, recorder, action, "skill", "", []byte(p.Declaration), err)
		}
		if decl.IsAgentRootDeclaration() {
			return blockedResult(ctx, recorder, action, "skill", decl.Agent.Key, []byte(p.Declaration), errors.New("skill_manage requires skill.key; agents are managed in the agent dialog"))
		}
		if strings.TrimSpace(decl.Agent.Key) != "" {
			return blockedResult(ctx, recorder, action, "skill", decl.Handle(), []byte(p.Declaration), errors.New("skill_manage creates standalone skills; omit agent.key"))
		}
		if p.Handle != "" && p.Handle != decl.Handle() {
			return blockedResult(ctx, recorder, action, "skill", p.Handle, []byte(p.Declaration), fmt.Errorf("handle mismatch: declaration is %s but handle is %s", decl.Handle(), p.Handle))
		}
		if p.Scope != "" {
			decl.Skill.Scope = p.Scope
		}
		if err := validateMutationScope(decl); err != nil {
			return blockedResult(ctx, recorder, action, "skill", decl.Handle(), []byte(p.Declaration), err)
		}
		res, err := importer.WriteSkill(ctx, decl, body)
		recordMutation(ctx, recorder, action, "skill", decl.Handle(), []byte(p.Declaration), res, err)
		return res, err
	case "write_file", "remove_file":
		if p.Handle == "" {
			return nil, fmt.Errorf("skill_manage: handle is required for %s", action)
		}
		scope := p.Scope
		if scope == "" {
			scope = "project"
		}
		if err := validateScopeValue(scope); err != nil {
			return blockedResult(ctx, recorder, action, "support_file", p.Handle, nil, err)
		}
		var (
			res   *ImportResult
			err   error
			bytes int
		)
		if action == "write_file" {
			content, decErr := decodeSupportContent(p.Support.Content, p.Support.ContentBase64)
			if decErr != nil {
				return nil, fmt.Errorf("skill_manage: %w", decErr)
			}
			bytes = len(content)
			res, err = importer.WriteSupportFile(ctx, scope, p.Handle, SupportFileKind(p.Support.Kind), p.Support.Path, content)
		} else {
			res, err = importer.RemoveSupportFile(ctx, scope, p.Handle, SupportFileKind(p.Support.Kind), p.Support.Path)
		}
		payload, _ := json.Marshal(struct {
			Handle string `json:"handle"`
			Kind   string `json:"kind"`
			Path   string `json:"path"`
			Bytes  int    `json:"bytes,omitempty"`
		}{p.Handle, p.Support.Kind, p.Support.Path, bytes})
		recordMutation(ctx, recorder, action, "support_file", p.Handle, payload, res, err)
		return res, err
	case "archive":
		if p.Handle == "" {
			return nil, errors.New("skill_manage: handle is required for archive")
		}
		res, err := importer.ArchiveSkill(ctx, p.Handle, p.AbsorbedInto, p.Reason)
		payload, _ := json.Marshal(struct {
			Handle       string `json:"handle"`
			AbsorbedInto string `json:"absorbed_into,omitempty"`
			Reason       string `json:"reason,omitempty"`
		}{p.Handle, p.AbsorbedInto, p.Reason})
		recordMutation(ctx, recorder, action, "skill", p.Handle, payload, res, err)
		return res, err
	default:
		return nil, fmt.Errorf("skill_manage: unknown action %q", p.Action)
	}
}

// validateMutationScope normalizes empty scopes to "project" and rejects any
// value other than "global" or "project". Both supported scopes are allowed;
// the importer maps them to the configured global and project skill roots.
func validateMutationScope(decl *SkillDeclaration) error {
	if decl == nil {
		return errors.New("declaration is required")
	}
	if err := validateScopeValue(decl.Agent.Scope); err != nil {
		return fmt.Errorf("agent.%w", err)
	}
	if !decl.IsAgentRootDeclaration() {
		if err := validateScopeValue(decl.Skill.Scope); err != nil {
			return fmt.Errorf("skill.%w", err)
		}
	}
	if strings.TrimSpace(decl.Agent.Scope) == "" {
		decl.Agent.Scope = "project"
	}
	if !decl.IsAgentRootDeclaration() && strings.TrimSpace(decl.Skill.Scope) == "" {
		decl.Skill.Scope = "project"
	}
	return nil
}

func validateScopeValue(scope string) error {
	switch strings.TrimSpace(scope) {
	case "", "global", "project":
		return nil
	default:
		return fmt.Errorf("scope must be global or project, got %q", scope)
	}
}

func decodeSupportContent(plain, b64 string) ([]byte, error) {
	if b64 != "" {
		raw, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return nil, fmt.Errorf("support content_base64: %w", err)
		}
		return raw, nil
	}
	if plain != "" {
		return []byte(plain), nil
	}
	return nil, errors.New("support content is empty")
}

func blockedResult(ctx context.Context, recorder MutationRecorder, action, target, key string, payload []byte, cause error) (*ImportResult, error) {
	res := &ImportResult{Applied: false, Blocked: []string{key}}
	recordMutation(ctx, recorder, action, target, key, payload, res, cause)
	return res, cause
}

func recordMutation(ctx context.Context, recorder MutationRecorder, action, target, key string, payload []byte, result *ImportResult, cause error) {
	if recorder == nil {
		return
	}
	_ = recorder.Record(ctx, action, target, key, payload, result, cause)
}

func marshalToolResult(res *ImportResult, err error) (string, bool, bool, error) {
	if err != nil && res == nil {
		return err.Error(), true, true, nil
	}
	if res == nil {
		res = &ImportResult{}
	}
	if err != nil {
		// Surface the cause inline; result.Applied=false already conveys block.
		merged := struct {
			*ImportResult
			Error string `json:"error,omitempty"`
		}{ImportResult: res, Error: err.Error()}
		raw, _ := json.Marshal(merged)
		return string(raw), true, true, nil
	}
	raw, mErr := json.Marshal(res)
	if mErr != nil {
		return mErr.Error(), true, true, nil
	}
	return string(raw), true, false, nil
}
