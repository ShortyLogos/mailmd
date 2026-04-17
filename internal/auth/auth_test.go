package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/oauth2"
)

func TestOAuthConfigScopes(t *testing.T) {
	cfg := NewOAuthConfig("client-id", "client-secret", "http://localhost:9999/callback")

	expectedScopes := []string{
		"https://www.googleapis.com/auth/gmail.modify",
		"https://www.googleapis.com/auth/gmail.send",
		"https://www.googleapis.com/auth/gmail.settings.basic",
	}

	if len(cfg.Scopes) != len(expectedScopes) {
		t.Fatalf("expected %d scopes, got %d", len(expectedScopes), len(cfg.Scopes))
	}
	for i, s := range expectedScopes {
		if cfg.Scopes[i] != s {
			t.Errorf("scope[%d]: expected %q, got %q", i, s, cfg.Scopes[i])
		}
	}
}

func TestCallbackServerCapturesCode(t *testing.T) {
	codeCh := make(chan string, 1)
	handler := callbackHandler(codeCh)

	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "?code=test-auth-code&state=test-state")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	code := <-codeCh
	if code != "test-auth-code" {
		t.Errorf("expected code 'test-auth-code', got %q", code)
	}
}

func TestIsInvalidGrant(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"RetrieveError invalid_grant", &oauth2.RetrieveError{ErrorCode: "invalid_grant"}, true},
		{"RetrieveError other", &oauth2.RetrieveError{ErrorCode: "invalid_client"}, false},
		{"wrapped RetrieveError", fmt.Errorf("token: %w", &oauth2.RetrieveError{ErrorCode: "invalid_grant"}), true},
		{"substring fallback", errors.New(`oauth2: "invalid_grant" Token has been expired or revoked.`), true},
		{"unrelated error", errors.New("network unreachable"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isInvalidGrant(tc.err); got != tc.want {
				t.Errorf("isInvalidGrant(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestExchangeToken(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"mock-access","token_type":"Bearer","refresh_token":"mock-refresh","expiry":"2026-12-01T00:00:00Z"}`))
	}))
	defer tokenSrv.Close()

	cfg := &oauth2.Config{
		ClientID:     "test-id",
		ClientSecret: "test-secret",
		Endpoint: oauth2.Endpoint{
			TokenURL: tokenSrv.URL,
		},
	}

	token, err := cfg.Exchange(context.Background(), "test-code")
	if err != nil {
		t.Fatal(err)
	}
	if token.AccessToken != "mock-access" {
		t.Errorf("expected 'mock-access', got %q", token.AccessToken)
	}
}
