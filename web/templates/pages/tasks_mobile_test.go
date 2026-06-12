package pages

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/openvibely/openvibely/internal/models"
)

func TestTasksContentHasMobileSafeShell(t *testing.T) {
	project := &models.Project{ID: "project-1", Name: "Project"}
	var buf bytes.Buffer
	if err := TasksContent(project, nil, nil, nil, "", "").Render(context.Background(), &buf); err != nil {
		t.Fatalf("render tasks content: %v", err)
	}

	body := buf.String()
	for _, want := range []string{
		"overflow-x-hidden",
		"overflow-y-auto",
		"min-h-0",
		"flex-col",
		"sm:flex-row",
		"min-h-11",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected mobile-safe tasks page shell to contain %q, got %s", want, body)
		}
	}
}
