package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/deric/mailmd/internal/auth"
	"github.com/deric/mailmd/internal/config"
	"github.com/deric/mailmd/internal/gmail"
	"github.com/deric/mailmd/internal/ui"
)

var (
	clientID     = ""
	clientSecret = ""
	version      = "dev"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()
	id := clientID
	secret := clientSecret
	if env := os.Getenv("MAILMD_CLIENT_ID"); env != "" {
		id = env
	}
	if env := os.Getenv("MAILMD_CLIENT_SECRET"); env != "" {
		secret = env
	}
	if id == "" || secret == "" {
		return fmt.Errorf("OAuth2 credentials not configured.\nSet MAILMD_CLIENT_ID and MAILMD_CLIENT_SECRET environment variables.\nSee README.md for setup instructions.")
	}

	configDir, err := os.UserConfigDir()
	if err != nil {
		return fmt.Errorf("config directory: %w", err)
	}

	cfgPath := filepath.Join(configDir, "mailmd", "config.toml")
	cfg, err := config.LoadOrCreate(cfgPath)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	var initialClient gmail.Client
	var activeEmail string

	if len(cfg.Accounts) > 0 {
		// Use last account, or fall back to first
		acct := cfg.Accounts[0]
		if cfg.LastAccount != "" {
			for _, a := range cfg.Accounts {
				if a.Email == cfg.LastAccount {
					acct = a
					break
				}
			}
		}
		tokenPath := auth.AccountTokenPath(configDir, acct.Email)
		store := auth.NewTokenStore(tokenPath)
		httpClient, err := auth.Authenticate(ctx, id, secret, store)
		if err != nil {
			return fmt.Errorf("auth: %w", err)
		}
		client, err := gmail.NewClient(ctx, httpClient)
		if err != nil {
			return fmt.Errorf("gmail client: %w", err)
		}
		initialClient = client
		activeEmail = acct.Email
	} else {
		// First run or legacy: authenticate and auto-detect account
		legacyPath := filepath.Join(configDir, "mailmd", "tokens.json")
		store := auth.NewTokenStore(legacyPath)
		httpClient, err := auth.Authenticate(ctx, id, secret, store)
		if err != nil {
			return fmt.Errorf("auth: %w", err)
		}
		client, err := gmail.NewClient(ctx, httpClient)
		if err != nil {
			return fmt.Errorf("gmail client: %w", err)
		}

		// Fetch email from Gmail profile
		email, err := client.GetProfile(ctx)
		if err != nil {
			return fmt.Errorf("fetching profile: %w", err)
		}

		// Migrate token to per-account path
		newPath := auth.AccountTokenPath(configDir, email)
		if data, err := os.ReadFile(legacyPath); err == nil {
			os.MkdirAll(filepath.Dir(newPath), 0700)
			os.WriteFile(newPath, data, 0600)
		}

		// Create first account entry
		name := strings.Split(email, "@")[0]
		if err := config.AddAccount(cfgPath, &cfg, name, email); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}

		initialClient = client
		activeEmail = email
	}

	opts := ui.AppOptions{
		Ctx:          ctx,
		Client:       initialClient,
		Cfg:          cfg,
		CfgPath:      cfgPath,
		ClientID:     id,
		ClientSecret: secret,
		ConfigDir:    configDir,
		ActiveEmail:  activeEmail,
	}

	p := tea.NewProgram(ui.New(opts), tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err = p.Run()
	return err
}

// Ensure version is referenced to avoid "declared but not used" errors.
var _ = version
