package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Account struct {
	Name  string `toml:"name"`
	Email string `toml:"email"`
}

type Config struct {
	General       General     `toml:"general"`
	Keybindings   Keybindings `toml:"keybindings"`
	Preview       Preview     `toml:"preview"`
	Accounts      []Account   `toml:"accounts"`
	LastAccount   string      `toml:"last_account,omitempty"`
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
	return cfg, nil
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

// AddAccount appends a new account and persists the config.
func AddAccount(path string, cfg *Config, name, email string) error {
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
