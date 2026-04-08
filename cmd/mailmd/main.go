package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

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

	cfg, err := config.LoadOrCreate(filepath.Join(configDir, "mailmd", "config.toml"))
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	store := auth.NewTokenStore(filepath.Join(configDir, "mailmd", "tokens.json"))
	httpClient, err := auth.Authenticate(ctx, id, secret, store)
	if err != nil {
		return fmt.Errorf("auth: %w", err)
	}

	client, err := gmail.NewClient(ctx, httpClient)
	if err != nil {
		return fmt.Errorf("gmail client: %w", err)
	}

	p := tea.NewProgram(ui.New(ctx, client, cfg), tea.WithAltScreen())
	_, err = p.Run()
	return err
}

// Ensure version is referenced to avoid "declared but not used" errors.
var _ = version
