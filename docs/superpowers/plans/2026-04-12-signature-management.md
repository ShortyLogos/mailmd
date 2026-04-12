# Signature Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add multi-signature support per account, a settings panel to manage them, Gmail import, and a signature picker in the compose dialog.

**Architecture:** Extend the config data model to support named signatures per account. Add a Gmail SendAs API call for import. Build an account-settings overlay (like the account switcher) keyed to `,`. Add a signature selector field to the compose dialog between Subject and Attachments.

**Tech Stack:** Go, Bubble Tea, Gmail API v1 (SendAs), BurntSushi/toml

---

### Task 1: Config Data Model — Signature struct and migration

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

- [ ] **Step 1: Add Signature struct and update Account**

In `internal/config/config.go`, add the `Signature` struct and update `Account`:

```go
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
```

- [ ] **Step 2: Add migration logic in Load()**

After `deduplicateAccounts`, add migration of old single `Signature` field to `Signatures` slice:

```go
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
```

- [ ] **Step 3: Add helper methods on Account**

```go
// DefaultSignature returns the default signature body, or empty string.
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
```

- [ ] **Step 4: Write migration test**

In `internal/config/config_test.go`, add:

```go
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
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/config/ -v -run TestMigrate`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat: multi-signature data model with migration from single signature"
```

---

### Task 2: Gmail API — GetSendAsSignature

**Files:**
- Modify: `internal/gmail/client.go`

- [ ] **Step 1: Add GetSendAsSignature to Client interface**

In `internal/gmail/client.go`, add to the `Client` interface (after `GetProfile`):

```go
GetSendAsSignature(ctx context.Context) (string, error)
```

- [ ] **Step 2: Implement GetSendAsSignature**

In `internal/gmail/client.go`, add the implementation after the `GetProfile` method:

```go
func (c *gmailClient) GetSendAsSignature(ctx context.Context) (string, error) {
	resp, err := c.svc.Users.Settings.SendAs.List(c.user).Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("failed to list SendAs aliases: %w", err)
	}
	for _, alias := range resp.SendAs {
		if alias.IsPrimary {
			return alias.Signature, nil // HTML string
		}
	}
	return "", nil
}
```

Add `"fmt"` to the import block if not already present.

- [ ] **Step 3: Verify build**

Run: `go build ./internal/gmail/...`
Expected: clean build

- [ ] **Step 4: Commit**

```bash
git add internal/gmail/client.go
git commit -m "feat: add GetSendAsSignature to Gmail client for importing signatures"
```

---

### Task 3: Key binding and settings message

**Files:**
- Modify: `internal/ui/common/keys.go`
- Modify: `internal/ui/common/messages.go`

- [ ] **Step 1: Add Settings key binding**

In `internal/ui/common/keys.go`, add `Settings` to the `KeyMap` struct:

```go
type KeyMap struct {
	Up, Down, Open, Back, Compose, Reply, ReplyAll, Forward, Trash key.Binding
	Preview, BPreview, NextTab, PrevTab, Send, Edit                key.Binding
	Refresh, Restore, Select, SelectAll, Quit, Help                key.Binding
	Home, End, Archive, Settings                                   key.Binding
}
```

Add the binding in the `var Keys` initializer (after `Archive`):

```go
Settings: key.NewBinding(key.WithKeys(","), key.WithHelp(",", "settings")),
```

- [ ] **Step 2: Add settings-related message types**

In `internal/ui/common/messages.go`, add at the end:

```go
// SignatureEditDoneMsg is sent when the external editor returns after editing a signature.
type SignatureEditDoneMsg struct {
	Content string
	Err     error
}
```

- [ ] **Step 3: Verify build**

Run: `go build ./internal/ui/common/...`
Expected: clean build

- [ ] **Step 4: Commit**

```bash
git add internal/ui/common/keys.go internal/ui/common/messages.go
git commit -m "feat: add settings keybinding and signature edit message type"
```

---

### Task 4: Settings panel state and key handling

**Files:**
- Modify: `internal/ui/app.go`

- [ ] **Step 1: Add settings panel state to App struct**

After the account switcher fields (around line 133), add:

```go
	// Settings panel
	showSettings       bool
	settingsCursor     int
	settingsNaming     bool          // true when typing name for new signature
	settingsNameInput  textinput.Model
```

- [ ] **Step 2: Initialize settings name input in New()**

In the `New()` function, create the textinput (near the other textinput initializations around line 200):

```go
	settingsNameInput := textinput.New()
	settingsNameInput.Placeholder = "Signature name..."
	settingsNameInput.CharLimit = 64
