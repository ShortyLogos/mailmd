package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/deric/mailmd/internal/auth"
	"github.com/deric/mailmd/internal/config"
	"github.com/deric/mailmd/internal/gmail"
	"github.com/deric/mailmd/internal/markdown"
	"github.com/deric/mailmd/internal/ui/common"
	"github.com/deric/mailmd/internal/ui/composer"
	"github.com/deric/mailmd/internal/ui/inbox"
	"github.com/deric/mailmd/internal/ui/reader"
)

type screen int

const (
	screenInbox screen = iota
	screenReader
	screenCompose
)

// AppOptions holds everything needed to create the app.
type AppOptions struct {
	Ctx          context.Context
	Client       gmail.Client
	Cfg          config.Config
	CfgPath      string
	ClientID     string
	ClientSecret string
	ConfigDir    string
	ActiveEmail  string
}

// --- Messages ---

type fetchMsgResultMsg struct {
	msg *gmail.Message
	err error
}

type fetchReplyResultMsg struct {
	msg *gmail.Message
	err error
}

type authResultMsg struct {
	client gmail.Client
	email  string
	name   string
	err    error
}

// --- Per-account state ---

type accountState struct {
	name     string
	email    string
	client   gmail.Client
	inbox    inbox.Model
	msgCache map[string]*gmail.Message
}

// --- App ---

type App struct {
	ctx          context.Context
	cfgPath      string
	cfg          config.Config
	clientID     string
	clientSecret string
	configDir    string
	width        int
	height       int

	// Account management
	active  *accountState
	states  map[string]*accountState // email -> cached state
	initted map[string]bool          // tracks which accounts had Init() called

	// Screens
	screen   screen
	reader   reader.Model
	composer composer.Model
	loading  bool

	// Help overlay
	showHelp bool

	// Account switcher
	showSwitcher   bool
	switcherCursor int
	addingAccount  bool
	editingAccount bool
	nameInput      textinput.Model
	authPending    bool
}

func New(opts AppOptions) App {
	ti := textinput.New()
	ti.Placeholder = "Account name (e.g. Work)"
	ti.CharLimit = 64

	state := &accountState{
		email:    opts.ActiveEmail,
		client:   opts.Client,
		inbox:    inbox.New(opts.Ctx, opts.Client),
		msgCache: make(map[string]*gmail.Message),
	}
	// Find the account name from config
	for _, a := range opts.Cfg.Accounts {
		if a.Email == opts.ActiveEmail {
			state.name = a.Name
			break
		}
	}
	state.inbox.AccountName = state.name
	state.inbox.AccountEmail = state.email

	states := map[string]*accountState{opts.ActiveEmail: state}

	return App{
		ctx:          opts.Ctx,
		cfgPath:      opts.CfgPath,
		cfg:          opts.Cfg,
		clientID:     opts.ClientID,
		clientSecret: opts.ClientSecret,
		configDir:    opts.ConfigDir,
		active:       state,
		states:       states,
		initted:      map[string]bool{opts.ActiveEmail: true},
		screen:       screenInbox,
		nameInput:    ti,
	}
}

func (a App) Init() tea.Cmd {
	return a.active.inbox.Init()
}

// activeAccountName returns the display name for the current account.
func (a App) activeAccountName() string {
	if a.active.name != "" {
		return a.active.name
	}
	if a.active.email != "" {
		return a.active.email
	}
	return ""
}

