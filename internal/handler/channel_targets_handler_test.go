package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/repository"
	"github.com/openvibely/openvibely/internal/service"
)

type outboundTargetTestSlack struct {
	channelID string
	threadTS  string
	text      string
}

func (s *outboundTargetTestSlack) SendOutboundMessage(ctx context.Context, channelID, threadTS, text string) service.SendMessageResult {
	_ = ctx
	s.channelID = channelID
	s.threadTS = threadTS
	s.text = text
	return service.SendMessageResult{OK: true, Platform: "slack", Target: "slack:" + channelID + ":" + threadTS, MessageID: "123.456"}
}

func TestOutboundTargetHandlersDenyCrossProjectTargetIDs(t *testing.T) {
	tc := NewTestContext(t)
	ownerProject := tc.CreateProject().WithName("Owner Project").Build()
	otherProject := tc.CreateProject().WithName("Other Project").Build()
	targetRepo := repository.NewChannelTargetRepo(tc.db)
	slack := &outboundTargetTestSlack{}
	router := service.NewChannelMessageRouter(targetRepo, tc.settingsRepo)
	router.SetSlackService(slack)
	tc.handler.SetChannelTargetRepo(targetRepo)
	tc.handler.SetChannelMessageRouter(router)

	target := models.ChannelTarget{
		ID:        repository.NewID(),
		ProjectID: ownerProject.ID,
		Platform:  "slack",
		Name:      "alerts",
		TargetID:  "COWNER",
	}
	if err := targetRepo.Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert target: %v", err)
	}

	editForm := url.Values{}
	editForm.Set("id", target.ID)
	editForm.Set("project_id", otherProject.ID)
	editForm.Set("platform", "slack")
	editForm.Set("name", "stolen")
	editForm.Set("target_id", "COTHER")
	editReq := httptest.NewRequest(http.MethodPost, "/channels/outbound-targets", strings.NewReader(editForm.Encode()))
	editReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	editRec := httptest.NewRecorder()
	tc.echo.ServeHTTP(editRec, editReq)
	if editRec.Code != http.StatusNotFound {
		t.Fatalf("expected cross-project edit to be denied with 404, got %d body=%s", editRec.Code, editRec.Body.String())
	}
	stored, err := targetRepo.GetByID(context.Background(), target.ID)
	if err != nil || stored == nil || stored.ProjectID != ownerProject.ID || stored.TargetID != "COWNER" || stored.Name != "alerts" {
		t.Fatalf("target should remain unchanged after cross-project edit, target=%+v err=%v", stored, err)
	}

	deletePath := "/channels/outbound-targets/" + target.ID + "?project_id=" + url.QueryEscape(otherProject.ID)
	deleteRec := tc.HTMX().Delete(deletePath).Execute()
	if deleteRec.Code != http.StatusNotFound {
		t.Fatalf("expected cross-project delete to be denied with 404, got %d body=%s", deleteRec.Code, deleteRec.Body.String())
	}
	stored, err = targetRepo.GetByID(context.Background(), target.ID)
	if err != nil || stored == nil {
		t.Fatalf("target should remain after cross-project delete, target=%v err=%v", stored, err)
	}

	testPath := "/channels/outbound-targets/" + target.ID + "/test?project_id=" + url.QueryEscape(otherProject.ID)
	testRec := tc.HTMX().Post(testPath).Execute()
	if testRec.Code != http.StatusNotFound {
		t.Fatalf("expected cross-project test send to be denied with 404, got %d body=%s", testRec.Code, testRec.Body.String())
	}
	if slack.channelID != "" || slack.text != "" {
		t.Fatalf("cross-project test send should not dispatch, channel=%q text=%q", slack.channelID, slack.text)
	}
}

func TestOutboundTargetTestPreservesThreadIDAndEscapesResult(t *testing.T) {
	tc := NewTestContext(t)
	project := tc.CreateProject().WithName("Outbound Target Project").Build()
	targetRepo := repository.NewChannelTargetRepo(tc.db)
	slack := &outboundTargetTestSlack{}
	router := service.NewChannelMessageRouter(targetRepo, tc.settingsRepo)
	router.SetSlackService(slack)
	tc.handler.SetChannelTargetRepo(targetRepo)
	tc.handler.SetChannelMessageRouter(router)

	target := models.ChannelTarget{
		ID:        repository.NewID(),
		ProjectID: project.ID,
		Platform:  "slack",
		Name:      "alerts",
		TargetID:  "C123",
		ThreadID:  "1690000000.000000",
		Home:      true,
	}
	if err := targetRepo.Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert target: %v", err)
	}

	path := "/channels/outbound-targets/" + target.ID + "/test?project_id=" + url.QueryEscape(project.ID)
	rec := tc.HTMX().Post(path).Execute()
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if slack.channelID != "C123" || slack.threadTS != "1690000000.000000" {
		t.Fatalf("sent to channel=%q thread=%q", slack.channelID, slack.threadTS)
	}
	if slack.text != "Test message from OpenVibely" {
		t.Fatalf("unexpected message %q", slack.text)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Test sent:") || !strings.Contains(body, "slack:C123:1690000000.000000") {
		t.Fatalf("unexpected body %q", body)
	}
	if strings.Contains(body, `{"ok":true`) {
		t.Fatalf("expected JSON to be HTML-escaped, got %q", body)
	}
	if !strings.Contains(body, "&#34;ok&#34;:true") {
		t.Fatalf("expected escaped JSON, got %q", body)
	}
}
