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
	editForm.Set("project_id", otherProject.ID)
	editForm.Add("target_row_id", target.ID)
	editForm.Add("target_platform", "slack")
	editForm.Add("target_name", "stolen")
	editForm.Add("target_target_id", "COTHER")
	editForm.Add("target_thread_id", "")
	editForm.Add("target_is_home", "false")
	editForm.Add("target_default_subject", "")
	editReq := httptest.NewRequest(http.MethodPost, "/channels/send-message-explicit-targets", strings.NewReader(editForm.Encode()))
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

	deletePath := "/channels/outbound-targets/" + target.ID + "?project_id=" + url.QueryEscape(ownerProject.ID)
	deleteRec := tc.HTMX().Delete(deletePath).Execute()
	if deleteRec.Code != http.StatusNotFound {
		t.Fatalf("expected direct delete route to be unavailable so deletes require Save Settings, got %d body=%s", deleteRec.Code, deleteRec.Body.String())
	}
	stored, err = targetRepo.GetByID(context.Background(), target.ID)
	if err != nil || stored == nil {
		t.Fatalf("target should remain after unavailable direct delete, target=%v err=%v", stored, err)
	}

	testPath := "/channels/outbound-targets/" + target.ID + "/test?project_id=" + url.QueryEscape(otherProject.ID)
	testRec := tc.HTMX().Post(testPath).Execute()
	if testRec.Code != http.StatusNotFound {
		t.Fatalf("expected cross-project test send to be denied with 404, got %d body=%s", testRec.Code, testRec.Body.String())
	}
	if slack.channelID != "" || slack.text != "" {
		t.Fatalf("cross-project test send should not dispatch, channel=%q text=%q", slack.channelID, slack.text)
	}

	otherTarget := models.ChannelTarget{ID: repository.NewID(), ProjectID: otherProject.ID, Platform: "slack", Name: "other", TargetID: "COTHER"}
	if err := targetRepo.Upsert(context.Background(), otherTarget); err != nil {
		t.Fatalf("upsert other target: %v", err)
	}
	validThenInvalid := url.Values{}
	validThenInvalid.Set("project_id", ownerProject.ID)
	validThenInvalid.Add("target_row_id", target.ID)
	validThenInvalid.Add("target_platform", "slack")
	validThenInvalid.Add("target_name", "updated")
	validThenInvalid.Add("target_target_id", "CUPDATED")
	validThenInvalid.Add("target_thread_id", "")
	validThenInvalid.Add("target_is_home", "false")
	validThenInvalid.Add("target_default_subject", "")
	validThenInvalid.Add("target_row_id", otherTarget.ID)
	validThenInvalid.Add("target_platform", "slack")
	validThenInvalid.Add("target_name", "stolen")
	validThenInvalid.Add("target_target_id", "COTHER")
	validThenInvalid.Add("target_thread_id", "")
	validThenInvalid.Add("target_is_home", "false")
	validThenInvalid.Add("target_default_subject", "")
	invalidReq := httptest.NewRequest(http.MethodPost, "/channels/send-message-explicit-targets", strings.NewReader(validThenInvalid.Encode()))
	invalidReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	invalidRec := httptest.NewRecorder()
	tc.echo.ServeHTTP(invalidRec, invalidReq)
	if invalidRec.Code != http.StatusNotFound {
		t.Fatalf("expected draft save with cross-project row to be denied with 404, got %d body=%s", invalidRec.Code, invalidRec.Body.String())
	}
	stored, err = targetRepo.GetByID(context.Background(), target.ID)
	if err != nil || stored == nil || stored.ProjectID != ownerProject.ID || stored.TargetID != "COWNER" || stored.Name != "alerts" {
		t.Fatalf("valid rows before an invalid draft row must not be persisted, target=%+v err=%v", stored, err)
	}
}

