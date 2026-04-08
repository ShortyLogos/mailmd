package reader

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/deric/mailmd/internal/gmail"
	"github.com/deric/mailmd/internal/markdown"
	"github.com/deric/mailmd/internal/ui/common"
)

// Model is the reader Bubble Tea model.
type Model struct {
	message  *gmail.Message
	viewport viewport.Model
	width    int
	height   int
	ready    bool
}

// New creates a new reader model for the given message.
func New(msg *gmail.Message, width, height int) Model {
	m := Model{
		message: msg,
		width:   width,
		height:  height,
	}
	m.initViewport()
	return m
}

func (m *Model) initViewport() {
	headerHeight := 5 // From, To, Subject, Date, separator
	statusHeight := 1
	vpHeight := m.height - headerHeight - statusHeight - 1
	if vpHeight < 1 {
		vpHeight = 1
	}

	m.viewport = viewport.New(m.width, vpHeight)
	m.viewport.SetContent(m.renderBody())
	m.ready = true
}

func (m Model) renderBody() string {
	if m.message == nil {
		return ""
	}

	body := m.message.Body
	if body == "" {
		body = "(No message body)"
	}

	rendered, err := glamour.Render(body, "dark")
	if err != nil {
		// Fall back to plain text with basic markdown convert
		return markdown.ConvertPlain(body)
	}
	return rendered
}

// Init is a no-op for reader.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update handles key events for the reader.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.initViewport()

	case tea.MouseMsg:
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, common.Keys.Back):
			return m, func() tea.Msg { return common.BackToInboxMsg{} }

		case key.Matches(msg, common.Keys.Reply):
			if m.message != nil {
				tmpl := markdown.ReplyTemplate(m.message.From, "Re: "+m.message.Subject, m.message.Body)
				return m, func() tea.Msg { return common.ComposeMsg{Template: tmpl} }
			}

		case key.Matches(msg, common.Keys.Forward):
			if m.message != nil {
				tmpl := markdown.ForwardTemplate(m.message.Subject, m.message.Body, m.message.From)
				return m, func() tea.Msg { return common.ComposeMsg{Template: tmpl} }
			}

		case key.Matches(msg, common.Keys.Quit):
			return m, tea.Quit

		default:
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

// View renders the reader.
func (m Model) View() string {
	if m.message == nil || !m.ready {
		return "Loading message..."
	}

	var b strings.Builder

	// Header block
	b.WriteString(common.ReaderHeader.Render(fmt.Sprintf("From:    %s", m.message.From)) + "\n")
	b.WriteString(common.ReaderHeader.Render(fmt.Sprintf("To:      %s", m.message.To)) + "\n")
	b.WriteString(common.ReaderHeader.Render(fmt.Sprintf("Subject: %s", m.message.Subject)) + "\n")
	dateStr := ""
	if !m.message.Date.IsZero() {
		dateStr = m.message.Date.Format("Mon, 02 Jan 2006 15:04:05 MST")
	}
	b.WriteString(common.ReaderHeader.Render(fmt.Sprintf("Date:    %s", dateStr)) + "\n")
	b.WriteString(strings.Repeat("─", m.width) + "\n")

	// Scrollable body
	b.WriteString(m.viewport.View() + "\n")

	// Status bar
	status := fmt.Sprintf(" esc=back  r=reply  f=forward  j/k=scroll  q=quit  [%d%%]",
		int(m.viewport.ScrollPercent()*100))
	b.WriteString(common.StatusBar.Width(m.width).Render(status))

	return b.String()
}