// Update is the root message dispatcher.
func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		var cmds []tea.Cmd
		var cmd tea.Cmd
		a.active.inbox, cmd = a.active.inbox.Update(msg)
		cmds = append(cmds, cmd)
		if a.screen == screenReader {
			a.reader, cmd = a.reader.Update(msg)
			cmds = append(cmds, cmd)
		}
		if a.screen == screenCompose {
			a.composer, cmd = a.composer.Update(msg)
			cmds = append(cmds, cmd)
		}
		return a, tea.Batch(cmds...)

	// --- Account switcher ---

	case authResultMsg:
		a.authPending = false
		if msg.err != nil {
			a.active.inbox.SetStatus("Auth failed: " + msg.err.Error())
			a.showSwitcher = false
			a.addingAccount = false
			return a, nil
		}
		// Save account to config
		config.AddAccount(a.cfgPath, &a.cfg, msg.name, msg.email)

		// Create new account state
		newInbox := inbox.New(a.ctx, msg.client)
		newInbox.AccountName = msg.name
		newInbox.AccountEmail = msg.email
		newState := &accountState{
			name:     msg.name,
			email:    msg.email,
			client:   msg.client,
			inbox:    newInbox,
			msgCache: make(map[string]*gmail.Message),
		}
		a.states[msg.email] = newState
		a.initted[msg.email] = true
		a.active = newState
		a.screen = screenInbox
		a.showSwitcher = false
		a.addingAccount = false
		a.persistLastAccount(msg.email)

		// Size the new inbox and start fetching
		sizeCmd := func() tea.Msg { return tea.WindowSizeMsg{Width: a.width, Height: a.height} }
		return a, tea.Batch(a.active.inbox.Init(), sizeCmd)

	// --- Cross-screen transitions ---

	case common.FetchMessageMsg:
		id := msg.ID
		a.active.inbox.MarkRead(id)
		if cached, ok := a.active.msgCache[id]; ok {
			a.active.inbox.SetStatus("")
			a.reader = reader.New(a.ctx, a.active.client, cached, a.width, a.height, a.active.inbox.TabIdx(), a.activeAccountName(), a.active.email)
			a.screen = screenReader
			return a, tea.Batch(a.reader.Init(), a.markAsRead(id))
		}
		a.active.inbox.SetLoadingStatus("Opening message...")
		a.loading = true
		return a, tea.Batch(
			func() tea.Msg {
				full, err := a.active.client.GetMessage(a.ctx, id)
				return fetchMsgResultMsg{msg: full, err: err}
			},
			a.active.inbox.SpinnerTick(),
		)

	case common.FetchAndReplyMsg:
		id := msg.ID
		if cached, ok := a.active.msgCache[id]; ok {
			tmpl := markdown.ReplyTemplate(cached.From, "Re: "+cached.Subject, cached.Body)
			a.composer = composer.New(a.ctx, a.active.client, a.cfg.Editor(), tmpl, a.width, a.height)
			a.screen = screenCompose
			return a, a.composer.Init()
		}
		a.loading = true
		return a, func() tea.Msg {
			full, err := a.active.client.GetMessage(a.ctx, id)
			return fetchReplyResultMsg{msg: full, err: err}
		}

	case fetchReplyResultMsg:
		a.loading = false
		if msg.err != nil {
			a.active.inbox.SetStatus(fmt.Sprintf("Error: %v", msg.err))
			return a, nil
		}
		a.active.msgCache[msg.msg.ID] = msg.msg
		tmpl := markdown.ReplyTemplate(msg.msg.From, "Re: "+msg.msg.Subject, msg.msg.Body)
		a.composer = composer.New(a.ctx, a.active.client, a.cfg.Editor(), tmpl, a.width, a.height)
		a.screen = screenCompose
		return a, a.composer.Init()

	case fetchMsgResultMsg:
		a.loading = false
		a.active.inbox.SetStatus("")
		if msg.err != nil {
			a.active.inbox.SetStatus(fmt.Sprintf("Error: %v", msg.err))
			return a, nil
		}
		a.active.msgCache[msg.msg.ID] = msg.msg
		a.active.inbox.MarkRead(msg.msg.ID)
		a.reader = reader.New(a.ctx, a.active.client, msg.msg, a.width, a.height, a.active.inbox.TabIdx(), a.activeAccountName(), a.active.email)
		a.screen = screenReader
		return a, tea.Batch(a.reader.Init(), a.markAsRead(msg.msg.ID))

	case common.OpenMessageMsg:
		a.reader = reader.New(a.ctx, a.active.client, msg.Message, a.width, a.height, a.active.inbox.TabIdx(), a.activeAccountName(), a.active.email)
		a.screen = screenReader
		return a, a.reader.Init()

	case common.ComposeMsg:
		a.composer = composer.New(a.ctx, a.active.client, a.cfg.Editor(), msg.Template, a.width, a.height)
		a.screen = screenCompose
		return a, a.composer.Init()

	case common.TrashFromReaderMsg:
		a.screen = screenInbox
		id := msg.ID
		a.active.inbox.OptimisticRemove(id)
		delete(a.active.msgCache, id)
		label := a.active.inbox.CurrentLabelID()
		if label == "TRASH" {
			a.active.inbox.SetStatus("Deleting message...")
			return a, func() tea.Msg {
				err := a.active.client.DeleteMessage(a.ctx, id)
				if err != nil {
					return common.StatusMsg{Text: "Error: " + err.Error()}
				}
				return common.StatusMsg{Text: "Message permanently deleted."}
			}
		}
		a.active.inbox.SetStatus("Trashing message...")
		return a, func() tea.Msg {
			err := a.active.client.TrashMessage(a.ctx, id)
			if err != nil {
				return common.StatusMsg{Text: "Error: " + err.Error()}
			}
			return common.StatusMsg{Text: "Message trashed."}
		}

	case common.BackToInboxMsg:
		a.screen = screenInbox
		a.active.inbox.SetStatus("")
		return a, nil

	case common.SendResultMsg:
		a.screen = screenInbox
		status := "Message sent!"
		if msg.Err != nil {
			status = fmt.Sprintf("Send failed: %v", msg.Err)
		}
		return a, tea.Batch(
			a.active.inbox.Init(),
			func() tea.Msg { return common.StatusMsg{Text: status} },
		)

	case common.StatusMsg:
		var cmd tea.Cmd
		a.active.inbox, cmd = a.active.inbox.Update(msg)
		return a, cmd
	}

	// --- Switcher key handling (intercept before screen delegation) ---
	if kmsg, ok := msg.(tea.KeyMsg); ok {
		// Help overlay
		if a.showHelp {
			if kmsg.String() == "K" || kmsg.String() == "esc" || kmsg.String() == "q" {
				a.showHelp = false
			}
			return a, nil
		}
		if kmsg.String() == "K" {
			a.showHelp = true
			return a, nil
		}

		if a.showSwitcher {
			return a.updateSwitcher(kmsg)
		}
		// S opens switcher from inbox (only when not in search/jump/compose)
		if a.screen == screenInbox && kmsg.String() == "S" {
			a.showSwitcher = true
			a.switcherCursor = 0
			a.addingAccount = false
			a.authPending = false
			return a, nil
		}
	}

	// Delegate to active screen.
	var cmd tea.Cmd
	switch a.screen {
	case screenInbox:
		a.active.inbox, cmd = a.active.inbox.Update(msg)
	case screenReader:
		a.reader, cmd = a.reader.Update(msg)
	case screenCompose:
		a.composer, cmd = a.composer.Update(msg)
	}
	return a, cmd
}