```

And add it to the App struct initialization:

```go
	settingsNameInput:  settingsNameInput,
```

- [ ] **Step 3: Add `,` key handler to open settings panel**

In the `Update` method, after the `S` key handler for the account switcher (around line 723), add:

```go
		// , opens settings from inbox (only when not in search/jump/compose)
		if a.screen == screenInbox && kmsg.String() == "," && !a.active.inbox.IsInputActive() {
			a.showSettings = true
			a.settingsCursor = 0
			a.settingsNaming = false
			return a, nil
		}
```

- [ ] **Step 4: Add settings panel dispatch in Update**

In the key event handling block (after `a.showSwitcher` check around line 714), add:

```go
		if a.showSettings {
			return a.updateSettings(kmsg)
		}
```

- [ ] **Step 5: Implement updateSettings()**

Add the method after `updateSwitcher()`:

```go
// updateSettings handles keys when the settings panel is open.
func (a App) updateSettings(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// If naming a new signature, capture input
	if a.settingsNaming {
		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
			name := strings.TrimSpace(a.settingsNameInput.Value())
			if name == "" {
				a.settingsNaming = false
				return a, nil
			}
			a.settingsNaming = false
			// Add empty signature and open editor
			acct := a.activeAccount()
			if acct == nil {
				return a, nil
			}
			acct.Signatures = append(acct.Signatures, config.Signature{Name: name})
			a.settingsCursor = len(acct.Signatures) - 1
			_ = config.Save(a.cfgPath, a.cfg)
			return a, a.editSignatureInEditor(a.settingsCursor)

		case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
			a.settingsNaming = false
			return a, nil
		}
		var cmd tea.Cmd
		a.settingsNameInput, cmd = a.settingsNameInput.Update(msg)
		return a, cmd
	}

	acct := a.activeAccount()
	sigCount := 0
	if acct != nil {
		sigCount = len(acct.Signatures)
	}

	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
		a.showSettings = false
		return a, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("j", "down"))):
		if a.settingsCursor < sigCount-1 {
			a.settingsCursor++
		}
		return a, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("k", "up"))):
		if a.settingsCursor > 0 {
			a.settingsCursor--
		}
		return a, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("a"))):
		// Add new signature — prompt for name
		a.settingsNaming = true
		a.settingsNameInput.SetValue("")
		a.settingsNameInput.Focus()
		return a, textinput.Blink

	case key.Matches(msg, key.NewBinding(key.WithKeys("e"))):
		// Edit selected signature body
		if acct != nil && a.settingsCursor < sigCount {
			return a, a.editSignatureInEditor(a.settingsCursor)
		}
		return a, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("d"))):
		// Delete selected signature
		if acct != nil && a.settingsCursor < sigCount {
			acct.Signatures = append(acct.Signatures[:a.settingsCursor], acct.Signatures[a.settingsCursor+1:]...)
			if a.settingsCursor >= len(acct.Signatures) && a.settingsCursor > 0 {
				a.settingsCursor--
			}
			_ = config.Save(a.cfgPath, a.cfg)
		}
		return a, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("*"))):
		// Toggle default on selected signature
		if acct != nil && a.settingsCursor < sigCount {
			for i := range acct.Signatures {
				acct.Signatures[i].IsDefault = (i == a.settingsCursor)
			}
			_ = config.Save(a.cfgPath, a.cfg)
		}
		return a, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("i"))):
		// Import from Gmail
		if acct == nil {
			return a, nil
		}
		return a, func() tea.Msg {
			htmlSig, err := a.active.client.GetSendAsSignature(a.ctx)
			if err != nil {
				return common.StatusMsg{Text: fmt.Sprintf("Import failed: %v", err)}
			}
			if htmlSig == "" {
				return common.StatusMsg{Text: "No signature found in Gmail"}
			}
			return gmailSignatureImportedMsg{html: htmlSig}
		}
	}

	return a, nil
}
```

- [ ] **Step 6: Add helper methods and message types**

Add near the other App helper methods:

```go
type gmailSignatureImportedMsg struct {
	html string
}

// activeAccount returns a pointer to the active account in the config slice.
func (a *App) activeAccount() *config.Account {
	for i := range a.cfg.Accounts {
		if a.cfg.Accounts[i].Email == a.active.email {
			return &a.cfg.Accounts[i]
		}
	}
	return nil
}

