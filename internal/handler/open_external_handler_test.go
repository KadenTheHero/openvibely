package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestOpenExternal_ServerMode_Returns404(t *testing.T) {
	h := &Handler{desktopMode: false}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/open-external?url=https://github.com/org/repo/pull/1", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := h.OpenExternal(c)
	if err == nil {
		t.Fatal("expected an error (echo.ErrNotFound) in server mode, got nil")
	}
	he, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("expected *echo.HTTPError, got %T: %v", err, err)
	}
	if he.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", he.Code)
	}
}

func TestOpenExternal_DesktopMode_MissingURL_Returns400(t *testing.T) {
	h := &Handler{desktopMode: true}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/open-external", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := h.OpenExternal(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestOpenExternal_DesktopMode_InvalidScheme_Returns400(t *testing.T) {
	h := &Handler{desktopMode: true}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/open-external?url=javascript:alert(1)", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := h.OpenExternal(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for non-http(s) scheme, got %d", rec.Code)
	}
}

func TestOpenExternal_DesktopMode_ValidURL_Opens(t *testing.T) {
	var openedURL string
	orig := openExternalURL
	defer func() { openExternalURL = orig }()
	openExternalURL = func(url string) error {
		openedURL = url
		return nil
	}

	h := &Handler{desktopMode: true}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/open-external?url=https://github.com/org/repo/pull/7", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := h.OpenExternal(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", rec.Code)
	}
	if openedURL != "https://github.com/org/repo/pull/7" {
		t.Errorf("expected opened URL to be the PR URL, got %q", openedURL)
	}
}

func TestOpenExternal_DesktopMode_OpenError_Returns500(t *testing.T) {
	orig := openExternalURL
	defer func() { openExternalURL = orig }()
	openExternalURL = func(url string) error {
		return &testOpenError{"no browser available"}
	}

	h := &Handler{desktopMode: true}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/open-external?url=https://github.com/org/repo/pull/7", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := h.OpenExternal(c); err != nil {
		t.Fatalf("unexpected echo error: %v", err)
	}
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

type testOpenError struct{ msg string }

func (e *testOpenError) Error() string { return e.msg }
