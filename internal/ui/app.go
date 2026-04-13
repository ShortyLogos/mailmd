package ui

import (
	"context"
	"fmt"
	"html"
	"net/mail"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/deric/mailmd/internal/auth"
	"github.com/deric/mailmd/internal/config"
	"github.com/deric/mailmd/internal/contacts"
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
	Ctx            context.Context
	Client         gmail.Client
	Cfg            config.Config
	CfgPath        string
	ClientID       string
	ClientSecret   string
	ConfigDir      string
	ActiveEmail    string
	InitialCompose *common.ComposeMsg // if set, open compose dialog on startup
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

type fetchReplyAllResultMsg struct {
	msg *gmail.Message
	err error
}

type fetchDraftResultMsg struct {
	msg *gmail.Message
	err error
}

type fetchSendDraftResultMsg struct {
	msg *gmail.Message
	err error
}

type authResultMsg struct {
	client gmail.Client
	email  string
	name   string
	err    error
}

type gmailSignatureImportedMsg struct {
	html string
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

	// Startup
	initialCompose *common.ComposeMsg

	// Help overlay
	showHelp bool

	// Undo-send
	pendingSend      *common.QueueSendMsg
	undoCountdown    int // seconds remaining
	pendingComposer  composer.Model // preserved for undo → re-open preview

	// Account switcher
	showSwitcher   bool
	switcherCursor int
	addingAccount  bool
	editingAccount bool
	nameInput      textinput.Model
	authPending    bool

	// Settings panel
	showSettings      bool
	settingsCursor    int
	settingsNaming    bool // true when typing name for new signature
	settingsNameInput textinput.Model

	// Compose dialog
	showComposeDialog   bool
	composeField        composeDialogField
	composeTo           []string
	composeCC           []string
	composeBCC          []string
	composeAttachments  []gmail.AttachmentFile
	composeToInput      textinput.Model
	composeCCInput      textinput.Model
	composeBCCInput     textinput.Model
	composeSubjectInput textinput.Model
	composeAttInput     textinput.Model
	composeSuggestions  []string
	composeSuggCursor   int
	composeSugLoading   bool
	composeThreadID     string
	composeInReplyTo    string
	composeBody         string
	composeDraftID      string
	composeTitle        string
	showCCField         bool
	showBCCField        bool
	showAttField        bool
	composeSignatureIdx int  // index into account's Signatures (-1 for none)
	showSigField        bool // show signature picker in compose dialog
	contactCache        []string
}

// fileSuggestionsMsg carries async file path completion results.
type fileSuggestionsMsg struct {
	suggestions []string
	query       string // the input that triggered this lookup
}

type composeDialogField int

const (
	composeFieldTo composeDialogField = iota
	composeFieldCC
	composeFieldBCC
	composeFieldSubject
	composeFieldSignature
	composeFieldAttachments
)

func New(opts AppOptions) App {
	ti := textinput.New()
	ti.Placeholder = "Account name (e.g. Work)"
	ti.CharLimit = 64

	toInput := textinput.New()
	toInput.Placeholder = "Type email address..."
	toInput.CharLimit = 256

	ccInput := textinput.New()
	ccInput.Placeholder = "Type email address..."
	ccInput.CharLimit = 256

	bccInput := textinput.New()
	bccInput.Placeholder = "Type email address..."
	bccInput.CharLimit = 256

	subjectInput := textinput.New()
	subjectInput.Placeholder = "Subject..."
	subjectInput.CharLimit = 256

	attInput := textinput.New()
	attInput.Placeholder = "File path (tab to skip)..."
	attInput.CharLimit = 512

	settingsNameInput := textinput.New()
	settingsNameInput.Placeholder = "Signature name..."
	settingsNameInput.CharLimit = 64

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
		ctx:                 opts.Ctx,
		cfgPath:             opts.CfgPath,
		cfg:                 opts.Cfg,
		clientID:            opts.ClientID,
		clientSecret:        opts.ClientSecret,
		configDir:           opts.ConfigDir,
		active:              state,
		states:              states,
		initted:             map[string]bool{opts.ActiveEmail: true},
		screen:              screenInbox,
		nameInput:           ti,
		settingsNameInput:   settingsNameInput,
		composeToInput:      toInput,
		composeCCInput:      ccInput,
		composeBCCInput:     bccInput,
		composeSubjectInput: subjectInput,
		composeAttInput:     attInput,
		initialCompose:      opts.InitialCompose,
	}
}

func (a App) Init() tea.Cmd {
	cmds := []tea.Cmd{a.active.inbox.Init()}
	if a.initialCompose != nil {
		msg := *a.initialCompose
		a.initialCompose = nil
		cmds = append(cmds, func() tea.Msg { return msg })
	}
	return tea.Batch(cmds...)
}

// queueDraftSend converts a draft message and queues it for sending with undo.
func (a *App) queueDraftSend(msg *gmail.Message) tea.Cmd {
	htmlBody := msg.Body
	plainBody := msg.Body
	if msg.Body != "" {
		if h, err := markdown.Convert(msg.Body); err == nil {
			htmlBody = h
		}
		plainBody = markdown.ConvertPlain(msg.Body)
	}
	queueMsg := common.QueueSendMsg{
		To:        msg.To,
		CC:        msg.CC,
		Subject:   msg.Subject,
		HTMLBody:  htmlBody,
		PlainBody: plainBody,
		DraftID:   msg.ID,
	}
	return func() tea.Msg { return queueMsg }
}

// activeComposeSignature returns the signature body for the currently selected
// compose signature index, or empty string if none selected.
func (a App) activeComposeSignature() string {
	acct := a.activeAccountConst()
	if acct == nil || a.composeSignatureIdx < 0 || a.composeSignatureIdx >= len(acct.Signatures) {
		return ""
	}
	return acct.Signatures[a.composeSignatureIdx].Body
}

// activeAccount returns a mutable pointer to the active account in the config slice.
func (a App) activeAccount() *config.Account {
	for i := range a.cfg.Accounts {
		if a.cfg.Accounts[i].Email == a.active.email {
			return &a.cfg.Accounts[i]
		}
	}
	return nil
}