// editSignatureInEditor opens the external editor with the signature body.
func (a *App) editSignatureInEditor(idx int) tea.Cmd {
	acct := a.activeAccount()
	if acct == nil || idx >= len(acct.Signatures) {
		return nil
	}
	body := acct.Signatures[idx].Body

	f, err := os.CreateTemp("", "mailmd-sig-*.md")
	if err != nil {
		return func() tea.Msg { return common.SignatureEditDoneMsg{Err: err} }
	}
	if _, err := f.WriteString(body); err != nil {
		f.Close()
		os.Remove(f.Name())
		return func() tea.Msg { return common.SignatureEditDoneMsg{Err: err} }
	}
	f.Close()

	tmpPath := f.Name()
	editorCmd := a.cfg.Editor()
	cmd := exec.Command(editorCmd, tmpPath)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			os.Remove(tmpPath)
			return common.SignatureEditDoneMsg{Err: err}
		}
		data, readErr := os.ReadFile(tmpPath)
		os.Remove(tmpPath)
		if readErr != nil {
			return common.SignatureEditDoneMsg{Err: readErr}
		}
		return common.SignatureEditDoneMsg{Content: string(data)}
	})
}
```

Add `"os/exec"` to imports if not already present.

- [ ] **Step 7: Handle SignatureEditDoneMsg and gmailSignatureImportedMsg in Update**

In the main `Update` switch (where other message types are handled), add:

```go
	case common.SignatureEditDoneMsg:
		if msg.Err != nil {
			a.active.inbox.SetStatus(fmt.Sprintf("Editor error: %v", msg.Err))
			return a, nil
		}
		acct := a.activeAccount()
		if acct != nil && a.settingsCursor < len(acct.Signatures) {
			acct.Signatures[a.settingsCursor].Body = msg.Content
			_ = config.Save(a.cfgPath, a.cfg)
		}
		return a, nil

	case gmailSignatureImportedMsg:
		acct := a.activeAccount()
		if acct == nil {
			return a, nil
		}
		// Convert HTML to plain text (simple strip for signatures)
		plain := stripHTMLSimple(msg.html)
		// Update or add "Gmail (imported)" signature
		found := false
		for i := range acct.Signatures {
			if acct.Signatures[i].Name == "Gmail (imported)" {
				acct.Signatures[i].Body = plain
				found = true
				break
			}
		}
		if !found {
			acct.Signatures = append(acct.Signatures, config.Signature{
				Name: "Gmail (imported)",
				Body: plain,
			})
		}
		_ = config.Save(a.cfgPath, a.cfg)
		a.active.inbox.SetStatus("Signature imported from Gmail")
		return a, nil
