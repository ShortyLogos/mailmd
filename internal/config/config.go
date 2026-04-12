package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Signature struct {
	Name      string `toml:"name"`
	Body      string `toml:"body"`
	IsDefault bool   `toml:"is_default,omitempty"`
}

type Account struct {
	Name       string      `toml:"name"`
	Email      string      `toml:"email"`
	Signature  string      `toml:"signature,omitempty"`  // deprecated, migrated on load
	Signatures []Signature `toml:"signatures,omitempty"`
}

type Template struct {
	Subject string `toml:"subject,omitempty"`
	Body    string `toml:"body"`
}

type Config struct {
	General       General              `toml:"general"`
	Keybindings   Keybindings          `toml:"keybindings"`
	Preview       Preview              `toml:"preview"`
	Accounts      []Account            `toml:"accounts"`
	LastAccount   string               `toml:"last_account,omitempty"`
	ContactGroups map[string][]string   `toml:"contact_groups,omitempty"`
	Templates     map[string]Template   `toml:"templates,omitempty"`
}

type General struct {
	EditorCmd string `toml:"editor"`
	Theme     string `toml:"theme"`
}

type Keybindings struct {
	Compose string `toml:"compose"`
	Reply   string `toml:"reply"`
	Forward string `toml:"forward"`
	Trash   string `toml:"trash"`
}

type Preview struct {
	Browser string `toml:"browser"`
}

func Default() Config {
	return Config{
		General: General{
			Theme: "default",
		},
		Keybindings: Keybindings{
			Compose: "c",
			Reply:   "r",
			Forward: "f",
			Trash:   "d",
		},
		Preview: Preview{
			Browser: "default",
		},
	}
}

func (c Config) Editor() string {
	if c.General.EditorCmd != "" {
		return c.General.EditorCmd
	}
	if editor := os.Getenv("EDITOR"); editor != "" {
		return editor
	}
	return "vi"
}

func Load(path string) (Config, error) {
	cfg := Default()
	_, err := toml.DecodeFile(path, &cfg)
	if err != nil {
		return Config{}, err
	}
	cfg.Accounts = deduplicateAccounts(cfg.Accounts)
	if migrateSignatures(&cfg) {
		_ = save(path, cfg) // persist migration, ignore error
	}
	return cfg, nil
}

// deduplicateAccounts removes duplicate accounts (by email), keeping the last occurrence.
func deduplicateAccounts(accounts []Account) []Account {
	seen := make(map[string]int) // email → index in result
	var result []Account
	for _, a := range accounts {
		if idx, ok := seen[a.Email]; ok {
			result[idx] = a // update with latest name
		} else {
			seen[a.Email] = len(result)
			result = append(result, a)
		}
	}
	return result
}

// migrateSignatures converts old single Signature field to Signatures slice.
// Returns true if any migration occurred.
func migrateSignatures(cfg *Config) bool {
	migrated := false
	for i := range cfg.Accounts {
		acct := &cfg.Accounts[i]
		if acct.Signature != "" && len(acct.Signatures) == 0 {
			acct.Signatures = []Signature{{
				Name:      "Default",
				Body:      acct.Signature,
				IsDefault: true,
			}}
			acct.Signature = ""
			migrated = true
		}
	}
	return migrated
}

// DefaultSignature returns the index and body of the default signature, or (-1, "") if none.
func (a Account) DefaultSignature() (int, string) {
	for i, s := range a.Signatures {
		if s.IsDefault {
			return i, s.Body
		}
	}
	if len(a.Signatures) > 0 {
		return 0, a.Signatures[0].Body
	}
	return -1, ""
}

func LoadOrCreate(path string) (Config, error) {
	if _, err := os.Stat(path); err == nil {
		return Load(path)
	}

	cfg := Default()
	if err := save(path, cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// AddAccount adds an account and persists the config.
// If an account with the same email already exists, it updates the name.
func AddAccount(path string, cfg *Config, name, email string) error {
	for i, a := range cfg.Accounts {
		if a.Email == email {
			cfg.Accounts[i].Name = name
			return Save(path, *cfg)
		}
	}
	cfg.Accounts = append(cfg.Accounts, Account{Name: name, Email: email})
	return Save(path, *cfg)
}

// Save persists the config to disk.
func Save(path string, cfg Config) error {
	return save(path, cfg)
}

func save(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(cfg)
}
