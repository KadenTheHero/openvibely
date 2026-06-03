package anthropicclient

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRefreshTokenFailureRedactsResponseBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","refresh_token":"secret-refresh","access_token":"secret-access"}`))
	}))
	defer srv.Close()

	oldURL := OAuthTokenURL
	OAuthTokenURL = srv.URL
	defer func() { OAuthTokenURL = oldURL }()

	_, err := RefreshToken("secret-refresh")
	if err == nil {
		t.Fatal("expected refresh failure")
	}
	message := err.Error()
	for _, forbidden := range []string{"secret-refresh", "secret-access", "invalid_grant", "refresh_token", "access_token"} {
		if strings.Contains(message, forbidden) {
			t.Fatalf("refresh failure leaked %q in error: %s", forbidden, message)
		}
	}
	if !strings.Contains(message, "HTTP 401") {
		t.Fatalf("refresh failure should include status code, got %s", message)
	}
}