```

- [ ] **Step 8: Add stripHTMLSimple helper**

Add near the other helpers:

```go
// stripHTMLSimple does a basic HTML-to-text conversion for signatures.
func stripHTMLSimple(s string) string {
	// Replace <br> and block tags with newlines
	for _, tag := range []string{"<br>", "<br/>", "<br />", "<BR>", "<p>", "<P>", "<div>", "<DIV>"} {
		s = strings.ReplaceAll(s, tag, "\n")
	}
	// Strip closing tags
	s = regexp.MustCompile(`</[^>]+>`).ReplaceAllString(s, "")
	// Strip remaining tags
	s = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(s, "")
	// Decode HTML entities
	s = html.UnescapeString(s)
	// Clean up whitespace
	s = strings.TrimSpace(s)
	return s
}
```

Add `"html"` and `"regexp"` to imports if not already present.

- [ ] **Step 9: Verify build**

Run: `go build ./...`
Expected: clean build

- [ ] **Step 10: Commit**

```bash
git add internal/ui/app.go internal/ui/common/messages.go
git commit -m "feat: settings panel state, key handling, Gmail import, and signature editing"
```

---

### Task 5: Settings panel rendering

**Files:**
- Modify: `internal/ui/app.go`

- [ ] **Step 1: Add renderSettingsOverlay()**

Add after `renderSwitcherOverlay()`:

```go
func (a App) renderSettingsOverlay(base string) string {
	innerWidth := 44
	var lines []string

	acctName := a.active.email
	for _, acct := range a.cfg.Accounts {
		if acct.Email == a.active.email && acct.Name != "" {
			acctName = acct.Name
			break
		}
	}

	title := lipgloss.NewStyle().Bold(true).Foreground(common.Primary).
		Render("Account Settings — " + acctName)
	lines = append(lines, title)
	lines = append(lines, "")

	section := lipgloss.NewStyle().Bold(true).Foreground(common.White).Render("Signatures")
	lines = append(lines, section)
	lines = append(lines, "")

	acct := a.activeAccountConst()
	if acct == nil || len(acct.Signatures) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(common.Muted).Render("  (no signatures)"))
	} else {
		for i, sig := range acct.Signatures {
			prefix := "  "
			if sig.IsDefault {
				prefix = "* "
			}
			name := sig.Name
			// Truncate body preview
			preview := strings.ReplaceAll(sig.Body, "\n", " ")
			if len(preview) > 30 {
				preview = preview[:30] + "..."
			}

			if i == a.settingsCursor {
				nameStyle := lipgloss.NewStyle().Bold(true).Foreground(common.White).Background(common.Primary)
				lines = append(lines, nameStyle.Render(padRight(prefix+name, innerWidth)))
				if preview != "" {
					lines = append(lines, "    "+lipgloss.NewStyle().Foreground(common.Muted).Render(preview))
				}
			} else {
				lines = append(lines, prefix+lipgloss.NewStyle().Foreground(common.Accent).Render(name))
				if preview != "" {
					lines = append(lines, "    "+lipgloss.NewStyle().Foreground(common.Muted).Render(preview))
				}
			}
		}
	}

	// Name input (if adding)
	if a.settingsNaming {
		lines = append(lines, "")
		lines = append(lines, lipgloss.NewStyle().Foreground(common.Muted).Render("  Name: ")+a.settingsNameInput.View())
	}

	// Help
	lines = append(lines, "")
	help := "j/k  a=add  e=edit  d=delete  *=default  i=import  esc"
	lines = append(lines, lipgloss.NewStyle().Foreground(common.Muted).Render(help))

	content := strings.Join(lines, "\n")

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(common.Secondary).
		Padding(1, 2)

	rendered := box.Render(content)
	return lipgloss.Place(a.width, a.height, lipgloss.Center, lipgloss.Center, rendered)
}
```

- [ ] **Step 2: Add activeAccountConst() helper for View methods**

```go
// activeAccountConst returns a pointer to the active account (read-only context).
func (a App) activeAccountConst() *config.Account {
	for i := range a.cfg.Accounts {
		if a.cfg.Accounts[i].Email == a.active.email {
			return &a.cfg.Accounts[i]
		}
	}
	return nil
}
```

- [ ] **Step 3: Wire rendering into View()**

Find the `View()` method in app.go. Look for where `renderSwitcherOverlay` is called and add settings panel rendering next to it. The pattern should be:

```go
if a.showSettings {
	return a.renderSettingsOverlay(base)
}
```

Add this right before or after the switcher overlay check.

- [ ] **Step 4: Verify build**

Run: `go build ./...`
Expected: clean build

- [ ] **Step 5: Commit**

```bash
git add internal/ui/app.go
git commit -m "feat: settings panel overlay rendering with signature list"
```

---

### Task 6: Compose dialog — signature selector field

**Files:**
- Modify: `internal/ui/app.go`

- [ ] **Step 1: Add composeFieldSignature constant**

Update the field constants:

```go
const (
	composeFieldTo composeDialogField = iota
	composeFieldCC
	composeFieldBCC
	composeFieldSubject
	composeFieldSignature
	composeFieldAttachments
)
```

- [ ] **Step 2: Add compose signature state to App struct**

Add after `showAttField` (around line 159):

```go
	composeSignatureIdx int  // index into account's Signatures (-1 for none)
	showSigField        bool // show signature picker in compose dialog
```

- [ ] **Step 3: Update openComposeDialog() to initialize signature**

In `openComposeDialog()`, after the `a.showAttField = len(attachments) > 0` line, add:

```go
	// Initialize signature selector to account default
	acct := a.activeAccountConst()
	if acct != nil && len(acct.Signatures) > 0 {
		idx, _ := acct.DefaultSignature()
		a.composeSignatureIdx = idx
		a.showSigField = true
	} else {
		a.composeSignatureIdx = -1
		a.showSigField = false
	}