// activeAccountConst is an alias for activeAccount for use in value-receiver methods.
func (a App) activeAccountConst() *config.Account {
	return a.activeAccount()
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
		a.active.inbox.SetStatus("Switched to " + msg.name)
		sizeCmd := func() tea.Msg { return tea.WindowSizeMsg{Width: a.width, Height: a.height} }
		return a, tea.Batch(a.active.inbox.Init(), sizeCmd)

	// --- Cross-screen transitions ---

	case common.FetchMessageMsg:
		id := msg.ID
		a.active.inbox.MarkRead(id)
		if cached, ok := a.active.msgCache[id]; ok {
			a.active.inbox.SetStatus("")
			a.reader = reader.New(a.ctx, a.active.client, cached, a.width, a.height, a.active.inbox.TabIdx(), a.active.inbox.FolderNames(), a.activeAccountName(), a.active.email)
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
			cmd := a.openComposeDialog(
				[]string{cached.From}, nil, nil, "Re: "+cached.Subject,
				quoteBodyText(cached.Body),
				cached.ThreadID, cached.MessageID, "", "Reply", nil,
			)
			return a, cmd
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
		cmd := a.openComposeDialog(
			[]string{msg.msg.From}, nil, nil, "Re: "+msg.msg.Subject,
			quoteBodyText(msg.msg.Body),
			msg.msg.ThreadID, msg.msg.MessageID, "", "Reply", nil,
		)
		return a, cmd

	case common.FetchAndReplyAllMsg:
		id := msg.ID
		if cached, ok := a.active.msgCache[id]; ok {
			to, cc := buildReplyAllRecipients(cached, a.active.email)
			cmd := a.openComposeDialog(
				to, cc, nil, "Re: "+cached.Subject,
				quoteBodyText(cached.Body),
				cached.ThreadID, cached.MessageID, "", "Reply All", nil,
			)
			return a, cmd
		}
		a.loading = true
		return a, func() tea.Msg {
			full, err := a.active.client.GetMessage(a.ctx, id)
			return fetchReplyAllResultMsg{msg: full, err: err}
		}

	case fetchReplyAllResultMsg:
		a.loading = false
		if msg.err != nil {
			a.active.inbox.SetStatus(fmt.Sprintf("Error: %v", msg.err))
			return a, nil
		}
		a.active.msgCache[msg.msg.ID] = msg.msg
		to, cc := buildReplyAllRecipients(msg.msg, a.active.email)
		cmd := a.openComposeDialog(
			to, cc, nil, "Re: "+msg.msg.Subject,
			quoteBodyText(msg.msg.Body),
			msg.msg.ThreadID, msg.msg.MessageID, "", "Reply All", nil,
		)
		return a, cmd

	case common.EditDraftMsg:
		id := msg.ID
		if cached, ok := a.active.msgCache[id]; ok {
			to := splitAddresses(cached.To)
			cc := splitAddresses(cached.CC)
			cmd := a.openComposeDialog(to, cc, nil, cached.Subject, cached.Body, "", "", id, "Edit Draft", nil)
			return a, cmd
		}
		a.active.inbox.SetLoadingStatus("Opening draft...")
		a.loading = true
		return a, tea.Batch(
			func() tea.Msg {
				full, err := a.active.client.GetMessage(a.ctx, id)
				return fetchDraftResultMsg{msg: full, err: err}
			},
			a.active.inbox.SpinnerTick(),
		)

	case common.SendDraftMsg:
		id := msg.ID
		if cached, ok := a.active.msgCache[id]; ok {
			return a, a.queueDraftSend(cached)
		}
		a.active.inbox.SetLoadingStatus("Preparing to send...")
		a.loading = true
		return a, tea.Batch(
			func() tea.Msg {
				full, err := a.active.client.GetMessage(a.ctx, id)
				return fetchSendDraftResultMsg{msg: full, err: err}
			},
			a.active.inbox.SpinnerTick(),
		)

	case fetchSendDraftResultMsg:
		a.loading = false
		if msg.err != nil {
			a.active.inbox.SetStatus(fmt.Sprintf("Error: %v", msg.err))
			return a, nil
		}
		a.active.msgCache[msg.msg.ID] = msg.msg
		return a, a.queueDraftSend(msg.msg)

	case fetchDraftResultMsg:
		a.loading = false
		if msg.err != nil {
			a.active.inbox.SetStatus(fmt.Sprintf("Error: %v", msg.err))
			return a, nil
		}
		a.active.msgCache[msg.msg.ID] = msg.msg
		to := splitAddresses(msg.msg.To)
		cc := splitAddresses(msg.msg.CC)
		cmd := a.openComposeDialog(to, cc, nil, msg.msg.Subject, msg.msg.Body, "", "", msg.msg.ID, "Edit Draft", nil)
		return a, cmd

	case fetchMsgResultMsg:
		a.loading = false
		a.active.inbox.SetStatus("")
		if msg.err != nil {
			a.active.inbox.SetStatus(fmt.Sprintf("Error: %v", msg.err))
			return a, nil
		}
		a.active.msgCache[msg.msg.ID] = msg.msg
		a.active.inbox.MarkRead(msg.msg.ID)
		a.reader = reader.New(a.ctx, a.active.client, msg.msg, a.width, a.height, a.active.inbox.TabIdx(), a.active.inbox.FolderNames(), a.activeAccountName(), a.active.email)
		a.screen = screenReader
		return a, tea.Batch(a.reader.Init(), a.markAsRead(msg.msg.ID))

	case common.OpenMessageMsg:
		a.reader = reader.New(a.ctx, a.active.client, msg.Message, a.width, a.height, a.active.inbox.TabIdx(), a.active.inbox.FolderNames(), a.activeAccountName(), a.active.email)
		a.screen = screenReader
		return a, a.reader.Init()

	case common.ComposeMsg:
		title := msg.Title
		if title == "" {
			title = "Compose"
		}
		cmd := a.openComposeDialog(msg.To, msg.CC, msg.BCC, msg.Subject, msg.Body, msg.ThreadID, msg.InReplyTo, msg.DraftID, title, msg.Attachments)
		return a, cmd

	case common.TrashFromReaderMsg:
		a.screen = screenInbox
		id := msg.ID
		a.active.inbox.OptimisticRemove(id)
		delete(a.active.msgCache, id)
		label := a.active.inbox.CurrentLabelID()
		if label == "TRASH" {
			a.active.inbox.SetStatus("Deleting message...")
			return a, tea.Batch(a.active.inbox.SpinnerTick(), func() tea.Msg {
				err := a.active.client.DeleteMessage(a.ctx, id)
				if err != nil {
					return common.StatusMsg{Text: "Error: " + err.Error()}
				}
				return common.StatusMsg{Text: "Message permanently deleted."}
			})
		}
		a.active.inbox.SetStatus("Trashing message...")
		return a, tea.Batch(a.active.inbox.SpinnerTick(), func() tea.Msg {
			err := a.active.client.TrashMessage(a.ctx, id)
			if err != nil {
				return common.StatusMsg{Text: "Error: " + err.Error()}
			}
			return common.StatusMsg{Text: "Message trashed."}
		})

	case common.ArchiveFromReaderMsg:
		a.screen = screenInbox
		id := msg.ID
		a.active.inbox.OptimisticRemove(id)
		delete(a.active.msgCache, id)
		a.active.inbox.SetStatus("Archiving message...")
		return a, tea.Batch(a.active.inbox.SpinnerTick(), func() tea.Msg {
			err := a.active.client.MoveMessage(a.ctx, id, nil, []string{"INBOX"})
			if err != nil {
				return common.StatusMsg{Text: "Error: " + err.Error()}
			}
			return common.StatusMsg{Text: "Message archived."}
		})

	case common.EditHeadersMsg:
		a.screen = screenInbox
		cmd := a.openComposeDialog(msg.To, msg.CC, msg.BCC, msg.Subject, msg.Body, msg.ThreadID, msg.InReplyTo, msg.DraftID, "Edit Headers", msg.Attachments)
		return a, cmd

	case common.SaveDraftMsg:
		a.screen = screenInbox
		a.active.inbox.SetStatus("Saving draft...")
		// Persist recipient addresses to contacts cache
		var addrs []string
		if msg.To != "" {
			addrs = append(addrs, strings.Split(msg.To, ",")...)
		}
		if msg.CC != "" {
			addrs = append(addrs, strings.Split(msg.CC, ",")...)
		}
		if len(addrs) > 0 {
			go contacts.Add(a.contactsPath(), addrs...)
		}
		client := a.active.client
		ctx := a.ctx
		return a, tea.Batch(
			a.active.inbox.SpinnerTick(),
			func() tea.Msg {
				htmlBody, plainBody := "", msg.Body
				if msg.Body != "" {
					if html, err := markdown.Convert(msg.Body); err == nil {
						htmlBody = html
					}
					plainBody = markdown.ConvertPlain(msg.Body)
				}
				err := client.CreateDraft(ctx, msg.To, msg.CC, msg.BCC, msg.Subject, htmlBody, plainBody, msg.Attachments)
				return common.DraftSavedMsg{Err: err}
			},
		)

	case common.DraftSavedMsg:
		if msg.Err != nil {
			a.active.inbox.SetStatus(fmt.Sprintf("Draft save failed: %v", msg.Err))
		} else {
			a.active.inbox.SetStatus("Draft saved.")
		}
		return a, nil

	case common.BackToInboxMsg:
		a.screen = screenInbox
		a.active.inbox.SetStatus("")
		return a, a.active.inbox.SpinnerTick()

	case common.QueueSendMsg:
		a.screen = screenInbox
		a.pendingSend = &msg
		a.pendingComposer = a.composer
		a.undoCountdown = 5
		a.active.inbox.SetStatus(undoSendStatus(a.undoCountdown))
		return a, tea.Tick(time.Second, func(time.Time) tea.Msg { return common.UndoSendTickMsg{} })

	case common.UndoSendTickMsg:
		if a.pendingSend == nil {
			return a, nil
		}
		a.undoCountdown--
		if a.undoCountdown <= 0 {
			// Timer expired — send now
			pending := a.pendingSend
			a.pendingSend = nil
			client := a.active.client
			ctx := a.ctx
			a.active.inbox.SetStatus("Sending...")
			// Persist contacts
			var addrs []string
			if pending.To != "" {
				addrs = append(addrs, strings.Split(pending.To, ",")...)
			}
			if pending.CC != "" {
				addrs = append(addrs, strings.Split(pending.CC, ",")...)
			}
			if len(addrs) > 0 {
				go contacts.Add(a.contactsPath(), addrs...)
			}
			return a, func() tea.Msg {
				var err error
				if pending.ThreadID != "" {
					err = client.ReplyMessage(ctx, pending.ThreadID, pending.InReplyTo, pending.To, pending.CC, pending.BCC, pending.Subject, pending.HTMLBody, pending.PlainBody, pending.Attachments)
				} else {
					err = client.SendMessage(ctx, pending.To, pending.CC, pending.BCC, pending.Subject, pending.HTMLBody, pending.PlainBody, pending.Attachments)
				}
				if err != nil {
					return common.SendResultMsg{Err: err}
				}
				if pending.DraftID != "" {
					client.TrashMessage(ctx, pending.DraftID)
				}
				return common.SendResultMsg{Err: nil}
			}
		}
		a.active.inbox.SetStatus(undoSendStatus(a.undoCountdown))
		return a, tea.Tick(time.Second, func(time.Time) tea.Msg { return common.UndoSendTickMsg{} })

	case common.UndoSendMsg:
		if a.pendingSend == nil {
			return a, nil
		}
		a.pendingSend = nil
		// Re-open the composer in preview mode
		a.composer = a.pendingComposer
		a.screen = screenCompose
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

	case fileSuggestionsMsg:
		// Only apply if the compose dialog is still showing attachments
		// and the query matches current input (avoid stale results)
		if a.showComposeDialog && a.composeField == composeFieldAttachments {
			current := a.composeAttInput.Value()
			if current == msg.query {
				a.composeSuggestions = msg.suggestions
				a.composeSuggCursor = -1
				a.composeSugLoading = false
			}
		}
		return a, nil

	case common.StatusMsg:
		var cmd tea.Cmd
		a.active.inbox, cmd = a.active.inbox.Update(msg)
		return a, cmd

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
		plain := stripHTMLSimple(msg.html)
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
	}

	// --- Key interception (before screen delegation) ---
	if kmsg, ok := msg.(tea.KeyMsg); ok {
		// Undo-send: U cancels a pending send
		if a.pendingSend != nil && a.screen == screenInbox && kmsg.String() == "U" {
			return a.Update(common.UndoSendMsg{})
		}

		// Help overlay
		if a.showHelp {
			if kmsg.String() == "K" || kmsg.String() == "esc" || kmsg.String() == "q" {
				a.showHelp = false
			}
			return a, nil
		}
		if kmsg.String() == "K" && !a.active.inbox.IsInputActive() {
			a.showHelp = true
			return a, nil
		}

		if a.showComposeDialog {
			return a.updateComposeDialog(kmsg)
		}
		if a.showSettings {
			return a.updateSettings(kmsg)
		}
		if a.showSwitcher {
			return a.updateSwitcher(kmsg)
		}
		// S opens switcher from inbox (only when not in search/jump/compose)
		if a.screen == screenInbox && kmsg.String() == "S" && !a.active.inbox.IsInputActive() {
			a.showSwitcher = true
			a.switcherCursor = 0
			a.addingAccount = false
			a.authPending = false
			return a, nil
		}
		// , opens settings from inbox (only when not in search/jump/compose)
		if a.screen == screenInbox && kmsg.String() == "," && !a.active.inbox.IsInputActive() {
			a.showSettings = true
			a.settingsCursor = 0
			a.settingsNaming = false
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

	default:
		// Number keys 1-9 jump to and select the corresponding account
		s := msg.String()
		if len(s) == 1 && s[0] >= '1' && s[0] <= '9' {
			idx := int(s[0]-'0') - 1
			if idx < len(a.cfg.Accounts) {
				acct := a.cfg.Accounts[idx]
				if acct.Email == a.active.email {
					a.showSwitcher = false
					return a, nil
				}
				return a.switchToAccount(acct)
			}
		}
	}
	return a, nil
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
		a.settingsNaming = true
		a.settingsNameInput.SetValue("")
		a.settingsNameInput.Focus()
		return a, textinput.Blink

	case key.Matches(msg, key.NewBinding(key.WithKeys("e"))):
		if acct != nil && a.settingsCursor < sigCount {
			return a, a.editSignatureInEditor(a.settingsCursor)
		}
		return a, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("d"))):
		if acct != nil && a.settingsCursor < sigCount {
			acct.Signatures = append(acct.Signatures[:a.settingsCursor], acct.Signatures[a.settingsCursor+1:]...)
			if a.settingsCursor >= len(acct.Signatures) && a.settingsCursor > 0 {
				a.settingsCursor--
			}
			_ = config.Save(a.cfgPath, a.cfg)
		}
		return a, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("*"))):
		if acct != nil && a.settingsCursor < sigCount {
			for i := range acct.Signatures {
				acct.Signatures[i].IsDefault = (i == a.settingsCursor)
			}
			_ = config.Save(a.cfgPath, a.cfg)
		}
		return a, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("i"))):
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
			a.active.inbox.SetStatus("Switched to " + acct.Name)
			return a, tea.Batch(a.active.inbox.Init(), sizeCmd)
		}
		a.active.inbox.SetStatus("Switched to " + acct.Name)
		return a, nil
	}

	// Not cached — authenticate and create state
	a.showSwitcher = false
	a.active.inbox.SetStatus("Switching to " + acct.Name + "...")
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
	if a.showComposeDialog {
		return a.renderComposeOverlay(base)
	}
	if a.showSettings {
		return a.renderSettingsOverlay(base)
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
		lines = append(lines, bind("d", "Trash / delete message"))
		lines = append(lines, "")
		lines = append(lines, sectionStyle.Render("  Actions"))
		lines = append(lines, bind("c", "Compose new email"))
		lines = append(lines, bind("r", "Reply to message"))
		lines = append(lines, bind("R", "Reply all"))
		lines = append(lines, bind("e", "Edit draft (in Drafts)"))
		lines = append(lines, bind("b", "Block sender (auto-trash)"))
		lines = append(lines, bind("m", "Toggle read / unread"))
		lines = append(lines, bind("u", "Restore from trash"))
		lines = append(lines, bind("space", "Select message"))
		lines = append(lines, bind("a", "Select / deselect all"))
		lines = append(lines, "")
		lines = append(lines, sectionStyle.Render("  Other"))
		lines = append(lines, bind("f / /", "Search"))
		lines = append(lines, bind("ctrl+r", "Refresh"))
		lines = append(lines, bind("p", "Toggle preview"))
		lines = append(lines, bind("S", "Switch account"))
		lines = append(lines, bind(",", "Account settings"))
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
		lines = append(lines, bind("R", "Reply all"))
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

	numStyle := lipgloss.NewStyle().Foreground(common.Muted)
	for i, acct := range a.cfg.Accounts {
		indicator := "  "
		if acct.Email == a.active.email {
			indicator = "● "
		}
		name := acct.Name
		if name == "" {
			name = acct.Email
		}
		prefix := fmt.Sprintf("[%d] ", i+1)
		line := indicator + prefix + name
		email := "     " + acct.Email

		if i == a.switcherCursor && a.editingAccount {
			lines = append(lines, indicator+prefix+a.nameInput.View())
			lines = append(lines, lipgloss.NewStyle().Foreground(common.Muted).Render(email))
		} else if i == a.switcherCursor {
			nameStyle := lipgloss.NewStyle().Bold(true).Foreground(common.White).Background(common.Primary)
			lines = append(lines, nameStyle.Render(padRight(line, innerWidth)))
			lines = append(lines, lipgloss.NewStyle().Foreground(common.Muted).Render(email))
		} else {
			lines = append(lines, indicator+numStyle.Render(prefix)+name)
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

// renderSettingsOverlay draws the account settings panel on top of the base view.
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

	if a.settingsNaming {
		lines = append(lines, "")
		lines = append(lines, lipgloss.NewStyle().Foreground(common.Muted).Render("  Name: ")+a.settingsNameInput.View())
	}

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

// stripHTMLSimple does a basic HTML-to-text conversion for signatures.
func stripHTMLSimple(s string) string {
	for _, tag := range []string{"<br>", "<br/>", "<br />", "<BR>", "<p>", "<P>", "<div>", "<DIV>"} {
		s = strings.ReplaceAll(s, tag, "\n")
	}
	s = regexp.MustCompile(`</[^>]+>`).ReplaceAllString(s, "")
	s = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = strings.TrimSpace(s)
	return s
}

var undoSendStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#38BDF8")) // sky blue

func undoSendStatus(seconds int) string {
	return undoSendStyle.Render(fmt.Sprintf("Message queued — U to undo (%ds)", seconds))
}

func formatFileSize(bytes int64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}
	if bytes < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(bytes)/(1024*1024))
}

// --- Compose dialog helpers ---

func quoteBodyText(body string) string {
	var b strings.Builder
	b.WriteString("\n")
	for _, line := range strings.Split(body, "\n") {
		b.WriteString("> " + line + "\n")
	}
	return b.String()
}

func splitAddresses(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func buildReplyAllRecipients(msg *gmail.Message, currentEmail string) (to, cc []string) {
	currentLower := strings.ToLower(currentEmail)
	seen := make(map[string]bool)
	seen[currentLower] = true // exclude self

	tryAdd := func(addr string) (string, bool) {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			return "", false
		}
		bare := extractEmail(addr)
		key := strings.ToLower(bare)
		if key == "" || seen[key] {
			return "", false
		}
		seen[key] = true
		return addr, true
	}

	// From → To (original sender goes first)
	if addr, ok := tryAdd(msg.From); ok {
		to = append(to, addr)
	}

	// Original To → To
	for _, a := range splitAddresses(msg.To) {
		if addr, ok := tryAdd(a); ok {
			to = append(to, addr)
		}
	}

	// Original CC → CC
	for _, a := range splitAddresses(msg.CC) {
		if addr, ok := tryAdd(a); ok {
			cc = append(cc, addr)
		}
	}

	// Fallback: ensure at least one recipient
	if len(to) == 0 && len(cc) == 0 {
		to = []string{msg.From}
	}

	return to, cc
}

// --- Compose dialog ---

func (a *App) contactsPath() string {
	// Per-account contacts file so suggestions don't leak between accounts
	safe := strings.ReplaceAll(a.active.email, "/", "_")
	return filepath.Join(a.configDir, "mailmd", "contacts-"+safe+".json")
}

func (a *App) openComposeDialog(to, cc, bcc []string, subject, body, threadID, inReplyTo, draftID, title string, attachments []gmail.AttachmentFile) tea.Cmd {
	a.showComposeDialog = true
	a.composeField = composeFieldTo
	a.composeTo = to
	a.composeCC = cc
	a.composeBCC = bcc
	a.composeAttachments = attachments
	a.composeBody = body
	a.composeThreadID = threadID
	a.composeInReplyTo = inReplyTo
	a.composeDraftID = draftID
	a.composeTitle = title
	a.composeSuggCursor = -1
	a.composeSuggestions = nil

	a.composeToInput.SetValue("")
	a.composeToInput.Focus()
	a.composeCCInput.SetValue("")
	a.composeCCInput.Blur()
	a.composeBCCInput.SetValue("")
	a.composeBCCInput.Blur()
	a.composeSubjectInput.SetValue(subject)
	a.composeSubjectInput.Blur()
	a.composeAttInput.SetValue("")
	a.composeAttInput.Blur()

	// Show optional fields only if pre-populated
	a.showCCField = len(cc) > 0
	a.showBCCField = len(bcc) > 0
	a.showAttField = len(attachments) > 0

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

	a.contactCache = a.buildContactCache()

	return textinput.Blink
}

func (a *App) buildContactCache() []string {
	seen := make(map[string]bool)
	var result []string

	// 1. Persistent contacts (sorted by most-recently-used)
	for _, addr := range contacts.All(a.contactsPath()) {
		email := extractEmail(addr)
		key := strings.ToLower(email)
		if key != "" && !seen[key] {
			seen[key] = true
			result = append(result, addr)
		}
	}

	// 2. In-memory addresses from inbox folder caches
	for _, addr := range a.active.inbox.RecentAddresses() {
		email := extractEmail(addr)
		key := strings.ToLower(email)
		if key != "" && !seen[key] {
			seen[key] = true
			result = append(result, addr)
		}
	}

	// 3. Full message cache (From, To, CC)
	for _, msg := range a.active.msgCache {
		for _, addr := range []string{msg.From, msg.To, msg.CC} {
			if addr == "" {
				continue
			}
			// May be comma-separated
			for _, part := range strings.Split(addr, ",") {
				part = strings.TrimSpace(part)
				email := extractEmail(part)
				key := strings.ToLower(email)
				if key != "" && !seen[key] {
					seen[key] = true
					result = append(result, part)
				}
			}
		}
	}

	return result
}

// extractEmail pulls the bare email from an RFC 5322 address string.
func extractEmail(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	addr, err := mail.ParseAddress(s)
	if err != nil {
		if strings.Contains(s, "@") {
			return s
		}
		return ""
	}
	return addr.Address
}

func (a *App) filterSuggestions(input string, exclude []string) []string {
	if input == "" {
		return nil
	}
	lower := strings.ToLower(input)
	excludeSet := make(map[string]bool, len(exclude))
	for _, e := range exclude {
		excludeSet[strings.ToLower(extractEmail(e))] = true
	}

	var result []string

	// Contact groups
	for name, members := range a.cfg.ContactGroups {
		if strings.Contains(strings.ToLower(name), lower) && len(members) > 0 {
			result = append(result, fmt.Sprintf("group:%s (%d members)", name, len(members)))
			if len(result) >= 5 {
				return result
			}
		}
	}

	for _, addr := range a.contactCache {
		email := extractEmail(addr)
		if excludeSet[strings.ToLower(email)] {
			continue
		}
		if strings.Contains(strings.ToLower(addr), lower) {
			result = append(result, addr)
			if len(result) >= 5 {
				break
			}
		}
	}
	return result
}

func (a *App) composeDialogActiveInput() *textinput.Model {
	switch a.composeField {
	case composeFieldTo:
		return &a.composeToInput
	case composeFieldCC:
		return &a.composeCCInput
	case composeFieldBCC:
		return &a.composeBCCInput
	case composeFieldAttachments:
		return &a.composeAttInput
	default:
		return &a.composeSubjectInput
	}
}

// composeFieldOrder returns the active field sequence based on which optional fields are shown.
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

func (a *App) composeDialogAdvanceField() {
	order := a.composeFieldOrder()
	for i, f := range order {
		if f == a.composeField && i+1 < len(order) {
			a.composeDialogFocusField(order[i+1])
			break
		}
	}
	a.composeSuggCursor = -1
	a.composeSuggestions = nil
}

func (a *App) composeDialogRetreatField() {
	order := a.composeFieldOrder()
	for i, f := range order {
		if f == a.composeField && i > 0 {
			a.composeDialogFocusField(order[i-1])
			break
		}
	}
	a.composeSuggCursor = -1
	a.composeSuggestions = nil
}

// composeDialogIsLastField returns true if the current field is the last in the order.
func (a *App) composeDialogIsLastField() bool {
	order := a.composeFieldOrder()
	return len(order) > 0 && order[len(order)-1] == a.composeField
}

func (a App) updateComposeDialog(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Signature field — selector, not text input
	if a.composeField == composeFieldSignature {
		acct := a.activeAccountConst()
		sigCount := 0
		if acct != nil {
			sigCount = len(acct.Signatures)
		}
		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("up", "k"))):
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
			a.showComposeDialog = false
			return a, nil
		}
		return a, nil
	}

	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
		a.showComposeDialog = false
		// Save draft if there's content worth keeping
		to := strings.Join(a.composeTo, ", ")
		cc := strings.Join(a.composeCC, ", ")
		bcc := strings.Join(a.composeBCC, ", ")
		subject := a.composeSubjectInput.Value()
		body := a.composeBody
		atts := a.composeAttachments
		if to != "" || body != "" {
			return a, func() tea.Msg {
				return common.SaveDraftMsg{
					To: to, CC: cc, BCC: bcc, Subject: subject, Body: body,
					Attachments: atts,
				}
			}
		}
		return a, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+t"))):
		// Insert template — show template names as suggestions
		if len(a.cfg.Templates) > 0 {
			var names []string
			for name := range a.cfg.Templates {
				names = append(names, "tpl:"+name)
			}
			a.composeSuggestions = names
			a.composeSuggCursor = 0
		}
		return a, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+o"))):
		// Toggle CC field
		if !a.showCCField {
			a.showCCField = true
			a.composeDialogFocusField(composeFieldCC)
			a.composeSuggCursor = -1
			a.composeSuggestions = nil
		}
		return a, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+b"))):
		// Toggle BCC field
		if !a.showBCCField {
			a.showBCCField = true
			a.composeDialogFocusField(composeFieldBCC)
			a.composeSuggCursor = -1
			a.composeSuggestions = nil
		}
		return a, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+a"))):
		// Toggle Attachments field
		if !a.showAttField {
			a.showAttField = true
			a.composeDialogFocusField(composeFieldAttachments)
			a.composeSuggCursor = -1
			a.composeSuggestions = nil
		}
		return a, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("tab"))):
		// On To/CC/BCC: commit any typed text first
		if a.composeField == composeFieldTo || a.composeField == composeFieldCC || a.composeField == composeFieldBCC {
			a.commitComposeInput()
		}
		if a.composeDialogIsLastField() {
			return a.launchComposeEditor()
		}
		a.composeDialogAdvanceField()
		return a, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("shift+tab"))):
		if a.composeField == composeFieldTo || a.composeField == composeFieldCC || a.composeField == composeFieldBCC {
			a.commitComposeInput()
		}
		a.composeDialogRetreatField()
		return a, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
		if a.composeField == composeFieldSubject {
			if a.composeDialogIsLastField() {
				return a.launchComposeEditor()
			}
			a.composeDialogAdvanceField()
			return a, nil
		}
		if a.composeField == composeFieldAttachments {
			// Add file if valid, or launch editor if empty
			input := &a.composeAttInput
			val := strings.TrimSpace(input.Value())
			if a.composeSuggCursor >= 0 && a.composeSuggCursor < len(a.composeSuggestions) {
				val = a.composeSuggestions[a.composeSuggCursor]
			}
			if val != "" {
				// Expand ~ to home dir
				if strings.HasPrefix(val, "~/") {
					if home, err := os.UserHomeDir(); err == nil {
						val = filepath.Join(home, val[2:])
					}
				}
				// If it's a directory, set it as input for further navigation
				if info, err := os.Stat(val); err == nil && info.IsDir() {
					if !strings.HasSuffix(val, "/") {
						val += "/"
					}
					input.SetValue(val)
					return a, a.updateComposeSuggestions()
				}
				// If file exists, add it
				if _, err := os.Stat(val); err == nil {
					a.composeAttachments = append(a.composeAttachments, gmail.AttachmentFile{Path: val})
					input.SetValue("")
					a.composeSuggCursor = -1
					a.composeSuggestions = nil
				}
			} else {
				// Empty enter → launch editor
				return a.launchComposeEditor()
			}
			return a, nil
		}
		// On To/CC/BCC: add typed text or selected suggestion
		if a.composeField == composeFieldTo || a.composeField == composeFieldCC || a.composeField == composeFieldBCC {
			input := a.composeDialogActiveInput()
			val := strings.TrimSpace(input.Value())

			// If a suggestion is highlighted, use it
			if a.composeSuggCursor >= 0 && a.composeSuggCursor < len(a.composeSuggestions) {
				val = a.composeSuggestions[a.composeSuggCursor]
			}

			if val != "" {
				// Apply template
				if strings.HasPrefix(val, "tpl:") {
					tplName := strings.TrimPrefix(val, "tpl:")
					if tpl, ok := a.cfg.Templates[tplName]; ok {
						a.composeBody = tpl.Body
						if tpl.Subject != "" {
							a.composeSubjectInput.SetValue(tpl.Subject)
						}
					}
					a.composeDialogActiveInput().SetValue("")
					a.composeSuggCursor = -1
					a.composeSuggestions = nil
					return a, nil
				}
				// Expand contact group
				var vals []string
				if strings.HasPrefix(val, "group:") {
					groupName := strings.SplitN(strings.TrimPrefix(val, "group:"), " (", 2)[0]
					if members, ok := a.cfg.ContactGroups[groupName]; ok {
						vals = members
					}
				}
				if vals == nil {
					vals = []string{val}
				}
				switch a.composeField {
				case composeFieldTo:
					a.composeTo = append(a.composeTo, vals...)
				case composeFieldCC:
					a.composeCC = append(a.composeCC, vals...)
				case composeFieldBCC:
					a.composeBCC = append(a.composeBCC, vals...)
				}
				input.SetValue("")
				a.composeSuggCursor = -1
				a.composeSuggestions = nil
			} else {
				// Empty input + enter: advance if we have recipients
				if a.composeField == composeFieldTo && len(a.composeTo) > 0 {
					a.composeDialogAdvanceField()
				} else if a.composeField == composeFieldCC || a.composeField == composeFieldBCC {
					a.composeDialogAdvanceField()
				}
			}
			return a, nil
		}

	case key.Matches(msg, key.NewBinding(key.WithKeys("backspace"))):
		input := a.composeDialogActiveInput()
		if input.Value() == "" {
			// Remove last item from current list field
			if a.composeField == composeFieldTo && len(a.composeTo) > 0 {
				a.composeTo = a.composeTo[:len(a.composeTo)-1]
			} else if a.composeField == composeFieldCC && len(a.composeCC) > 0 {
				a.composeCC = a.composeCC[:len(a.composeCC)-1]
			} else if a.composeField == composeFieldBCC && len(a.composeBCC) > 0 {
				a.composeBCC = a.composeBCC[:len(a.composeBCC)-1]
			} else if a.composeField == composeFieldAttachments && len(a.composeAttachments) > 0 {
				a.composeAttachments = a.composeAttachments[:len(a.composeAttachments)-1]
			}
			return a, nil
		}
		// Let textinput handle backspace
		var cmd tea.Cmd
		*input, cmd = input.Update(msg)
		sugCmd := a.updateComposeSuggestions()
		return a, tea.Batch(cmd, sugCmd)

	case key.Matches(msg, key.NewBinding(key.WithKeys("up"))):
		if len(a.composeSuggestions) > 0 {
			a.composeSuggCursor--
			if a.composeSuggCursor < -1 {
				a.composeSuggCursor = len(a.composeSuggestions) - 1
			}
		}
		return a, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("down"))):
		if len(a.composeSuggestions) > 0 {
			a.composeSuggCursor++
			if a.composeSuggCursor >= len(a.composeSuggestions) {
				a.composeSuggCursor = -1
			}
		}
		return a, nil
	}

	// Default: delegate to active textinput
	input := a.composeDialogActiveInput()
	var cmd tea.Cmd
	*input, cmd = input.Update(msg)

	// Update suggestions after text change
	if a.composeField == composeFieldTo || a.composeField == composeFieldCC || a.composeField == composeFieldBCC || a.composeField == composeFieldAttachments {
		sugCmd := a.updateComposeSuggestions()
		return a, tea.Batch(cmd, sugCmd)
	}
	return a, cmd
}

