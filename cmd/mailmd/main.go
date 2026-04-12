package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/deric/mailmd/internal/auth"
	"github.com/deric/mailmd/internal/config"
	"github.com/deric/mailmd/internal/gmail"
	"github.com/deric/mailmd/internal/markdown"
	"github.com/deric/mailmd/internal/ui"
	"github.com/deric/mailmd/internal/ui/common"
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
	if len(os.Args) >= 2 {
		switch os.Args[1] {
		case "compose":
			return runCompose(os.Args[2:])
		case "draft":
			return runDraft(os.Args[2:])
		case "version":
			fmt.Println("mailmd " + version)
			return nil
		case "help", "--help", "-h":
			topic := ""
			if len(os.Args) >= 3 {
				topic = os.Args[2]
			}
			printHelp(topic)
			return nil
		}
	}
	return runTUI(nil)
}

func printHelp(topic string) {
	switch topic {
	case "compose":
		fmt.Println(`mailmd compose — Open TUI with compose dialog pre-filled

Usage:
  mailmd compose [flags]

Flags:
  --to <email>       Recipient email address (repeatable for multiple)
  --cc <email>       CC recipient (repeatable)
  --subject <text>   Email subject line
  --body <text>      Body text in Markdown format
  --body-file <path> Path to a Markdown file for the body

The compose dialog opens with fields pre-filled. You can edit recipients,
subject, and attachments before the external editor opens for the body.

Examples:
  mailmd compose --to alice@example.com --subject "Hello"
  mailmd compose --to alice@example.com --to bob@example.com --body-file draft.md
  mailmd compose --to team@company.com --cc boss@company.com --subject "Update"`)

	case "draft":
		fmt.Println(`mailmd draft — Create a Gmail draft without opening the TUI

Usage:
  mailmd draft [flags]

Flags:
  --to <email>       Recipient email address (repeatable, required)
  --cc <email>       CC recipient (repeatable)
  --subject <text>   Email subject line
  --body <text>      Body text in Markdown format
  --body-file <path> Path to a Markdown file for the body
  --open             Open the TUI compose dialog instead of saving a draft

Body can also be piped via stdin. Markdown is converted to HTML automatically.
The draft appears in your Drafts folder — open mailmd, go to Drafts, press 'e'.

Examples:
  mailmd draft --to alice@example.com --subject "Report" --body-file report.md
  mailmd draft --to alice@example.com --subject "Hi" --body-file draft.md --open
  echo "**Hello** from a script" | mailmd draft --to bob@example.com --subject "Hi"
  cat notes.md | mailmd draft --to team@example.com --subject "Meeting notes"

LLM integration:
  An LLM can generate email drafts by writing Markdown to a file, then:
    mailmd draft --to recipient@example.com --subject "Subject" --body-file /tmp/draft.md
  Add --open to review and send immediately in the TUI.`)

	case "keys", "keybindings":
		fmt.Println(`mailmd keybindings — Keyboard shortcuts reference

INBOX
  j / ↓              Move down
  k / ↑              Move up
  enter / o / l / →  Open message
  N + enter          Jump to message N
  tab                Next folder
  shift+tab          Previous folder
  c                  Compose new email
  r                  Reply to message
  R                  Reply all
  e                  Edit draft (in Drafts folder)
  s                  Toggle star
  t                  Apply label to message
  A                  Archive (remove from Inbox, keep in All Mail)
  d                  Trash / permanently delete
  b                  Block sender (auto-trash future mail)
  m                  Toggle read / unread
  u                  Restore from trash
  space              Select message
  a                  Select / deselect all
  f / /              Search
  ctrl+r             Refresh
  p                  Toggle preview pane
  L                  Browse labels (custom Gmail labels)
  S                  Switch account
  ,                  Account settings (signatures)
  K                  Show keybindings help
  q / ctrl+c         Quit

READER
  j / ↓              Scroll down
  k / ↑              Scroll up
  esc / h            Back to inbox
  r                  Reply
  R                  Reply all
  f                  Forward
  A                  Archive message (in Inbox)
  d                  Trash message
  N + l              Open link N in browser
  N + enter          Open attachment N
  P                  Open in browser
  I                  Open all images

COMPOSE DIALOG
  tab                Next field (To → CC → Subject → Attachments)
  shift+tab          Previous field
  enter              Add recipient/attachment, or advance
  backspace          Remove last item (on empty input)
  up / down          Navigate autocomplete suggestions
  ctrl+b             Show BCC field
  ctrl+t             Insert template (if configured)
  esc                Cancel (saves draft if content exists)

PREVIEW (after writing body)
  y                  Send (with 6s undo window)
  e                  Edit body in editor
  H                  Edit headers (recipients, subject, attachments)
  P                  Open HTML preview in browser
  esc                Cancel (saves draft)

UNDO SEND
  U                  Cancel send during the 5-second countdown

EDITOR TIPS
  The body is composed in your $EDITOR. To import a file:
    Vim/Neovim:  :r ~/path/to/file.txt
    Nano:        Ctrl+R
    Emacs:       C-x i`)

	case "config":
		fmt.Println(`mailmd config — Configuration reference

Config file location: ~/.config/mailmd/config.toml

SIGNATURES

Add a signature per account. It's appended as Markdown when composing.

  [[accounts]]
  name = "Work"
  email = "alice@work.com"

  [[accounts.signatures]]
  name = "Formal"
  body = """
  ---
  **Alice Smith** | Engineering
  alice@work.com | (555) 123-4567
  """
  is_default = true

  [[accounts.signatures]]
  name = "Casual"
  body = "— Alice"

  [[accounts]]
  name = "Personal"
  email = "alice@gmail.com"

TEMPLATES

Define reusable email templates. Access with ctrl+t in the compose dialog.

  [templates.standup]
  subject = "Standup Update"
  body = """
  **Yesterday:**
  -

  **Today:**
  -

  **Blockers:**
  - None
  """

  [templates.intro]
  body = """
  Hi,

  My name is Alice and I'm reaching out because...

  Best,
  Alice
  """

CONTACT GROUPS

Define groups that expand in the To/CC/BCC fields.

  [contact_groups]
  team = ["alice@work.com", "bob@work.com", "carol@work.com"]
  leads = ["dave@work.com", "eve@work.com"]

THEMES

Set a color theme. Available: default, solarized, nord, gruvbox.

  [general]
  theme = "nord"

OTHER SETTINGS

  [general]
  editor = "nvim"       # defaults to $EDITOR, then vi

  [preview]
  browser = "firefox"   # defaults to system browser`)

	default:
		fmt.Println(`mailmd — A terminal email client for Gmail

Usage:
  mailmd                Open the TUI email client
  mailmd compose        Open TUI with compose dialog pre-filled
  mailmd draft          Create a Gmail draft and exit (no TUI)
  mailmd version        Print version
  mailmd help [topic]   Show help

Topics:
  compose       Compose command flags and examples
  draft         Draft command flags and examples (incl. LLM integration)
  keys          Full keybindings reference
  config        Signatures, templates, contact groups, themes

Examples:
  mailmd
  mailmd compose --to alice@example.com --subject "Hello" --body-file draft.md
  mailmd draft --to bob@example.com --subject "Report" --body-file report.md
  echo "Hello" | mailmd draft --to alice@example.com --subject "Hi"
  mailmd help keys`)
	}
}