```

- [ ] **Step 4: Update composeFieldOrder()**

Add the signature field between Subject and Attachments:

```go
func (a *App) composeFieldOrder() []composeDialogField {
	order := []composeDialogField{composeFieldTo}
	if a.showCCField {
		order = append(order, composeFieldCC)
	}
	if a.showBCCField {
		order = append(order, composeFieldBCC)
	}
	order = append(order, composeFieldSubject)
	if a.showSigField {
		order = append(order, composeFieldSignature)
	}
	if a.showAttField {
		order = append(order, composeFieldAttachments)
	}
	return order
}
```

- [ ] **Step 5: Update composeDialogActiveInput()**

The signature field has no text input, so `composeDialogActiveInput()` doesn't need a new case — it falls through to `default` (subject input). The field is a selector, not a text input.

- [ ] **Step 6: Handle Up/Down in signature field within updateComposeDialog()**

Find the key handling in `updateComposeDialog()`. Add a special case for the signature field. When `a.composeField == composeFieldSignature`, handle up/down to cycle signatures:

In the main switch of `updateComposeDialog()`, add before the default textinput delegation:

```go
	// Signature field — selector, not text input
	if a.composeField == composeFieldSignature {
		acct := a.activeAccountConst()
		sigCount := 0
		if acct != nil {
			sigCount = len(acct.Signatures)
		}
		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("up", "k"))):
			// -1 = (none), 0..sigCount-1 = signatures
			if a.composeSignatureIdx > -1 {
				a.composeSignatureIdx--
			}
			return a, nil
		case key.Matches(msg, key.NewBinding(key.WithKeys("down", "j"))):
			if a.composeSignatureIdx < sigCount-1 {
				a.composeSignatureIdx++
			}
			return a, nil
		case key.Matches(msg, key.NewBinding(key.WithKeys("tab"))):
			if a.composeDialogIsLastField() {
				return a.launchComposeEditor()
			}
			a.composeDialogAdvanceField()
			return a, nil
		case key.Matches(msg, key.NewBinding(key.WithKeys("shift+tab"))):
			a.composeDialogRetreatField()
			return a, nil
		case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
			if a.composeDialogIsLastField() {
				return a.launchComposeEditor()
			}
			a.composeDialogAdvanceField()
			return a, nil
		case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
			// Same as the existing esc handler — close dialog
			a.showComposeDialog = false
			return a, nil
		}
		return a, nil
	}
```

- [ ] **Step 7: Render signature field in renderComposeOverlay()**

After the Subject field rendering block (after the `lines = append(lines, "")` that follows Subject), add:

```go
	// Signature field (only shown when account has signatures)
	if a.showSigField {
		sigLabel := labelStyle
		if a.composeField == composeFieldSignature {
			sigLabel = activeLabel
		}

		sigName := "(none)"
		acct := a.activeAccountConst()
		if acct != nil && a.composeSignatureIdx >= 0 && a.composeSignatureIdx < len(acct.Signatures) {
			sigName = acct.Signatures[a.composeSignatureIdx].Name
		}

		if a.composeField == composeFieldSignature {
			lines = append(lines, sigLabel.Render("Signature: ")+valueStyle.Render("< "+sigName+" >")+
				mutedStyle.Render("  ↑/↓"))
		} else {
			lines = append(lines, sigLabel.Render("Signature: ")+mutedStyle.Render(sigName))
		}
	}
```

- [ ] **Step 8: Update help text for signature field**

In the help text switch at the bottom of `renderComposeOverlay()`, add a case:

```go
	case composeFieldSignature:
		if a.composeDialogIsLastField() {
			lines = append(lines, helpStyle.Render("↑/↓=change  enter/tab=open editor"+toggles))
		} else {
			lines = append(lines, helpStyle.Render("↑/↓=change  enter/tab=next  shift+tab=back"+toggles))
		}
		lines = append(lines, helpStyle.Render("esc=cancel"))
```

- [ ] **Step 9: Verify build**

Run: `go build ./...`
Expected: clean build

- [ ] **Step 10: Commit**

```bash
git add internal/ui/app.go
git commit -m "feat: signature selector field in compose dialog"
```

---

### Task 7: Signature injection update

**Files:**
- Modify: `internal/ui/app.go`

- [ ] **Step 1: Update activeAccountSignature() to use composeSignatureIdx**

Replace `activeAccountSignature()` with:

```go
// activeComposeSignature returns the signature body for the currently selected
// compose signature index, or empty string if none selected.
func (a App) activeComposeSignature() string {
	acct := a.activeAccountConst()
	if acct == nil || a.composeSignatureIdx < 0 || a.composeSignatureIdx >= len(acct.Signatures) {
		return ""
	}
	return acct.Signatures[a.composeSignatureIdx].Body
}
```

- [ ] **Step 2: Update launchComposeEditor()**

Change the signature injection in `launchComposeEditor()` from:

```go
	sig := a.activeAccountSignature()
