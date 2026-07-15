package contracts

import (
	"context"

	"github.com/openvibely/openvibely/internal/models"
)

// Operation identifies the high-level call shape.
type Operation string

const (
	OperationDirect    Operation = "direct"
	OperationStreaming Operation = "streaming"
	OperationTask      Operation = "task"
)

// AgentRequest is the canonical provider-agnostic request contract passed to adapters.
type AgentRequest struct {
	Ctx                 context.Context
	Operation           Operation
	Message             string
	Attachments         []models.Attachment
	Agent               models.LLMConfig
	ExecID              string
	TransportScope      string // Stable provider transport identity (for example task:<id> or chat:project:<id>)
	ChatHistory         []models.Execution
	ChatMode            models.ChatMode
	ChatSystemContext   string
	WorkDir             string
	Followup            bool
	ProjectInstructions string
	AgentDefinition     *models.Agent // Optional agent definition (system prompt, skills, MCP)
	PluginDirs          []string      // Optional plugin directories for CLI sessions (--plugin-dir)
	DisableTools        bool          // Optional: suppress tool/plugin execution for this request
	RawDirectPrompt     bool          // Optional: direct request message is already fully composed; skip OpenVibely task/system prompt wrapping
}

type transportScopeContextKey struct{}

func WithTransportScope(ctx context.Context, scope string) context.Context {
	if ctx == nil || scope == "" {
		return ctx
	}
	return context.WithValue(ctx, transportScopeContextKey{}, scope)
}

func TransportScopeFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	scope, _ := ctx.Value(transportScopeContextKey{}).(string)
	return scope
}

// Usage tracks provider usage in a canonical shape.
// Only TotalTokens is guaranteed across all transports; the other fields are best-effort.
type Usage struct {
	InputTokens       int
	OutputTokens      int
	TotalTokens       int
	CachedInputTokens int
	ReasoningTokens   int
	ProviderRaw       map[string]int
	ProviderIDs       map[string]string
}

// AgentResult is the canonical provider-agnostic adapter response.
type ChatContext struct {
	Messages []ChatContextMessage `json:"messages"`
}

type ChatContextMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type AgentResult struct {
	Output         string
	TextOnlyOutput string
	Usage          Usage
	StopReason     string
	SessionID      string
	ChatContext    ChatContext
}