func (a *App) commitComposeInput() {
	input := a.composeDialogActiveInput()
	val := strings.TrimSpace(input.Value())
	if val == "" {
		return
	}
	switch a.composeField {
	case composeFieldTo:
		a.composeTo = append(a.composeTo, val)
	case composeFieldCC:
		a.composeCC = append(a.composeCC, val)
	case composeFieldBCC:
		a.composeBCC = append(a.composeBCC, val)
	}
	input.SetValue("")
	a.composeSuggCursor = -1
	a.composeSuggestions = nil
}

func (a *App) updateComposeSuggestions() tea.Cmd {
	input := a.composeDialogActiveInput()
	val := input.Value()

	if a.composeField == composeFieldAttachments {
		a.composeSugLoading = true
		a.composeSuggestions = nil
		a.composeSuggCursor = -1
		query := val
		return func() tea.Msg {
			return fileSuggestionsMsg{suggestions: filterFileSuggestions(query), query: query}
		}
	}

	a.composeSugLoading = false
	var exclude []string
	switch a.composeField {
	case composeFieldTo:
		exclude = a.composeTo
	case composeFieldCC:
		exclude = a.composeCC
	case composeFieldBCC:
		exclude = a.composeBCC
	}
	a.composeSuggestions = a.filterSuggestions(val, exclude)
	a.composeSuggCursor = -1
	return nil
}

