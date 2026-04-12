package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	cfg := Default()

	if cfg.General.Theme != "default" {
		t.Errorf("expected theme 'default', got %q", cfg.General.Theme)
	}
	if cfg.Preview.Browser != "default" {
		t.Errorf("expected browser 'default', got %q", cfg.Preview.Browser)
	}
	if cfg.Keybindings.Compose != "c" {
		t.Errorf("expected compose key 'c', got %q", cfg.Keybindings.Compose)
	}
}

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `
[general]
theme = "dark"

[keybindings]
compose = "n"

[preview]
browser = "firefox"
`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.General.Theme != "dark" {
		t.Errorf("expected theme 'dark', got %q", cfg.General.Theme)
	}
	if cfg.Keybindings.Compose != "n" {
		t.Errorf("expected compose key 'n', got %q", cfg.Keybindings.Compose)
	}
	if cfg.Preview.Browser != "firefox" {
		t.Errorf("expected browser 'firefox', got %q", cfg.Preview.Browser)
	}
}

func TestCreateDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.General.Theme != "default" {
		t.Errorf("expected default theme")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal("config file not created")
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected 0600 permissions, got %o", info.Mode().Perm())
	}
}

func TestEditorFallback(t *testing.T) {
	cfg := Default()

	t.Setenv("EDITOR", "nvim")
	editor := cfg.Editor()
	if editor != "nvim" {
		t.Errorf("expected 'nvim', got %q", editor)
	}
}

func TestMigrateSignatures(t *testing.T) {
	cfg := Config{
		Accounts: []Account{
			{Name: "Test", Email: "a@b.com", Signature: "-- old sig"},
			{Name: "NoSig", Email: "c@d.com"},
		},
	}
	migrated := migrateSignatures(&cfg)
	if !migrated {
		t.Fatal("expected migration")
	}
	if cfg.Accounts[0].Signature != "" {
		t.Error("old signature field should be cleared")
	}
	if len(cfg.Accounts[0].Signatures) != 1 {
		t.Fatal("expected 1 signature")
	}
	sig := cfg.Accounts[0].Signatures[0]
	if sig.Name != "Default" || sig.Body != "-- old sig" || !sig.IsDefault {
		t.Errorf("unexpected signature: %+v", sig)
	}
	if len(cfg.Accounts[1].Signatures) != 0 {
		t.Error("account without signature should stay empty")
	}
}