// updateSwitcher handles keys when the account switcher is open.
func (a App) updateSwitcher(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Editing account name
	if a.editingAccount {
		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
			name := strings.TrimSpace(a.nameInput.Value())
			if name != "" && a.switcherCursor < len(a.cfg.Accounts) {
				a.cfg.Accounts[a.switcherCursor].Name = name
				config.Save(a.cfgPath, a.cfg)
				// Update cached state
				email := a.cfg.Accounts[a.switcherCursor].Email
				if s, ok := a.states[email]; ok {
					s.name = name
					s.inbox.AccountName = name
				}
			}
			a.editingAccount = false
			a.nameInput.Blur()
			return a, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
			a.editingAccount = false
			a.nameInput.Blur()
			return a, nil
		}
		var cmd tea.Cmd
		a.nameInput, cmd = a.nameInput.Update(msg)
		return a, cmd
	}

	// Adding account: name input mode
	if a.addingAccount && !a.authPending {
		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
			name := strings.TrimSpace(a.nameInput.Value())
			if name == "" {
				return a, nil
			}
			a.authPending = true
			a.nameInput.Blur()
			return a, a.authenticateNewAccount(name)

		case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
			a.addingAccount = false
			return a, nil
		}
		var cmd tea.Cmd
		a.nameInput, cmd = a.nameInput.Update(msg)
		return a, cmd
	}

	// Auth pending: only allow Esc
	if a.authPending {
		if key.Matches(msg, key.NewBinding(key.WithKeys("esc"))) {
			a.showSwitcher = false
			a.addingAccount = false
			a.authPending = false
		}
		return a, nil
	}

	// Normal switcher navigation
	totalItems := len(a.cfg.Accounts) + 1 // accounts + "Add account"
	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
		a.showSwitcher = false
		return a, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("j", "down"))):
		if a.switcherCursor < totalItems-1 {
			a.switcherCursor++
		}
		return a, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("k", "up"))):
		if a.switcherCursor > 0 {
			a.switcherCursor--
		}
		return a, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
		if a.switcherCursor < len(a.cfg.Accounts) {
			// Select existing account
			acct := a.cfg.Accounts[a.switcherCursor]
			if acct.Email == a.active.email {
				a.showSwitcher = false
				return a, nil
			}
			return a.switchToAccount(acct)
		}
		// "Add account" selected
		a.addingAccount = true
		a.nameInput.SetValue("")
		a.nameInput.Focus()
		return a, textinput.Blink

	case key.Matches(msg, key.NewBinding(key.WithKeys("e"))):
		// Edit highlighted account name
		if a.switcherCursor < len(a.cfg.Accounts) {
			a.editingAccount = true
			a.nameInput.SetValue(a.cfg.Accounts[a.switcherCursor].Name)
			a.nameInput.Focus()
			return a, textinput.Blink
		}

	case key.Matches(msg, key.NewBinding(key.WithKeys("x"))):
		// Remove highlighted account (can't remove active account)
		if a.switcherCursor < len(a.cfg.Accounts) {
			acct := a.cfg.Accounts[a.switcherCursor]
			if acct.Email == a.active.email {
				return a, nil // can't remove active account
			}
			a.removeAccount(acct.Email)
			if a.switcherCursor >= len(a.cfg.Accounts) {
				a.switcherCursor = len(a.cfg.Accounts) // clamp to "Add account"
			}
			return a, nil
		}
	}
	return a, nil
}

