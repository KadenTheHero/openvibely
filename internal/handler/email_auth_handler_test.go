package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/openvibely/openvibely/internal/models"
	"github.com/openvibely/openvibely/internal/repository"
)

func TestEmailAuthorizedSendersHandlers(t *testing.T) {
	h, e, _, db := setupTestHandlerWithDB(t)
	h.SetEmailAuthRepo(repository.NewEmailAuthRepo(db))
	project := createProject(t, h, "Email Auth UI")

	rec := htmxGet(e, "/channels/email/authorized-senders?project_id="+project.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected list 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "No authorized senders configured. Access is denied until senders are added.") {
		t.Fatal("expected deny empty state")
	}

	form := url.Values{"project_id": {project.ID}, "email_address": {"Alice@Example.COM"}, "display_name": {"Alice"}}
	rec = postForm(e, "/channels/email/authorized-senders", form)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected add 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "alice@example.com") {
		t.Fatal("expected normalized sender in response")
	}

	senders, err := h.emailAuthRepo.ListByProject(httptest.NewRequest(http.MethodGet, "/", nil).Context(), project.ID)
	if err != nil || len(senders) != 1 {
		t.Fatalf("expected one sender, got %d err=%v", len(senders), err)
	}
	req := httptest.NewRequest(http.MethodDelete, "/channels/email/authorized-senders/"+senders[0].ID+"?project_id="+project.ID, nil)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected remove 200, got %d", rec.Code)
	}
}

func TestEmailAuthorizedSendersValidation(t *testing.T) {
	_, e, _, _ := setupTestHandlerWithDB(t)
	rec := postForm(e, "/channels/email/authorized-senders", url.Values{"email_address": {"a@example.com"}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected missing project 400, got %d", rec.Code)
	}
	rec = postForm(e, "/channels/email/authorized-senders", url.Values{"project_id": {"default"}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected missing address 400, got %d", rec.Code)
	}
}

func TestEmailConfigurePresetsRemove(t *testing.T) {
	h, e, _, _ := setupTestHandlerWithDB(t)
	form := url.Values{"email_provider": {"gmail"}, "email_address": {"bot@example.com"}, "email_password": {"secret"}, "email_send_responses": {"true"}, "email_mark_existing_seen_on_start": {"true"}}
	req := httptest.NewRequest(http.MethodPost, "/channels/email/configure", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected configure 200, got %d %s", rec.Code, rec.Body.String())
	}
	imapHost, _ := h.settingsRepo.Get(req.Context(), "email_imap_host")
	smtpHost, _ := h.settingsRepo.Get(req.Context(), "email_smtp_host")
	if imapHost != "imap.gmail.com" || smtpHost != "smtp.gmail.com" {
		t.Fatalf("expected gmail hosts, got %q %q", imapHost, smtpHost)
	}

	custom := url.Values{"email_provider": {"custom"}, "email_address": {"bot@example.com"}}
	req = httptest.NewRequest(http.MethodPost, "/channels/email/configure", strings.NewReader(custom.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected custom missing host 400, got %d", rec.Code)
	}

	removeReq := httptest.NewRequest(http.MethodPost, "/channels/email/remove", nil)
	removeReq.Header.Set("HX-Request", "true")
	removeRec := httptest.NewRecorder()
	e.ServeHTTP(removeRec, removeReq)
	if removeRec.Code != http.StatusOK {
		t.Fatalf("expected remove 200, got %d", removeRec.Code)
	}
	password, _ := h.settingsRepo.Get(removeReq.Context(), "email_password")
	if password != "" {
		t.Fatal("expected email password cleared")
	}
}

func TestChannelsPageEmailUI(t *testing.T) {
	h, e, _, _ := setupTestHandlerWithDB(t)
	_ = h.settingsRepo.Set(httptest.NewRequest(http.MethodGet, "/", nil).Context(), "email_address", "bot@example.com")
	_ = h.settingsRepo.Set(httptest.NewRequest(http.MethodGet, "/", nil).Context(), "email_password", "secret")
	_ = h.emailAuthRepo.Create(httptest.NewRequest(http.MethodGet, "/", nil).Context(), &models.EmailAuthorizedSender{ProjectID: "default", EmailAddress: "alice@example.com", AddedBy: "test"})

	req := httptest.NewRequest(http.MethodGet, "/channels?project_id=default", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected channels 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"data-channel-type=\"email\"", "Configure Email", "Gmail", "Outlook / Microsoft 365", "Use a Google app password", "Authorized Senders", "1 sender(s)", "person@example.com"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected email UI to contain %q", want)
		}
	}
	if strings.Contains(body, "pairing") || strings.Contains(body, "PIN") {
		t.Fatal("email UI should not include pairing/pin language")
	}
}
