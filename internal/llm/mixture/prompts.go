package mixture

import (
	"fmt"
	"strings"

	llmcontracts "github.com/openvibely/openvibely/internal/llm/contracts"
	"github.com/openvibely/openvibely/internal/models"
)

func ReferencePrompt(currentMessage string, history []models.Execution) string {
	var b strings.Builder
	b.WriteString("You are a reference model in a mixture-of-models process.\n\n")
	b.WriteString("Give concise advice for the final model. Focus on:\n")
	b.WriteString("- likely answer\n")
	b.WriteString("- risks or missing information\n")
	b.WriteString("- useful tool strategy\n")
	b.WriteString("- disagreements or caveats\n\n")
	b.WriteString("Do not call tools. Do not claim to have performed actions.\n\n")
	b.WriteString("User request:\n")
	b.WriteString(strings.TrimSpace(currentMessage))
	if relevant := RelevantConversation(history); relevant != "" {
		b.WriteString("\n\nRelevant conversation:\n")
		b.WriteString(relevant)
	}
	return b.String()
}

func RelevantConversation(history []models.Execution) string {
	var parts []string
	for _, turn := range history {
		if prompt := cleanConversationText(turn.PromptSent); prompt != "" {
			parts = append(parts, "User: "+prompt)
		}
		if output := cleanConversationText(turn.Output); output != "" {
			parts = append(parts, "Assistant: "+output)
		}
	}
	return strings.Join(parts, "\n\n")
}

func PrivateContext(results []ReferenceResult) string {
	var b strings.Builder
	b.WriteString("[Mixture of Models private context]\n")
	b.WriteString("You are the aggregator and acting model. The following reference models were asked for advisory output only. They did not call tools and may be wrong. Use them as private guidance, not as quoted user-visible content.\n")
	for i, result := range results {
		label := strings.TrimSpace(result.Label)
		if label == "" {
			label = fmt.Sprintf("Reference %d", i+1)
		}
		providerModel := strings.Trim(strings.TrimSpace(result.Provider)+"/"+strings.TrimSpace(result.Model), "/")
		if providerModel == "" {
			providerModel = "unknown"
		}
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("Reference %d - %s (%s):\n", i+1, label, providerModel))
		if result.Err != "" {
			b.WriteString("[failed: ")
			b.WriteString(strings.TrimSpace(result.Err))
			b.WriteString("]\n")
			continue
		}
		output := strings.TrimSpace(result.Output)
		if output == "" {
			output = "[failed: empty response]"
		}
		b.WriteString(output)
		b.WriteString("\n")
	}
	b.WriteString("\nInstruction: produce the final answer normally. Use tools only if the task requires them. Resolve disagreements yourself.\n")
	b.WriteString("[/Mixture of Models private context]")
	return b.String()
}

func AppendPrivateContext(req llmcontracts.AgentRequest, contextBlock string) llmcontracts.AgentRequest {
	contextBlock = strings.TrimSpace(contextBlock)
	if contextBlock == "" {
		return req
	}
	if strings.TrimSpace(req.ChatSystemContext) != "" {
		req.ChatSystemContext = strings.TrimSpace(req.ChatSystemContext) + "\n\n" + contextBlock
		return req
	}
	if strings.TrimSpace(req.ProjectInstructions) != "" {
		req.ProjectInstructions = strings.TrimSpace(req.ProjectInstructions) + "\n\n" + contextBlock
		return req
	}
	if strings.TrimSpace(req.Message) != "" {
		req.Message = strings.TrimSpace(req.Message) + "\n\n" + contextBlock
		return req
	}
	req.Message = contextBlock
	return req
}

func cleanConversationText(value string) string {
	var lines []string
	inFence := false
	for _, rawLine := range strings.Split(value, "\n") {
		line := strings.TrimSpace(rawLine)
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
			continue
		}
		if inFence || line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.Contains(line, "]") {
			continue
		}
		if strings.HasPrefix(line, "tool_result") || strings.HasPrefix(line, "tool_call") || strings.HasPrefix(line, "Tool result") {
			continue
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
