package ui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/deric/mailmd/internal/config"
	"github.com/deric/mailmd/internal/gmail"
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

// fetchMsgResultMsg carries the result of fetching a full message.
type fetchMsgResultMsg struct {
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

	status string
}

// New creates and returns the root app model.
func New(ctx context.Context, client gmail.Client, cfg config.Config) App {
	return App{
		ctx:    ctx,
		client: client,
		cfg:    cfg,
		active: screenInbox,
		inbox:  inbox.New(ctx, client),
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
		// Inbox wants to open a message; fetch it fully first.
		id := msg.ID
		return a, func() tea.Msg {
			full, err := a.client.GetMessage(a.ctx, id)
			return fetchMsgResultMsg{msg: full, err: err}
		}

	case fetchMsgResultMsg:
		if msg.err != nil {
			a.status = fmt.Sprintf("Error fetching message: %v", msg.err)
			return a, nil
		}
		a.reader = reader.New(msg.msg, a.width, a.height)
		a.active = screenReader
		return a, a.reader.Init()

	case common.OpenMessageMsg:
		a.reader = reader.New(msg.Message, a.width, a.height)
		a.active = screenReader
		return a, a.reader.Init()

	case common.ComposeMsg:
		a.composer = composer.New(a.ctx, a.client, a.cfg.Editor(), msg.Template, a.width, a.height)
		a.active = screenCompose
		return a, a.composer.Init()

	case common.BackToInboxMsg:
		a.active = screenInbox
		a.status = ""
		return a, nil

	case common.SendResultMsg:
		a.active = screenInbox
		if msg.Err != nil {
			a.status = fmt.Sprintf("Send failed: %v", msg.Err)
		} else {
			a.status = "Message sent!"
		}
		return a, tea.Batch(
			a.inbox.Init(), // refresh inbox
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

// View delegates to the active screen.
func (a App) View() string {
	switch a.active {
	case screenReader:
		return a.reader.View()
	case screenCompose:
		return a.composer.View()
	default:
		return a.inbox.View()
	}
}