// switchToAccount switches to an existing account, using cache if available.
func (a App) switchToAccount(acct config.Account) (tea.Model, tea.Cmd) {
	if state, ok := a.states[acct.Email]; ok {
		// Cached — instant switch
		a.active = state
		a.screen = screenInbox
		a.showSwitcher = false
		a.persistLastAccount(acct.Email)
		// Re-send window size to ensure layout
		if !a.initted[acct.Email] {
			a.initted[acct.Email] = true
			sizeCmd := func() tea.Msg { return tea.WindowSizeMsg{Width: a.width, Height: a.height} }
			return a, tea.Batch(a.active.inbox.Init(), sizeCmd)
		}
		return a, nil
	}

	// Not cached — authenticate and create state
	a.showSwitcher = false
	a.active.inbox.SetLoadingStatus("Switching to " + acct.Name + "...")
	tokenPath := auth.AccountTokenPath(a.configDir, acct.Email)
	store := auth.NewTokenStore(tokenPath)

	return a, func() tea.Msg {
		httpClient, err := auth.AuthenticateSilent(a.ctx, a.clientID, a.clientSecret, store)
		if err != nil {
			return authResultMsg{err: err}
		}
		client, err := gmail.NewClient(a.ctx, httpClient)
		if err != nil {
			return authResultMsg{err: err}
		}
		return authResultMsg{client: client, email: acct.Email, name: acct.Name}
	}
}

// authenticateNewAccount starts OAuth for a new account.
func (a App) authenticateNewAccount(name string) tea.Cmd {
	return func() tea.Msg {
		// Use a temp token store — we don't know the email yet
		tempPath := filepath.Join(a.configDir, "mailmd", "tokens", "pending.json")
		os.MkdirAll(filepath.Dir(tempPath), 0700)
		store := auth.NewTokenStore(tempPath)

		httpClient, err := auth.AuthenticateSilent(a.ctx, a.clientID, a.clientSecret, store)
		if err != nil {
			return authResultMsg{err: err, name: name}
		}

		client, err := gmail.NewClient(a.ctx, httpClient)
		if err != nil {
			return authResultMsg{err: err, name: name}
		}

		// Get email from profile
		email, err := client.GetProfile(a.ctx)
		if err != nil {
			return authResultMsg{err: err, name: name}
		}

		// Move token to permanent path
		finalPath := auth.AccountTokenPath(a.configDir, email)
		os.Rename(tempPath, finalPath)

		return authResultMsg{client: client, email: email, name: name}
	}
}

