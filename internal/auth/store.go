package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/oauth2"
)

// AccountTokenPath returns the token file path for a specific account email.
func AccountTokenPath(configDir, email string) string {
	safe := strings.ReplaceAll(email, "@", "_at_")
	safe = strings.ReplaceAll(safe, ".", "_")
	return filepath.Join(configDir, "mailmd", "tokens", safe+".json")
}

type TokenStore struct {
	path string
}

func NewTokenStore(path string) *TokenStore {
	return &TokenStore{path: path}
}

func (s *TokenStore) Path() string {
	return s.path
}

func (s *TokenStore) Exists() bool {
	_, err := os.Stat(s.path)
	return err == nil
}

func (s *TokenStore) Load() (*oauth2.Token, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, fmt.Errorf("failed to read token file: %w", err)
	}

	var token oauth2.Token
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, fmt.Errorf("failed to parse token file: %w", err)
	}

	return &token, nil
}

func (s *TokenStore) Save(token *oauth2.Token) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return fmt.Errorf("failed to create token directory: %w", err)
	}

	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal token: %w", err)
	}

	if err := os.WriteFile(s.path, data, 0600); err != nil {
		return fmt.Errorf("failed to write token file: %w", err)
	}

	return nil
}