func TestOutboundTargetsPersistOnlyOnSaveSettings(t *testing.T) {
	tc := NewTestContext(t)
	project := tc.CreateProject().WithName("Staged Outbound Targets").Build()
	targetRepo := repository.NewChannelTargetRepo(tc.db)
	tc.handler.SetChannelTargetRepo(targetRepo)

	keep := models.ChannelTarget{ID: repository.NewID(), ProjectID: project.ID, Platform: "email", Name: "keep", TargetID: "keep@example.com"}
	remove := models.ChannelTarget{ID: repository.NewID(), ProjectID: project.ID, Platform: "slack", Name: "remove", TargetID: "CREMOVE"}
	if err := targetRepo.Upsert(context.Background(), keep); err != nil {
		t.Fatalf("upsert keep target: %v", err)
	}
	if err := targetRepo.Upsert(context.Background(), remove); err != nil {
		t.Fatalf("upsert remove target: %v", err)
	}

	cardBefore := tc.HTTP().Get("/channels/outbound-targets/card?project_id=" + url.QueryEscape(project.ID)).Execute()
	if cardBefore.Code != http.StatusOK || !strings.Contains(cardBefore.Body.String(), "email: 1") || !strings.Contains(cardBefore.Body.String(), "slack: 1") || !strings.Contains(cardBefore.Body.String(), "Saved targets only") {
		t.Fatalf("expected persisted card before save, status=%d body=%s", cardBefore.Code, cardBefore.Body.String())
	}
	// Simulating client-side draft add/delete/toggle without submitting Save Settings: persisted state is unchanged.
	cardStill := tc.HTTP().Get("/channels/outbound-targets/card?project_id=" + url.QueryEscape(project.ID)).Execute()
	if cardStill.Code != http.StatusOK || !strings.Contains(cardStill.Body.String(), "email: 1") || !strings.Contains(cardStill.Body.String(), "slack: 1") || !strings.Contains(cardStill.Body.String(), "Saved targets only") {
		t.Fatalf("expected unsaved draft changes to be discarded, status=%d body=%s", cardStill.Code, cardStill.Body.String())
	}

	form := url.Values{}
	form.Set("project_id", project.ID)
	form.Set("enabled", "true")
	form.Add("target_row_id", keep.ID)
	form.Add("target_platform", "email")
	form.Add("target_name", "keep")
	form.Add("target_target_id", "keep@example.com")
	form.Add("target_thread_id", "")
	form.Add("target_is_home", "false")
	form.Add("target_default_subject", "")
	form.Add("target_row_id", "")
	form.Add("target_platform", "email")
	form.Add("target_name", "")
	form.Add("target_target_id", "billing@example.com")
	form.Add("target_thread_id", "")
	form.Add("target_is_home", "false")
	form.Add("target_default_subject", "")
	form.Add("target_row_id", "")
	form.Add("target_platform", "discord")
	form.Add("target_name", "ops")
	form.Add("target_target_id", "123456789")
	form.Add("target_thread_id", "987654321")
	form.Add("target_is_home", "false")
	form.Add("target_default_subject", "")
	req := httptest.NewRequest(http.MethodPost, "/channels/send-message-explicit-targets", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	tc.echo.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected save status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("HX-Trigger") != "outbound-targets-card-refresh" {
		t.Fatalf("expected card refresh trigger, got %q", rec.Header().Get("HX-Trigger"))
	}

	targets, err := targetRepo.ListByProject(context.Background(), project.ID)
	if err != nil {
		t.Fatalf("list targets after save: %v", err)
	}
	if len(targets) != 3 {
		t.Fatalf("expected save to reconcile target list to three rows, got %+v", targets)
	}
	var sawKeep, sawNew, sawDiscord, sawRemoved bool
	for _, target := range targets {
		sawKeep = sawKeep || target.ID == keep.ID
		sawNew = sawNew || target.Platform == "email" && target.Name == "" && target.TargetID == "billing@example.com"
		sawDiscord = sawDiscord || target.Platform == "discord" && target.Name == "ops" && target.TargetID == "123456789" && target.ThreadID == "987654321"
		sawRemoved = sawRemoved || target.ID == remove.ID
	}
	if !sawKeep || !sawNew || !sawDiscord || sawRemoved {
		t.Fatalf("unexpected reconciled targets: keep=%v new=%v discord=%v removed=%v targets=%+v", sawKeep, sawNew, sawDiscord, sawRemoved, targets)
	}

	cardAfter := tc.HTTP().Get("/channels/outbound-targets/card?project_id=" + url.QueryEscape(project.ID)).Execute()
	if cardAfter.Code != http.StatusOK || !strings.Contains(cardAfter.Body.String(), "email: 2") || !strings.Contains(cardAfter.Body.String(), "discord: 1") || !strings.Contains(cardAfter.Body.String(), "Explicit targets allowed") || strings.Contains(cardAfter.Body.String(), "slack: 1") {
		t.Fatalf("expected card after save to reflect reconciled targets and policy, status=%d body=%s", cardAfter.Code, cardAfter.Body.String())
	}

	duplicateForm := url.Values{}
	duplicateForm.Set("project_id", project.ID)
	duplicateForm.Add("target_row_id", keep.ID)
	duplicateForm.Add("target_platform", "email")
	duplicateForm.Add("target_name", "keep")
	duplicateForm.Add("target_target_id", "keep@example.com")
	duplicateForm.Add("target_thread_id", "")
	duplicateForm.Add("target_is_home", "false")
	duplicateForm.Add("target_default_subject", "")
	duplicateForm.Add("target_row_id", "")
	duplicateForm.Add("target_platform", "email")
	duplicateForm.Add("target_name", "")
	duplicateForm.Add("target_target_id", "keep@example.com")
	duplicateForm.Add("target_thread_id", "")
	duplicateForm.Add("target_is_home", "false")
	duplicateForm.Add("target_default_subject", "")
	duplicateReq := httptest.NewRequest(http.MethodPost, "/channels/send-message-explicit-targets", strings.NewReader(duplicateForm.Encode()))
	duplicateReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	duplicateRec := httptest.NewRecorder()
	tc.echo.ServeHTTP(duplicateRec, duplicateReq)
	if duplicateRec.Code != http.StatusOK || duplicateRec.Header().Get("HX-Trigger") != "outbound-targets-save-error" || !strings.Contains(duplicateRec.Body.String(), "Duplicate outbound target destination") {
		t.Fatalf("expected inline duplicate destination validation, got %d trigger=%q body=%s", duplicateRec.Code, duplicateRec.Header().Get("HX-Trigger"), duplicateRec.Body.String())
	}
	targetsAfterDuplicate, err := targetRepo.ListByProject(context.Background(), project.ID)
	if err != nil || len(targetsAfterDuplicate) != 3 {
		t.Fatalf("duplicate validation should not mutate saved targets, targets=%+v err=%v", targetsAfterDuplicate, err)
	}
}

func TestOutboundTargetDraftTestSendsWithoutPersisting(t *testing.T) {
	tc := NewTestContext(t)
	project := tc.CreateProject().WithName("Outbound Draft Test Project").Build()
	targetRepo := repository.NewChannelTargetRepo(tc.db)
	slack := &outboundTargetTestSlack{}
	router := service.NewChannelMessageRouter(targetRepo, tc.settingsRepo)
	router.SetSlackService(slack)
	tc.handler.SetChannelTargetRepo(targetRepo)
	tc.handler.SetChannelMessageRouter(router)

	form := url.Values{}
	form.Set("project_id", project.ID)
	form.Set("target_platform", "slack")
	form.Set("target_target_id", "CDRAFT")
	form.Set("target_thread_id", "1690000000.000000")
	form.Set("target_default_subject", "")
	req := httptest.NewRequest(http.MethodPost, "/channels/outbound-targets/test-draft", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	tc.echo.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if slack.channelID != "CDRAFT" || slack.threadTS != "1690000000.000000" || slack.text != "Test message from OpenVibely" {
		t.Fatalf("unexpected draft test send channel=%q thread=%q text=%q", slack.channelID, slack.threadTS, slack.text)
	}
	targets, err := targetRepo.ListByProject(context.Background(), project.ID)
	if err != nil || len(targets) != 0 {
		t.Fatalf("draft test must not persist targets, targets=%+v err=%v", targets, err)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `>✓</span><span>Sent</span>`) || !strings.Contains(body, `text-success`) || strings.Contains(body, "&#34;ok&#34;") || strings.Contains(body, `{"ok"`) || strings.Contains(body, `alert alert-success`) {
		t.Fatalf("expected compact button-local success result with green check and without raw JSON or banner, got %q", body)
	}
}

func TestOutboundTargetDraftTestRendersCleanFailure(t *testing.T) {
	tc := NewTestContext(t)
	project := tc.CreateProject().WithName("Outbound Draft Failure Project").Build()
	targetRepo := repository.NewChannelTargetRepo(tc.db)
	router := service.NewChannelMessageRouter(targetRepo, tc.settingsRepo)
	tc.handler.SetChannelTargetRepo(targetRepo)
	tc.handler.SetChannelMessageRouter(router)

	form := url.Values{}
	form.Set("project_id", project.ID)
	form.Set("target_platform", "email")
	form.Set("target_target_id", "draft@example.com")
	req := httptest.NewRequest(http.MethodPost, "/channels/outbound-targets/test-draft", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	tc.echo.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `>✕</span><span>Failed</span>`) || !strings.Contains(body, `text-error`) || !strings.Contains(body, `title="Test failed:`) || strings.Contains(body, `{"ok"`) || strings.Contains(body, "&#34;ok&#34;") || strings.Contains(body, `alert alert-error`) {
		t.Fatalf("expected compact button-local failure result with red x and without raw JSON or banner, got %q", body)
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
	if !strings.Contains(body, `>✓</span><span>Sent</span>`) || !strings.Contains(body, `text-success`) {
		t.Fatalf("unexpected body %q", body)
	}
	if strings.Contains(body, "slack:C123:1690000000.000000") || strings.Contains(body, `{"ok":true`) || strings.Contains(body, "&#34;ok&#34;") || strings.Contains(body, `alert alert-success`) {
		t.Fatalf("expected compact button-local success result with green check and without transport JSON or banner, got %q", body)
	}
}
