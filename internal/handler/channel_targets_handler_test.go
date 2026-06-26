package handler

import (
	"context"
	"net/http"
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
