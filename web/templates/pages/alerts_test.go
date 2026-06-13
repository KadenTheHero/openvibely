package pages

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/openvibely/openvibely/internal/models"
)

func TestAlertsContent_DeleteActionsDoNotDependOnHxConfirm(t *testing.T) {
	alerts := []models.Alert{{ID: "alert-1", Title: "Disk full", ProjectID: "project-1"}}

	var buf bytes.Buffer
	err := AlertsContent(alerts, "project-1", 1).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render alerts content: %v", err)
	}

	html := buf.String()
	if !strings.Contains(html, `data-delete-url="/alerts?project_id=project-1"`) {
		t.Fatal("expected delete-all alerts action to provide delete URL via dataset")
	}
	if !strings.Contains(html, `data-delete-url="/alerts/alert-1?project_id=project-1"`) {
		t.Fatal("expected per-alert delete action to provide delete URL via dataset")
	}
	if !strings.Contains(html, `onclick="return deleteAlertsFromDataset(this)"`) {
		t.Fatal("expected delete-all action to call dataset-based delete helper")
	}
	if !strings.Contains(html, `deleteAlertsFromDataset(this)`) {
		t.Fatal("expected per-alert action to call dataset-based delete helper")
	}
	if strings.Contains(html, `hx-confirm="Delete all alerts? This action cannot be undone."`) {
		t.Fatal("delete-all should not depend on hx-confirm in desktop webview")
	}
	if strings.Contains(html, `hx-confirm="Delete this alert?"`) {
		t.Fatal("per-alert delete should not depend on hx-confirm in desktop webview")
	}
	if strings.Contains(html, `Delete all alerts? This action cannot be undone.`) {
		t.Fatal("alerts delete-all should not include confirmation copy")
	}
	if strings.Contains(html, `Delete this alert?`) {
		t.Fatal("per-alert delete should not include confirmation copy")
	}
	if strings.Contains(html, `function confirmAndDeleteAlerts(`) {
		t.Fatal("alerts template should not define confirmation-based delete helper")
	}
	if !strings.Contains(html, `function deleteAlerts(url, target)`) {
		t.Fatal("expected direct delete helper in alerts template")
	}
}

func TestAlertsContent_CardsConformToNarrowViewport(t *testing.T) {
	longText := strings.Repeat("SuperLongUnbrokenAlertToken", 8)
	alerts := []models.Alert{{ID: "alert-1", Title: longText, Message: longText, ProjectID: "project-1"}}

	var buf bytes.Buffer
	err := AlertsContent(alerts, "project-1", 1).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render alerts content: %v", err)
	}

	html := buf.String()
	for _, want := range []string{
		`id="alerts-container" class="h-full overflow-y-auto overflow-x-hidden max-w-full min-w-0"`,
		`class="grid grid-cols-1 gap-4 max-w-full min-w-0"`,
		`transition-all w-full min-w-0 max-w-full`,
		`card-body relative max-w-full min-w-0 p-4 sm:p-6`,
		`class="max-w-full min-w-0 pr-20 sm:pr-24"`,
		`font-semibold break-words [overflow-wrap:anywhere]`,
		`class="text-sm opacity-60 mt-1 break-words [overflow-wrap:anywhere]"`,
		`class="absolute top-4 right-4 flex items-center gap-1"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected alerts markup to include responsive class %q", want)
		}
	}
	if strings.Contains(html, "overflow-wrap-anywhere") {
		t.Fatal("alerts should use Tailwind arbitrary overflow-wrap utility, not a non-existent class")
	}
}