// repeatable is a flag.Value that collects multiple --to or --cc values.
type repeatable []string

func (r *repeatable) String() string { return strings.Join(*r, ", ") }
func (r *repeatable) Set(v string) error {
	*r = append(*r, v)
	return nil
}

func parseComposeFlags(args []string) (to, cc repeatable, subject, body string, err error) {
	fs := flag.NewFlagSet("compose", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // suppress default flag error output
	fs.Var(&to, "to", "Recipient email (repeatable)")
	fs.Var(&cc, "cc", "CC recipient (repeatable)")
	fs.StringVar(&subject, "subject", "", "Email subject")
	fs.StringVar(&body, "body", "", "Body text (Markdown)")
	var bodyFile string
	fs.StringVar(&bodyFile, "body-file", "", "Path to Markdown file for body")

	if err := fs.Parse(args); err != nil {
		return nil, nil, "", "", err
	}

	// Read body from file if specified
	if bodyFile != "" && body == "" {
		data, err := os.ReadFile(bodyFile)
		if err != nil {
			return nil, nil, "", "", fmt.Errorf("reading body file: %w", err)
		}
		body = string(data)
	}

	// Read body from stdin if piped and no body given
	if body == "" {
		if info, err := os.Stdin.Stat(); err == nil && (info.Mode()&os.ModeCharDevice) == 0 {
			data, err := io.ReadAll(os.Stdin)
			if err == nil && len(data) > 0 {
				body = string(data)
			}
		}
	}

	return to, cc, subject, body, nil
}

func runCompose(args []string) error {
	to, cc, subject, body, err := parseComposeFlags(args)
	if err != nil {
		return err
	}

	msg := &common.ComposeMsg{
		To:      to,
		CC:      cc,
		Subject: subject,
		Body:    body,
		Title:   "Compose",
	}

	return runTUI(msg)
}

func runDraft(args []string) error {
	// Parse --open separately before parseComposeFlags
	var open bool
	var filtered []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--open" {
			open = true
		} else {
			filtered = append(filtered, args[i])
		}
	}

	to, cc, subject, body, err := parseComposeFlags(filtered)
	if err != nil {
		return err
	}

	if len(to) == 0 {
		return fmt.Errorf("--to is required for draft creation")
	}

	if open {
		return runTUI(&common.ComposeMsg{
			To:      to,
			CC:      cc,
			Subject: subject,
			Body:    body,
			Title:   "Compose",
		})
	}

	ctx := context.Background()
	client, _, err := initClient(ctx)
	if err != nil {
		return err
	}

	toStr := strings.Join(to, ", ")
	ccStr := strings.Join(cc, ", ")

	var htmlBody, plainBody string
	if body != "" {
		if h, err := markdown.Convert(body); err == nil {
			htmlBody = h
		}
		plainBody = markdown.ConvertPlain(body)
	}

	if err := client.CreateDraft(ctx, toStr, ccStr, "", subject, htmlBody, plainBody, nil); err != nil {
		return fmt.Errorf("creating draft: %w", err)
	}

	fmt.Printf("Draft created: To=%s Subject=%q\n", toStr, subject)
	return nil
}