// filterFileSuggestions returns filesystem entries matching the typed path prefix.
func filterFileSuggestions(input string) []string {
	if input == "" {
		return nil
	}

	// Expand ~
	expanded := input
	if strings.HasPrefix(expanded, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			expanded = filepath.Join(home, expanded[2:])
		}
	}

	dir := filepath.Dir(expanded)
	prefix := filepath.Base(expanded)
	// If input ends with /, list the directory contents
	if strings.HasSuffix(input, "/") {
		dir = expanded
		prefix = ""
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var result []string
	for _, e := range entries {
		name := e.Name()
		// Skip hidden files unless the user is typing a dot
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(prefix, ".") {
			continue
		}
		if prefix != "" && !strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix)) {
			continue
		}
		full := filepath.Join(dir, name)
		// Show with the user's original prefix style (preserve ~/)
		display := full
		if strings.HasPrefix(input, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				display = "~/" + strings.TrimPrefix(full, home+"/")
			}
		}
		if e.IsDir() {
			display += "/"
		}
		result = append(result, display)
		if len(result) >= 8 {
			break
		}
	}
	return result
}

func (a App) launchComposeEditor() (tea.Model, tea.Cmd) {
	if len(a.composeTo) == 0 {
		// Don't launch editor without at least one recipient
		return a, nil
	}

	a.showComposeDialog = false

	to := strings.Join(a.composeTo, ", ")
	cc := strings.Join(a.composeCC, ", ")
	bcc := strings.Join(a.composeBCC, ", ")
	subject := a.composeSubjectInput.Value()

	body := a.composeBody
	// Append per-account signature if configured and body doesn't already contain it
	sig := a.activeComposeSignature()
	if sig != "" && !strings.Contains(body, sig) {
		if body != "" {
			body += "\n\n"
		}
		body += sig
	}

	a.composer = composer.NewWithMetadata(
		a.ctx, a.active.client, a.cfg.Editor(), body,
		a.width, a.height,
		to, cc, bcc, subject,
		a.composeThreadID, a.composeInReplyTo, a.composeDraftID,
		a.composeAttachments,
	)
	a.screen = screenCompose
	return a, a.composer.Init()
}

