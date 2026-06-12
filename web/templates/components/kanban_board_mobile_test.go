package components

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/openvibely/openvibely/internal/models"
)

func TestKanbanBoardUsesResponsiveNonScrollingLayout(t *testing.T) {
	var buf bytes.Buffer
	if err := KanbanBoard(nil, "project-1", "", "", nil, nil).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render kanban board: %v", err)
	}

	body := buf.String()
	for _, want := range []string{
		`id="kanban-board"`,
		"grid-cols-1",
		"md:grid-cols-2",
		"lg:grid-cols-4",
		"overflow-x-hidden",
		"overflow-y-auto",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected kanban board markup to contain %q, got %s", want, body)
		}
	}
	if strings.Contains(body, "overflow-x-auto") {
		t.Fatalf("kanban board should not force horizontal scrolling on mobile, got %s", body)
	}
}

func TestKanbanColumnHasMobileSafeWidthAndTouchMenu(t *testing.T) {
	tasks := []models.Task{
		{
			ID:        "task-1",
			ProjectID: "project-1",
			Title:     "Urgent backlog task",
			Category:  models.CategoryBacklog,
			Status:    models.StatusPending,
			Priority:  4,
		},
	}

	var buf bytes.Buffer
	if err := KanbanColumn(tasks, "project-1", models.CategoryBacklog, "", "", nil, nil).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render kanban column: %v", err)
	}

	body := buf.String()
	for _, want := range []string{
		"kanban-column",
		"w-full",
		"min-h-[18rem]",
		"lg:h-full",
		"min-h-11",
		"h-11",
		"w-11",
		"max-w-[calc(100vw-2rem)]",
		`class="text-sm min-h-11`,
		`/tasks/backlog/activate?project_id=project-1`,
		`class="text-sm pl-8 min-h-11`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected responsive kanban column markup to contain %q, got %s", want, body)
		}
	}

	activateIdx := strings.Index(body, `/tasks/backlog/activate?project_id=project-1`)
	if activateIdx == -1 {
		t.Fatalf("expected backlog activate action in markup, got %s", body)
	}
	activateSnippet := body[activateIdx:]
	if len(activateSnippet) > 500 {
		activateSnippet = activateSnippet[:500]
	}
	if !strings.Contains(activateSnippet, `class="text-sm min-h-11"`) {
		t.Fatalf("expected backlog activate action to have touch-friendly min height, got %s", activateSnippet)
	}
}
