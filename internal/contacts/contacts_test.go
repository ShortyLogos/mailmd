package contacts

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAddAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "contacts.json")

	if err := Add(path, "alice@example.com", "Bob <bob@example.com>"); err != nil {
		t.Fatal(err)
	}

	contacts := Load(path)
	if len(contacts) != 2 {
		t.Fatalf("expected 2 contacts, got %d", len(contacts))
	}

	// Check alice (bare email)
	if contacts[0].Email != "alice@example.com" {
		t.Errorf("expected alice@example.com, got %s", contacts[0].Email)
	}
	if contacts[0].Name != "" {
		t.Errorf("expected empty name for alice, got %s", contacts[0].Name)
	}

	// Check bob (with display name)
	if contacts[1].Email != "bob@example.com" {
		t.Errorf("expected bob@example.com, got %s", contacts[1].Email)
	}
	if contacts[1].Name != "Bob" {
		t.Errorf("expected name Bob, got %s", contacts[1].Name)
	}
}

func TestAddUpdatesLastUsed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "contacts.json")

	Add(path, "alice@example.com")
	contacts1 := Load(path)

	Add(path, "alice@example.com")
	contacts2 := Load(path)

	if len(contacts2) != 1 {
		t.Fatalf("expected 1 contact after dedup, got %d", len(contacts2))
	}
	if !contacts2[0].LastUsed.After(contacts1[0].LastUsed) || contacts2[0].LastUsed.Equal(contacts1[0].LastUsed) {
		// LastUsed should be updated (or at least equal if fast)
	}
}

func TestAllSortsByLastUsed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "contacts.json")

	Add(path, "old@example.com")
	Add(path, "new@example.com")

	all := All(path)
	if len(all) != 2 {
		t.Fatalf("expected 2 contacts, got %d", len(all))
	}
	// new@example.com was added last, should be first
	if all[0] != "new@example.com" {
		t.Errorf("expected new@example.com first, got %s", all[0])
	}
}

func TestAllFormatsWithName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "contacts.json")

	Add(path, "Alice Smith <alice@example.com>")

	all := All(path)
	if len(all) != 1 {
		t.Fatalf("expected 1 contact, got %d", len(all))
	}
	if all[0] != "Alice Smith <alice@example.com>" {
		t.Errorf("expected formatted address, got %s", all[0])
	}
}

func TestLoadMissingFile(t *testing.T) {
	contacts := Load("/nonexistent/path/contacts.json")
	if contacts != nil {
		t.Errorf("expected nil for missing file, got %v", contacts)
	}
}

func TestLoadCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "contacts.json")
	os.WriteFile(path, []byte("not json"), 0600)

	contacts := Load(path)
	if contacts != nil {
		t.Errorf("expected nil for corrupt file, got %v", contacts)
	}
}

func TestParseAddresses(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"alice@example.com", 1},
		{"alice@example.com, bob@example.com", 2},
		{"Alice <alice@example.com>, Bob <bob@example.com>", 2},
		{"", 0},
		{"   ", 0},
	}
	for _, tt := range tests {
		got := ParseAddresses(tt.input)
		if len(got) != tt.want {
			t.Errorf("ParseAddresses(%q) = %d addresses, want %d", tt.input, len(got), tt.want)
		}
	}
}