func (a App) renderComposeOverlay(base string) string {
	innerWidth := 56
	var lines []string

	title := lipgloss.NewStyle().Bold(true).Foreground(common.Primary).Render(a.composeTitle)
	lines = append(lines, title)
	lines = append(lines, "")

	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(common.White)
	valueStyle := lipgloss.NewStyle().Foreground(common.Accent)
	mutedStyle := lipgloss.NewStyle().Foreground(common.Muted)
	activeLabel := lipgloss.NewStyle().Bold(true).Foreground(common.Primary)
	suggHighlight := lipgloss.NewStyle().Bold(true).Foreground(common.White).Background(common.Primary)

	// To field
	toLabel := labelStyle
	if a.composeField == composeFieldTo {
		toLabel = activeLabel
	}
	toLine := toLabel.Render("To: ")
	if len(a.composeTo) > 0 {
		toLine += valueStyle.Render(strings.Join(a.composeTo, ", "))
	}
	lines = append(lines, toLine)
	if a.composeField == composeFieldTo {
		lines = append(lines, "  "+a.composeToInput.View())
		// Suggestions
		for i, s := range a.composeSuggestions {
			prefix := "  "
			if i == a.composeSuggCursor {
				lines = append(lines, suggHighlight.Render(padRight("  "+s, innerWidth)))
			} else {
				lines = append(lines, prefix+mutedStyle.Render(s))
			}
		}
	}

	// CC field (only shown when toggled)
	if a.showCCField {
		lines = append(lines, "")
		ccLabel := labelStyle
		if a.composeField == composeFieldCC {
			ccLabel = activeLabel
		}
		ccLine := ccLabel.Render("CC: ")
		if len(a.composeCC) > 0 {
			ccLine += valueStyle.Render(strings.Join(a.composeCC, ", "))
		} else if a.composeField != composeFieldCC {
			ccLine += mutedStyle.Render("(none)")
		}
		lines = append(lines, ccLine)
		if a.composeField == composeFieldCC {
			lines = append(lines, "  "+a.composeCCInput.View())
			for i, s := range a.composeSuggestions {
				if i == a.composeSuggCursor {
					lines = append(lines, suggHighlight.Render(padRight("  "+s, innerWidth)))
				} else {
					lines = append(lines, "  "+mutedStyle.Render(s))
				}
			}
		}
	}

	// BCC field (only shown when toggled with ctrl+b)
	if a.showBCCField {
		lines = append(lines, "")
		bccLabel := labelStyle
		if a.composeField == composeFieldBCC {
			bccLabel = activeLabel
		}
		bccLine := bccLabel.Render("BCC: ")
		if len(a.composeBCC) > 0 {
			bccLine += valueStyle.Render(strings.Join(a.composeBCC, ", "))
		} else if a.composeField != composeFieldBCC {
			bccLine += mutedStyle.Render("(none)")
		}
		lines = append(lines, bccLine)
		if a.composeField == composeFieldBCC {
			lines = append(lines, "  "+a.composeBCCInput.View())
			for i, s := range a.composeSuggestions {
				if i == a.composeSuggCursor {
					lines = append(lines, suggHighlight.Render(padRight("  "+s, innerWidth)))
				} else {
					lines = append(lines, "  "+mutedStyle.Render(s))
				}
			}
		}
	}

	lines = append(lines, "")

	// Subject field
	subjectLabel := labelStyle
	if a.composeField == composeFieldSubject {
		subjectLabel = activeLabel
	}
	if a.composeField == composeFieldSubject {
		lines = append(lines, subjectLabel.Render("Subject: ")+a.composeSubjectInput.View())
	} else {
		subjectVal := a.composeSubjectInput.Value()
		if subjectVal == "" {
			subjectVal = "(empty)"
		}
		lines = append(lines, subjectLabel.Render("Subject: ")+mutedStyle.Render(subjectVal))
	}

	lines = append(lines, "")

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
				"  "+mutedStyle.Render("↑/↓"))
		} else {
			lines = append(lines, sigLabel.Render("Signature: ")+mutedStyle.Render(sigName))
		}

		lines = append(lines, "")
	}

	// Attachments field (only shown when toggled)
	if a.showAttField {
		lines = append(lines, "")
		attLabel := labelStyle
		if a.composeField == composeFieldAttachments {
			attLabel = activeLabel
		}
		attLine := attLabel.Render("Attach: ")
		if len(a.composeAttachments) == 0 && a.composeField != composeFieldAttachments {
			attLine += mutedStyle.Render("(none)")
		}
		lines = append(lines, attLine)
		for _, att := range a.composeAttachments {
			size := ""
			if info, err := os.Stat(att.Path); err == nil {
				size = formatFileSize(info.Size())
			}
			lines = append(lines, "  "+valueStyle.Render(filepath.Base(att.Path))+" "+mutedStyle.Render(size))
		}
		if a.composeField == composeFieldAttachments {
			lines = append(lines, "  "+a.composeAttInput.View())
			if a.composeSugLoading {
				lines = append(lines, "  "+mutedStyle.Render("Loading..."))
			} else {
				for i, s := range a.composeSuggestions {
					if i == a.composeSuggCursor {
						lines = append(lines, suggHighlight.Render(padRight("  "+s, innerWidth)))
					} else {
						lines = append(lines, "  "+mutedStyle.Render(s))
					}
				}
			}
		}
	}

	lines = append(lines, "")

	// Help
	helpStyle := lipgloss.NewStyle().Foreground(common.Muted)

	// Build toggle hints for hidden optional fields
	var toggles string
	if !a.showCCField {
		toggles += "  ^o=CC"
	}
	if !a.showBCCField {
		toggles += "  ^b=BCC"
	}
	if !a.showAttField {
		toggles += "  ^a=attach"
	}
	if len(a.cfg.Templates) > 0 {
		toggles += "  ^t=template"
	}

	switch a.composeField {
	case composeFieldSubject:
		if a.composeDialogIsLastField() {
			lines = append(lines, helpStyle.Render("enter=open editor  tab=open editor"+toggles))
		} else {
			lines = append(lines, helpStyle.Render("enter=next  tab=next  shift+tab=back"+toggles))
		}
		lines = append(lines, helpStyle.Render("esc=cancel"))
	case composeFieldSignature:
		if a.composeDialogIsLastField() {
			lines = append(lines, helpStyle.Render("↑/↓=change  enter/tab=open editor"+toggles))
		} else {
			lines = append(lines, helpStyle.Render("↑/↓=change  enter/tab=next  shift+tab=back"+toggles))
		}
		lines = append(lines, helpStyle.Render("esc=cancel"))
	case composeFieldAttachments:
		lines = append(lines, helpStyle.Render("enter=add file  tab/enter(empty)=open editor"))
		lines = append(lines, helpStyle.Render("shift+tab=back  backspace=remove  esc=cancel"))
	default:
		lines = append(lines, helpStyle.Render("enter=add  tab=next  shift+tab=back"+toggles))
		lines = append(lines, helpStyle.Render("backspace on empty=remove last  esc=cancel"))
	}

	content := strings.Join(lines, "\n")

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(common.Secondary).
		Padding(1, 2).
		Width(innerWidth + 4) // +4 for padding

	rendered := box.Render(content)
	return lipgloss.Place(a.width, a.height, lipgloss.Center, lipgloss.Center, rendered)
}
