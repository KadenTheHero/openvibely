package pages

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/openvibely/openvibely/internal/models"
)

func TestAnalyticsContent_TokenUsageModelSelectStaysWithinCard(t *testing.T) {
	project := &models.Project{ID: "project-1", Name: "Project One"}

	var buf bytes.Buffer
	if err := AnalyticsContent(project).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render analytics content: %v", err)
	}

	content := buf.String()
	required := []string{
		`<div class="flex flex-wrap items-end justify-between gap-3 mb-2 min-w-0">`,
		`<div class="form-control min-w-0 w-full sm:w-auto">`,
		`id="usageRateModelSelect" class="select select-bordered select-xs w-full max-w-full sm:min-w-48"`,
	}
	for _, expected := range required {
		if !strings.Contains(content, expected) {
			t.Fatalf("Token Usage model select should stay within its card on narrow screens; missing %q", expected)
		}
	}
	if strings.Contains(content, `id="usageRateModelSelect" class="select select-bordered select-xs min-w-48"`) {
		t.Fatal("Token Usage model select should not force a fixed minimum width on mobile")
	}
}