func (a *App) removeAccount(email string) {
	// Remove from config
	var remaining []config.Account
	for _, acct := range a.cfg.Accounts {
		if acct.Email != email {
			remaining = append(remaining, acct)
		}
	}
	a.cfg.Accounts = remaining
	config.Save(a.cfgPath, a.cfg)

	// Remove cached state
	delete(a.states, email)
	delete(a.initted, email)

	// Delete token file
	tokenPath := auth.AccountTokenPath(a.configDir, email)
	os.Remove(tokenPath)
}

func (a *App) persistLastAccount(email string) {
	a.cfg.LastAccount = email
	config.Save(a.cfgPath, a.cfg)
}

func (a App) markAsRead(id string) tea.Cmd {
	return func() tea.Msg {
		a.active.client.MoveMessage(a.ctx, id, nil, []string{"UNREAD"})
		return nil
	}
}

// View delegates to the active screen, with optional switcher overlay.
func (a App) View() string {
	var base string
	if a.loading {
		base = a.active.inbox.View()
	} else {
		switch a.screen {
		case screenReader:
			base = a.reader.View()
		case screenCompose:
			base = a.composer.View()
		default:
			base = a.active.inbox.View()
		}
	}

	if a.showHelp {
		return a.renderHelpOverlay(base)
	}
	if a.showSwitcher {
		return a.renderSwitcherOverlay(base)
	}
	return base
}

// renderHelpOverlay draws a context-aware keybind reference.
func (a App) renderHelpOverlay(base string) string {
	keyStyle := lipgloss.NewStyle().Bold(true).Foreground(common.Primary)
	descStyle := lipgloss.NewStyle().Foreground(common.White)
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(common.Warning)

	bind := func(key, desc string) string {
		return fmt.Sprintf("  %s  %s", keyStyle.Render(fmt.Sprintf("%-12s", key)), descStyle.Render(desc))
	}

	var lines []string

	title := lipgloss.NewStyle().Bold(true).Foreground(common.Primary).Render("Keybindings")

	switch a.screen {
	case screenInbox:
		lines = append(lines, title)
		lines = append(lines, "")
		lines = append(lines, sectionStyle.Render("  Navigation"))
		lines = append(lines, bind("j / ↓", "Move down"))
		lines = append(lines, bind("k / ↑", "Move up"))
		lines = append(lines, bind("enter / o", "Open message"))
		lines = append(lines, bind("l / →", "Open message"))
		lines = append(lines, bind("N + enter", "Jump to message N"))
		lines = append(lines, bind("tab", "Next folder"))
		lines = append(lines, bind("shift+tab", "Previous folder"))
		lines = append(lines, bind("h / ←", "Previous folder"))
		lines = append(lines, "")
		lines = append(lines, sectionStyle.Render("  Actions"))
		lines = append(lines, bind("c", "Compose new email"))
		lines = append(lines, bind("r", "Reply to message"))
		lines = append(lines, bind("d", "Trash / delete message"))
		lines = append(lines, bind("b", "Block sender (auto-trash)"))
		lines = append(lines, bind("m", "Toggle read / unread"))
		lines = append(lines, bind("u", "Restore from trash"))
		lines = append(lines, bind("space", "Select message"))
		lines = append(lines, bind("a", "Select / deselect all"))
		lines = append(lines, "")
		lines = append(lines, sectionStyle.Render("  Other"))
		lines = append(lines, bind("f / /", "Search"))
		lines = append(lines, bind("R", "Refresh"))
		lines = append(lines, bind("p", "Toggle preview"))
		lines = append(lines, bind("S", "Switch account"))
		lines = append(lines, bind("K", "This help"))
		lines = append(lines, bind("q / ctrl+c", "Quit"))

	case screenReader:
		lines = append(lines, title)
		lines = append(lines, "")
		lines = append(lines, sectionStyle.Render("  Navigation"))
		lines = append(lines, bind("j / ↓", "Scroll down"))
		lines = append(lines, bind("k / ↑", "Scroll up"))
		lines = append(lines, bind("esc / h", "Back to inbox"))
		lines = append(lines, bind("N + l", "Open link N in browser"))
		lines = append(lines, bind("N + enter", "Open attachment N"))
		lines = append(lines, "")
		lines = append(lines, sectionStyle.Render("  Actions"))
		lines = append(lines, bind("r", "Reply"))
		lines = append(lines, bind("f", "Forward"))
		lines = append(lines, bind("d", "Trash message"))
		lines = append(lines, bind("P", "Open in browser"))
		lines = append(lines, bind("I", "Open all images"))
		lines = append(lines, "")
		lines = append(lines, sectionStyle.Render("  Other"))
		lines = append(lines, bind("K", "This help"))
		lines = append(lines, bind("q / ctrl+c", "Quit"))

	default:
		lines = append(lines, title)
		lines = append(lines, "")
		lines = append(lines, bind("K / esc", "Close help"))
	}

	lines = append(lines, "")
	lines = append(lines, lipgloss.NewStyle().Foreground(common.Muted).Render("  Press K or esc to close"))

	content := strings.Join(lines, "\n")

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(common.Secondary).
		Padding(1, 2)

	rendered := box.Render(content)

	return lipgloss.Place(a.width, a.height, lipgloss.Center, lipgloss.Center, rendered)
}