func runTUI(initialCompose *common.ComposeMsg) error {
	ctx := context.Background()
	client, activeEmail, err := initClient(ctx)
	if err != nil {
		return err
	}

	configDir, _ := os.UserConfigDir()
	cfgPath := filepath.Join(configDir, "mailmd", "config.toml")
	cfg, err := config.LoadOrCreate(cfgPath)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	// Apply theme from config
	if cfg.General.Theme != "" && cfg.General.Theme != "default" {
		common.ApplyTheme(cfg.General.Theme)
	}

	id, secret := oauthCreds()

	opts := ui.AppOptions{
		Ctx:            ctx,
		Client:         client,
		Cfg:            cfg,
		CfgPath:        cfgPath,
		ClientID:       id,
		ClientSecret:   secret,
		ConfigDir:      configDir,
		ActiveEmail:    activeEmail,
		InitialCompose: initialCompose,
	}

	p := tea.NewProgram(ui.New(opts), tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err = p.Run()
	return err
}

func oauthCreds() (string, string) {
	id := clientID
	secret := clientSecret
	if env := os.Getenv("MAILMD_CLIENT_ID"); env != "" {
		id = env
	}
	if env := os.Getenv("MAILMD_CLIENT_SECRET"); env != "" {
		secret = env
	}
	return id, secret
}

func initClient(ctx context.Context) (gmail.Client, string, error) {
	id, secret := oauthCreds()
	if id == "" || secret == "" {
		return nil, "", fmt.Errorf("OAuth2 credentials not configured.\nSet MAILMD_CLIENT_ID and MAILMD_CLIENT_SECRET environment variables.\nSee README.md for setup instructions.")
	}

	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, "", fmt.Errorf("config directory: %w", err)
	}

	cfgPath := filepath.Join(configDir, "mailmd", "config.toml")
	cfg, err := config.LoadOrCreate(cfgPath)
	if err != nil {
		return nil, "", fmt.Errorf("config: %w", err)
	}

	if len(cfg.Accounts) > 0 {
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
			return nil, "", fmt.Errorf("auth: %w", err)
		}
		client, err := gmail.NewClient(ctx, httpClient)
		if err != nil {
			return nil, "", fmt.Errorf("gmail client: %w", err)
		}
		return client, acct.Email, nil
	}

	// First run: authenticate and auto-detect
	legacyPath := filepath.Join(configDir, "mailmd", "tokens.json")
	store := auth.NewTokenStore(legacyPath)
	httpClient, err := auth.Authenticate(ctx, id, secret, store)
	if err != nil {
		return nil, "", fmt.Errorf("auth: %w", err)
	}
	client, err := gmail.NewClient(ctx, httpClient)
	if err != nil {
		return nil, "", fmt.Errorf("gmail client: %w", err)
	}

	email, err := client.GetProfile(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("fetching profile: %w", err)
	}

	newPath := auth.AccountTokenPath(configDir, email)
	if data, err := os.ReadFile(legacyPath); err == nil {
		os.MkdirAll(filepath.Dir(newPath), 0700)
		os.WriteFile(newPath, data, 0600)
	}

	name := strings.Split(email, "@")[0]
	if err := config.AddAccount(cfgPath, &cfg, name, email); err != nil {
		return nil, "", fmt.Errorf("saving config: %w", err)
	}

	return client, email, nil
}

// Ensure version is referenced to avoid "declared but not used" errors.
var _ = version