```

to:

```go
	sig := a.activeComposeSignature()
```

The rest of the deduplication logic (`!strings.Contains(body, sig)`) stays the same.

- [ ] **Step 3: Keep activeAccountSignature() for backward compat**

Actually, keep the old `activeAccountSignature()` but update it to use the new data model:

```go
// activeAccountSignature returns the default signature for the active account.
func (a App) activeAccountSignature() string {
	for _, acct := range a.cfg.Accounts {
		if acct.Email == a.active.email {
			_, body := acct.DefaultSignature()
			return body
		}
	}
	return ""
}
```

This is still used nowhere after the change, but can be removed later if confirmed unused.

- [ ] **Step 4: Verify build**

Run: `go build ./...`
Expected: clean build

- [ ] **Step 5: Commit**

```bash
git add internal/ui/app.go
git commit -m "feat: use compose dialog signature selection for injection"
```

---

### Task 8: Help text and status bar updates

**Files:**
- Modify: `internal/ui/app.go`
- Modify: `internal/ui/inbox/inbox.go`
- Modify: `cmd/mailmd/main.go`

- [ ] **Step 1: Update inbox status bar**

In `internal/ui/inbox/inbox.go`, find the status bar suffix line and add `,=settings`:

Change:
```go
suffix := "  f=search  R=reply all  ctrl+r=refresh  tab=folder" + labelHint + "  K=keys  q=quit"
```
to:
```go
suffix := "  f=search  R=reply all  ctrl+r=refresh  ,=settings  tab=folder" + labelHint + "  K=keys  q=quit"
```

- [ ] **Step 2: Update help overlay in app.go**

In `renderHelpOverlay()`, in the Inbox section (after the `bind("S", "Switch account")` line), add:

```go
		lines = append(lines, bind(",", "Account settings"))
```

- [ ] **Step 3: Update CLI help text in main.go**

In the INBOX section, add after the `S                  Switch account` line:

```
  ,                  Account settings (signatures)
```

- [ ] **Step 4: Verify build**

Run: `go build ./...`
Expected: clean build

- [ ] **Step 5: Rebuild and verify help**

Run: `go build -o mailmd ./cmd/mailmd && ./mailmd --help keys`
Verify: `,` = Account settings appears in INBOX section

- [ ] **Step 6: Commit**

```bash
git add internal/ui/app.go internal/ui/inbox/inbox.go cmd/mailmd/main.go
git commit -m "feat: help text and status bar updates for settings panel"
```

---

### Task 9: Wire View() overlay rendering and final integration

**Files:**
- Modify: `internal/ui/app.go`

- [ ] **Step 1: Find and update View() method**

Search for the View() method in app.go. Find where `a.showSwitcher` is checked and the switcher overlay is rendered. Add the settings panel check alongside it:

```go
if a.showSettings {
	return a.renderSettingsOverlay(base)
}
```

This should be placed near the `a.showSwitcher` check, before the help overlay check.

- [ ] **Step 2: Handle the focus management for settings panel**

In `updateComposeDialog`, ensure the signature field doesn't try to focus a textinput. In `composeDialogFocusField()`, add a guard:

```go
func (a *App) composeDialogFocusField(field composeDialogField) {
	a.composeToInput.Blur()
	a.composeCCInput.Blur()
	a.composeBCCInput.Blur()
	a.composeSubjectInput.Blur()
	a.composeAttInput.Blur()
	a.composeField = field
	if field != composeFieldSignature {
		a.composeDialogActiveInput().Focus()
	}
}
```

- [ ] **Step 3: Full build and test**

Run: `go build ./... && go test ./...`
Expected: clean build, tests pass (ignore pre-existing TestOAuthConfigScopes failure)

- [ ] **Step 4: Commit**

```bash
git add internal/ui/app.go
git commit -m "feat: wire settings panel rendering and compose dialog integration"
```

---

### Task 10: Config documentation update

**Files:**
- Modify: `cmd/mailmd/main.go`

- [ ] **Step 1: Update config example in help text**

Find the config example in `main.go` (around line 185) and update the signature section:

```toml
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
```

- [ ] **Step 2: Verify build**

Run: `go build ./...`
Expected: clean build

- [ ] **Step 3: Commit**

```bash
git add cmd/mailmd/main.go
git commit -m "docs: update config example for multi-signature format"
```
