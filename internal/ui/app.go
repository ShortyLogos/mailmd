package ui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
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

// fetchMsgResultMsg carries the result of fetching a full message (for reader).
type fetchMsgResultMsg struct {
	msg *gmail.Message
	err error
}

// fetchReplyResultMsg carries the result of fetching a message for quick reply.
type fetchReplyResultMsg struct {
	msg *gmail.Message
	err error
}

// App is the root Bubble Tea model.
type App struct {
	ctx    context.Context
	client gmail.Client
	cfg    config.Config
	width  int
	height int

	active   screen
	inbox    inbox.Model
	reader   reader.Model
	composer composer.Model

	status   string
	loading  bool                    // true while fetching a message to open
	msgCache map[string]*gmail.Message // message ID → full message
}

// New creates and returns the root app model.
func New(ctx context.Context, client gmail.Client, cfg config.Config) App {
	return App{
		ctx:      ctx,
		client:   client,
		cfg:      cfg,
		active:   screenInbox,
		inbox:    inbox.New(ctx, client),
		msgCache: make(map[string]*gmail.Message),
	}
}

// Init delegates to the active screen.
func (a App) Init() tea.Cmd {
	return a.inbox.Init()
}

// Update is the root message dispatcher.
func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		// Forward to all sub-models so they are sized correctly even when not active.
		var cmds []tea.Cmd
		var cmd tea.Cmd
		a.inbox, cmd = a.inbox.Update(msg)
		cmds = append(cmds, cmd)
		if a.active == screenReader {
			a.reader, cmd = a.reader.Update(msg)
			cmds = append(cmds, cmd)
		}
		if a.active == screenCompose {
			a.composer, cmd = a.composer.Update(msg)
			cmds = append(cmds, cmd)
		}
		return a, tea.Batch(cmds...)

	// --- Cross-screen transitions ---

	case common.FetchMessageMsg:
		id := msg.ID
		a.inbox.MarkRead(id) // optimistic local update
		// Check cache first
		if cached, ok := a.msgCache[id]; ok {
			a.inbox.SetStatus("")
			a.reader = reader.New(a.ctx, a.client, cached, a.width, a.height, a.inbox.TabIdx())
			a.active = screenReader
			return a, tea.Batch(a.reader.Init(), a.markAsRead(id))
		}
		a.inbox.SetLoadingStatus("Opening message...")
		// Show loading state, fetch in background
		a.loading = true
		return a, tea.Batch(
			func() tea.Msg {
				full, err := a.client.GetMessage(a.ctx, id)
				return fetchMsgResultMsg{msg: full, err: err}
			},
			a.inbox.SpinnerTick(),
		)

	case common.FetchAndReplyMsg:
		id := msg.ID
		// Check cache first
		if cached, ok := a.msgCache[id]; ok {
			tmpl := markdown.ReplyTemplate(cached.From, "Re: "+cached.Subject, cached.Body)
			a.composer = composer.New(a.ctx, a.client, a.cfg.Editor(), tmpl, a.width, a.height)
			a.active = screenCompose
			return a, a.composer.Init()
		}
		a.loading = true
		return a, func() tea.Msg {
			full, err := a.client.GetMessage(a.ctx, id)
			return fetchReplyResultMsg{msg: full, err: err}
		}

	case fetchReplyResultMsg:
		a.loading = false
		if msg.err != nil {
			a.status = fmt.Sprintf("Error fetching message: %v", msg.err)
			return a, nil
		}
		a.msgCache[msg.msg.ID] = msg.msg
		tmpl := markdown.ReplyTemplate(msg.msg.From, "Re: "+msg.msg.Subject, msg.msg.Body)
		a.composer = composer.New(a.ctx, a.client, a.cfg.Editor(), tmpl, a.width, a.height)
		a.active = screenCompose
		return a, a.composer.Init()

	case fetchMsgResultMsg:
		a.loading = false
		a.inbox.SetStatus("")
		if msg.err != nil {
			a.inbox.SetStatus(fmt.Sprintf("Error: %v", msg.err))
			return a, nil
		}
		a.msgCache[msg.msg.ID] = msg.msg
		a.inbox.MarkRead(msg.msg.ID)
		a.reader = reader.New(a.ctx, a.client, msg.msg, a.width, a.height, a.inbox.TabIdx())
		a.active = screenReader
		return a, tea.Batch(a.reader.Init(), a.markAsRead(msg.msg.ID))

	case common.OpenMessageMsg:
		a.reader = reader.New(a.ctx, a.client, msg.Message, a.width, a.height, a.inbox.TabIdx())
		a.active = screenReader
		return a, a.reader.Init()

	case common.ComposeMsg:
		a.composer = composer.New(a.ctx, a.client, a.cfg.Editor(), msg.Template, a.width, a.height)
		a.active = screenCompose
		return a, a.composer.Init()

	case common.TrashFromReaderMsg:
		a.active = screenInbox
		id := msg.ID
		// Optimistic removal from inbox cache
		a.inbox.OptimisticRemove(id)
		delete(a.msgCache, id)
		// Trash or delete depending on folder
		label := a.inbox.CurrentLabelID()
		if label == "TRASH" {
			a.inbox.SetStatus("Deleting message...")
			return a, func() tea.Msg {
				err := a.client.DeleteMessage(a.ctx, id)
				if err != nil {
					return common.StatusMsg{Text: "Error: " + err.Error()}
				}
				return common.StatusMsg{Text: "Message permanently deleted."}
			}
		}
		a.inbox.SetStatus("Trashing message...")
		return a, func() tea.Msg {
			err := a.client.TrashMessage(a.ctx, id)
			if err != nil {
				return common.StatusMsg{Text: "Error: " + err.Error()}
			}
			return common.StatusMsg{Text: "Message trashed."}
		}

	case common.BackToInboxMsg:
		a.active = screenInbox
		a.status = ""
		a.inbox.SetStatus("")
		return a, nil

	case common.SendResultMsg:
		a.active = screenInbox
		if msg.Err != nil {
			a.status = fmt.Sprintf("Send failed: %v", msg.Err)
		} else {
			a.status = "Message sent!"
		}
		return a, tea.Batch(
			a.inbox.Init(),
			func() tea.Msg { return common.StatusMsg{Text: a.status} },
		)

	case common.StatusMsg:
		a.status = msg.Text
		var cmd tea.Cmd
		a.inbox, cmd = a.inbox.Update(msg)
		return a, cmd
	}

	// Delegate to active screen.
	var cmd tea.Cmd
	switch a.active {
	case screenInbox:
		a.inbox, cmd = a.inbox.Update(msg)
	case screenReader:
		a.reader, cmd = a.reader.Update(msg)
	case screenCompose:
		a.composer, cmd = a.composer.Update(msg)
	}
	return a, cmd
}

// markAsRead removes the UNREAD label from a message in the background.
func (a App) markAsRead(id string) tea.Cmd {
	return func() tea.Msg {
		a.client.MoveMessage(a.ctx, id, nil, []string{"UNREAD"})
		return nil // fire and forget
	}
}

// View delegates to the active screen.
func (a App) View() string {
	if a.loading {
		return a.inbox.View()
	}
	switch a.active {
	case screenReader:
		return a.reader.View()
	case screenCompose:
		return a.composer.View()
	default:
		return a.inbox.View()
	}
}
