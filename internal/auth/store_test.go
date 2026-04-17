package auth

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestSaveAndLoadToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tokens.json")
	store := NewTokenStore(path)

	token := &oauth2.Token{
		AccessToken:  "access-123",
		RefreshToken: "refresh-456",
		TokenType:    "Bearer",
		Expiry:       time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC),
	}

	if err := store.Save(token); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.AccessToken != "access-123" {
		t.Errorf("expected access token 'access-123', got %q", loaded.AccessToken)
	}
	if loaded.RefreshToken != "refresh-456" {
		t.Errorf("expected refresh token 'refresh-456', got %q", loaded.RefreshToken)
	}
}

func TestSaveCreatesFileWith0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tokens.json")
	store := NewTokenStore(path)

	token := &oauth2.Token{AccessToken: "test", TokenType: "Bearer"}
	if err := store.Save(token); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected 0600 permissions, got %o", info.Mode().Perm())
	}
}

func TestLoadNonExistentFile(t *testing.T) {
	store := NewTokenStore("/nonexistent/path/tokens.json")
	_, err := store.Load()
	if err == nil {
		t.Error("expected error loading nonexistent file")
	}
}

func TestDeleteRemovesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tokens.json")
	store := NewTokenStore(path)

	token := &oauth2.Token{AccessToken: "test", TokenType: "Bearer"}
	if err := store.Save(token); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected file to be gone, stat err = %v", err)
	}
}

func TestDeleteMissingFileIsNoError(t *testing.T) {
	store := NewTokenStore(filepath.Join(t.TempDir(), "missing.json"))
	if err := store.Delete(); err != nil {
		t.Errorf("Delete on missing file: %v", err)
	}
}

func TestSaveCreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "dir", "tokens.json")
	store := NewTokenStore(path)

	token := &oauth2.Token{AccessToken: "test", TokenType: "Bearer"}
	if err := store.Save(token); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Error("expected file to exist")
	}
}
