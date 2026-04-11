package contacts

import (
	"encoding/json"
	"net/mail"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Contact represents a known email contact.
type Contact struct {
	Email    string    `json:"email"`
	Name     string    `json:"name,omitempty"`
	LastUsed time.Time `json:"last_used"`
}

// Load reads the contacts file. Returns an empty slice on any error.
func Load(path string) []Contact {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var contacts []Contact
	if err := json.Unmarshal(data, &contacts); err != nil {
		return nil
	}
	return contacts
}

// Save writes contacts to disk.
func Save(path string, contacts []Contact) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(contacts, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// Add parses RFC 5322 addresses, merges them into the existing contacts file,
// and saves. Existing contacts get their LastUsed updated; new ones are appended.
func Add(path string, addresses ...string) error {
	existing := Load(path)
	byEmail := make(map[string]int, len(existing))
	for i, c := range existing {
		byEmail[strings.ToLower(c.Email)] = i
	}

	now := time.Now()
	for _, addr := range addresses {
		email, name := parseAddress(addr)
		if email == "" {
			continue
		}
		key := strings.ToLower(email)
		if idx, ok := byEmail[key]; ok {
			existing[idx].LastUsed = now
			if name != "" && existing[idx].Name == "" {
				existing[idx].Name = name
			}
		} else {
			byEmail[key] = len(existing)
			existing = append(existing, Contact{
				Email:    email,
				Name:     name,
				LastUsed: now,
			})
		}
	}

	return Save(path, existing)
}

// All returns all contacts formatted as "Name <email>" (or just "email"),
// sorted by LastUsed descending (most recent first).
func All(path string) []string {
	contacts := Load(path)
	sort.Slice(contacts, func(i, j int) bool {
		return contacts[i].LastUsed.After(contacts[j].LastUsed)
	})
	result := make([]string, 0, len(contacts))
	for _, c := range contacts {
		if c.Name != "" {
			result = append(result, c.Name+" <"+c.Email+">")
		} else {
			result = append(result, c.Email)
		}
	}
	return result
}

// parseAddress extracts email and name from an RFC 5322 address string.
// Falls back to treating the whole string as an email if parsing fails.
func parseAddress(s string) (email, name string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", ""
	}
	addr, err := mail.ParseAddress(s)
	if err != nil {
		// Fallback: treat as bare email if it contains @
		if strings.Contains(s, "@") {
			return s, ""
		}
		return "", ""
	}
	return addr.Address, addr.Name
}

// ParseAddresses splits a comma-separated address string and parses each one.
func ParseAddresses(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	addrs, err := mail.ParseAddressList(s)
	if err != nil {
		// Fallback: split by comma and parse individually
		parts := strings.Split(s, ",")
		var result []string
		for _, p := range parts {
			email, _ := parseAddress(p)
			if email != "" {
				result = append(result, email)
			}
		}
		return result
	}
	result := make([]string, 0, len(addrs))
	for _, a := range addrs {
		result = append(result, a.Address)
	}
	return result
}
