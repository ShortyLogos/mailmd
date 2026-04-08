package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

func NewOAuthConfig(clientID, clientSecret, redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Scopes: []string{
			"https://www.googleapis.com/auth/gmail.modify",
			"https://www.googleapis.com/auth/gmail.send",
		},
		Endpoint: google.Endpoint,
	}
}

func Authenticate(ctx context.Context, clientID, clientSecret string, store *TokenStore) (*http.Client, error) {
	if store.Exists() {
		token, err := store.Load()
		if err == nil {
			cfg := NewOAuthConfig(clientID, clientSecret, "")
			client := cfg.Client(ctx, token)
			return client, nil
		}
	}

	token, err := browserFlow(ctx, clientID, clientSecret)
	if err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	if err := store.Save(token); err != nil {
		return nil, fmt.Errorf("failed to save token: %w", err)
	}

	cfg := NewOAuthConfig(clientID, clientSecret, "")
	return cfg.Client(ctx, token), nil
}

func browserFlow(ctx context.Context, clientID, clientSecret string) (*oauth2.Token, error) {
	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return nil, fmt.Errorf("failed to start callback server: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	redirectURL := fmt.Sprintf("http://localhost:%d/callback", port)

	cfg := NewOAuthConfig(clientID, clientSecret, redirectURL)

	state, err := randomState()
	if err != nil {
		ln.Close()
		return nil, err
	}

	codeCh := make(chan string, 1)
	mux := http.NewServeMux()
	mux.Handle("/callback", callbackHandler(codeCh))

	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	defer srv.Shutdown(ctx)

	authURL := cfg.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)
	fmt.Printf("Opening browser for authentication...\n")
	fmt.Printf("If the browser doesn't open, visit:\n%s\n", authURL)
	openBrowser(authURL)

	var code string
	select {
	case code = <-codeCh:
	case <-time.After(2 * time.Minute):
		return nil, fmt.Errorf("authentication timed out after 2 minutes")
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	token, err := cfg.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("token exchange failed: %w", err)
	}

	return token, nil
}

func callbackHandler(codeCh chan<- string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing code parameter", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html><body><h1>Authentication successful!</h1><p>You can close this tab and return to your terminal.</p></body></html>"))

		codeCh <- code
	})
}

func randomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate state: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		return
	}
	cmd.Start()
}
