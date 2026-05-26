package agentlibrary

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMutationToolSchemasAreValidJSONAndIncludeRequiredHints(t *testing.T) {
	tools := MutationTools(&Importer{}, nil)
	if tools == nil {
		t.Fatal("MutationTools returned nil")
	}
	if len(tools.Definitions) != 1 {
		t.Fatalf("expected 1 tool def, got %d", len(tools.Definitions))
	}
	for _, def := range tools.Definitions {
		var v map[string]any
		if err := json.Unmarshal(def.Parameters, &v); err != nil {
			t.Fatalf("invalid JSON schema for %s: %v\nschema:\n%s", def.Name, err, string(def.Parameters))
		}
		props, ok := v["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s: missing properties", def.Name)
		}
		decl, ok := props["declaration"].(map[string]any)
		if !ok {
			t.Fatalf("%s: missing declaration property", def.Name)
		}
		descRaw, _ := decl["description"].(string)
		// Description must mention all required fields so the LLM knows
		// what to put in the declaration string.
		for _, want := range []string{
			"kind",
			"openvibely.agent_skill",
			"version",
			"skill.key",
			"Example",
		} {
			if !strings.Contains(descRaw, want) {
				t.Errorf("%s declaration.description missing %q\ngot: %s", def.Name, want, descRaw)
			}
		}
	}
}
