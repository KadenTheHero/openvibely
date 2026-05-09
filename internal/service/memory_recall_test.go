package service

import (
	"strings"
	"testing"

	"github.com/openvibely/openvibely/internal/memory"
)

func TestMemoryRecallSelectionPromptUsesManifestAndRequest(t *testing.T) {
	files := []memory.MemoryFile{{
		RelPath:   "provider_architecture.md",
		Meta:      memory.FileMeta{Title: "Provider architecture", Type: memory.TypeProject, Confidence: "high", Updated: "2026-05-09"},
		SizeBytes: 512,
	}}
	prompt := buildMemoryRecallSelectionPrompt(MemoryRecallQuery{
		Surface: "task",
		Title:   "Fix OpenAI memory tools",
		Prompt:  "Make memory work generically for API providers",
	}, "- provider_architecture.md: Provider guidance", files)

	for _, want := range []string{
		"You are selecting memories that will be useful",
		"Return JSON only",
		"Select memories relevant to:",
		"Fix OpenAI memory tools",
		"provider_architecture.md",
		"Provider architecture",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestMemoryRecallSynthesisPromptIncludesSelectedBodies(t *testing.T) {
	prompt := buildMemoryRecallSynthesisPrompt(MemoryRecallQuery{Surface: "task", Title: "Schedules", Prompt: "What ran?"}, []memory.MemoryFile{{
		RelPath: "memory_consolidation_schedule.md",
		Meta:    memory.FileMeta{Title: "Memory schedule", Type: memory.TypeProject},
		Body:    "Memory consolidation should run as a normal scheduled task.",
	}})

	if !strings.Contains(prompt, "You read persistent memory files") || !strings.Contains(prompt, "Memory consolidation should run as a normal scheduled task") {
		t.Fatalf("unexpected synthesis prompt:\n%s", prompt)
	}
}

func TestRenderSynthesizedMemoryContext(t *testing.T) {
	out := renderSynthesizedMemoryContext(memoryRecallSynthesis{
		RelevantFacts: []string{"Use provider-generic runtime tools.", ""},
		CitedMemories: []string{"provider_architecture.md", "provider_architecture.md"},
	})
	if !strings.Contains(out, "Recalled from your persistent memory system:") || !strings.Contains(out, "Use provider-generic runtime tools.") {
		t.Fatalf("unexpected context:\n%s", out)
	}
	if !strings.Contains(out, "Sources: provider_architecture.md") || strings.Count(out, "provider_architecture.md") != 1 {
		t.Fatalf("expected deduped citation, got:\n%s", out)
	}
}

func TestUnmarshalModelJSONAllowsWrappedJSON(t *testing.T) {
	var got memoryRecallSelection
	if err := unmarshalModelJSON("```json\n{\"selected_memories\":[\"a.md\"]}\n```", &got); err != nil {
		t.Fatalf("unmarshal wrapped json: %v", err)
	}
	if len(got.SelectedMemories) != 1 || got.SelectedMemories[0] != "a.md" {
		t.Fatalf("unexpected selection: %#v", got)
	}
}