// renderSwitcherOverlay draws the account switcher on top of the base view.
func (a App) renderSwitcherOverlay(base string) string {
	innerWidth := 36

	var lines []string

	// Title
	title := lipgloss.NewStyle().Bold(true).Foreground(common.Primary).Render("Accounts")
	lines = append(lines, title)
	lines = append(lines, "")

	for i, acct := range a.cfg.Accounts {
		indicator := "  "
		if acct.Email == a.active.email {
			indicator = "● "
		}
		name := acct.Name
		if name == "" {
			name = acct.Email
		}
		line := indicator + name
		email := "  " + acct.Email

		if i == a.switcherCursor && a.editingAccount {
			lines = append(lines, indicator+a.nameInput.View())
			lines = append(lines, lipgloss.NewStyle().Foreground(common.Muted).Render(email))
		} else if i == a.switcherCursor {
			nameStyle := lipgloss.NewStyle().Bold(true).Foreground(common.White).Background(common.Primary)
			lines = append(lines, nameStyle.Render(padRight(line, innerWidth)))
			lines = append(lines, lipgloss.NewStyle().Foreground(common.Muted).Render(email))
		} else {
			lines = append(lines, line)
			lines = append(lines, lipgloss.NewStyle().Foreground(common.Muted).Render(email))
		}
	}

	// Separator + Add account
	lines = append(lines, "")
	addIdx := len(a.cfg.Accounts)
	addText := "  + Add account"
	if a.switcherCursor == addIdx && !a.addingAccount {
		addStyle := lipgloss.NewStyle().Bold(true).Foreground(common.White).Background(common.Primary)
		lines = append(lines, addStyle.Render(padRight(addText, innerWidth)))
	} else {
		lines = append(lines, lipgloss.NewStyle().Foreground(common.Accent).Render(addText))
	}

	// Name input (if adding)
	if a.addingAccount {
		lines = append(lines, "")
		if a.authPending {
			lines = append(lines, lipgloss.NewStyle().Foreground(common.Warning).Italic(true).Render("  Opening browser..."))
		} else {
			lines = append(lines, lipgloss.NewStyle().Foreground(common.Muted).Render("  Name: ")+a.nameInput.View())
		}
	}

	// Help
	lines = append(lines, "")
	lines = append(lines, lipgloss.NewStyle().Foreground(common.Muted).Render("j/k  enter  e=edit  x=remove  esc"))

	content := strings.Join(lines, "\n")

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(common.Secondary).
		Padding(1, 2)

	rendered := box.Render(content)

	return lipgloss.Place(a.width, a.height, lipgloss.Center, lipgloss.Center, rendered)
}

func padRight(s string, width int) string {
	w := lipgloss.Width(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}
