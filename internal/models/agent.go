package models

import "time"

// AgentModelOption represents one selectable model override in the Agent modal.
type AgentModelOption struct {
	Value string
	Label string
}

// MCPServerConfig defines an MCP server connection for an agent.
type MCPServerConfig struct {
	Name    string            `json:"name"`
	Type    string            `json:"type,omitempty"`    // stdio, http, sse, ws
	Command []string          `json:"command,omitempty"` // stdio server command + args
	URL     string            `json:"url,omitempty"`     // remote server URL
	Env     map[string]string `json:"env,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// SkillConfig defines a skill (slash command) embedded in an agent.
type SkillConfig struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Tools       string `json:"tools,omitempty"` // comma-separated tool names
	Content     string `json:"content"`         // the skill instruction body
}

// ScopedFilesConfig grants project-relative filesystem access constrained to a
// directory and explicit permissions.
type ScopedFilesConfig struct {
	Directory   string   `json:"directory"`
	Permissions []string `json:"permissions"`
}

// AgentToolConfig stores structured configuration for parameterized tools.
type AgentToolConfig struct {
	ScopedFiles            []ScopedFilesConfig `json:"scoped_files,omitempty"`
	SkipDefaultTools       bool                `json:"skip_default_tools,omitempty"`
	DisableRuntimeWorktree bool                `json:"disable_runtime_worktree,omitempty"`
}

// Agent is a named configuration that wraps a system prompt, tool restrictions,
// skills, MCP servers, and parameterized tool config. Tasks can be assigned to an agent.
type Agent struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	SystemPrompt string            `json:"system_prompt"`
	Model        string            `json:"model"` // inherit, sonnet, haiku, opus
	Tools        []string          `json:"tools"`
	ToolConfig   AgentToolConfig   `json:"tool_config"`
	Plugins      []string          `json:"plugins"` // plugin IDs: "plugin@marketplace"
	MCPServers   []MCPServerConfig `json:"mcp_servers"`
	SystemKind   string            `json:"system_kind,omitempty"`
	Skills       []SkillConfig     `json:"skills"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

// PluginMarketplace mirrors Claude plugin marketplace list metadata.
type PluginMarketplace struct {
	Name            string `json:"name"`
	Source          string `json:"source"`
	URL             string `json:"url,omitempty"`
	Repo            string `json:"repo,omitempty"`
	InstallLocation string `json:"installLocation,omitempty"`
}

// InstalledPlugin mirrors `claude plugin list --json` entries.
type InstalledPlugin struct {
	ID          string   `json:"id"`
	Version     string   `json:"version,omitempty"`
	Scope       string   `json:"scope,omitempty"`
	Enabled     bool     `json:"enabled"`
	InstallPath string   `json:"installPath,omitempty"`
	InstalledAt string   `json:"installedAt,omitempty"`
	LastUpdated string   `json:"lastUpdated,omitempty"`
	Errors      []string `json:"errors,omitempty"`
}

// AvailablePlugin mirrors `claude plugin list --json --available` entries.
type AvailablePlugin struct {
	PluginID        string `json:"pluginId"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	MarketplaceName string `json:"marketplaceName"`
	Source          string `json:"source,omitempty"`
}

// PluginState returns the current plugin marketplace/installation view.
type PluginState struct {
	Marketplaces []PluginMarketplace `json:"marketplaces"`
	Installed    []InstalledPlugin   `json:"installed"`
	Available    []AvailablePlugin   `json:"available"`
	Runtime      []PluginRuntimeMCP  `json:"runtime,omitempty"`
}

// PluginRuntimeMCP reports MCP server runtime health for plugin-backed tools.
type PluginRuntimeMCP struct {
	Name      string `json:"name"`
	PluginID  string `json:"plugin_id,omitempty"` // owning plugin ID (e.g. "github@marketplace")
	Status    string `json:"status"`              // running, failed, stopped
	Error     string `json:"error,omitempty"`
	ToolCount int    `json:"tool_count,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// AllAgentTools is the set of tool names an agent can allow.
const (
	AgentSystemKindMemoryConsolidator = "memory_consolidator"
	AgentToolScopedFiles              = "ScopedFiles"
)

var AllAgentTools = []string{
	"Read", "Write", "Edit", "Bash", "Glob", "Grep",
	"WebFetch", "WebSearch", "NotebookEdit", AgentToolScopedFiles,
}
