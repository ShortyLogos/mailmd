package inbox

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/deric/mailmd/internal/gmail"
	"github.com/deric/mailmd/internal/markdown"
	"github.com/deric/mailmd/internal/ui/common"
)

// folder represents a Gmail label/folder tab.
type folder struct {
	name    string
	labelID string
}

var folders = []folder{
	{name: "Inbox", labelID: "INBOX"},
	{name: "Drafts", labelID: "DRAFT"},
	{name: "Sent", labelID: "SENT"},
	{name: "Trash", labelID: "TRASH"},
}

// messagesLoadedMsg carries the result of fetching messages.
type messagesLoadedMsg struct {
	messages []gmail.MessageSummary
	err      error
}

// trashDoneMsg signals a trash operation completed.
type trashDoneMsg struct{ err error }

// Model is the inbox Bubble Tea model.
type Model struct {
	ctx         context.Context
	client      gmail.Client
	width       int
	height      int
	tabIdx      int
	cursor      int
	messages    []gmail.MessageSummary
	loading     bool
	err         string
	status      string
	showPreview bool
}

// New creates a new inbox model.
func New(ctx context.Context, client gmail.Client) Model {
	return Model{
		ctx:     ctx,
		client:  client,
		loading: true,
	}
}

// Init loads messages for the default folder.
func (m Model) Init() tea.Cmd {
	return m.fetchMessages()
}

func (m Model) fetchMessages() tea.Cmd {
	labelID := folders[m.tabIdx].labelID
	return func() tea.Msg {
		list, err := m.client.ListMessages(m.ctx, labelID, "")
		if err != nil {
			return messagesLoadedMsg{err: err}
		}
		return messagesLoadedMsg{messages: list.Messages}
	}
}

func (m Model) trashMessage(id string) tea.Cmd {
	return func() tea.Msg {
		err := m.client.TrashMessage(m.ctx, id)
		return trashDoneMsg{err: err}
	}
}

// Update handles key presses and messages.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case messagesLoadedMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err.Error()
		} else {
			m.messages = msg.messages
			m.cursor = 0
			m.err = ""
		}

	case trashDoneMsg:
		if msg.err != nil {
			m.status = "Error trashing message: " + msg.err.Error()
		} else {
			m.status = "Message moved to Trash."
			m.loading = true
			return m, m.fetchMessages()
		}

	case common.StatusMsg:
		m.status = msg.Text

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, common.Keys.Quit):
			return m, tea.Quit

		case key.Matches(msg, common.Keys.Up):
			if m.cursor > 0 {
				m.cursor--
			}

		case key.Matches(msg, common.Keys.Down):
			if m.cursor < len(m.messages)-1 {
				m.cursor++
			}

		case key.Matches(msg, common.Keys.NextTab):
			m.tabIdx = (m.tabIdx + 1) % len(folders)
			m.loading = true
			m.messages = nil
			m.cursor = 0
			return m, m.fetchMessages()

		case key.Matches(msg, common.Keys.PrevTab):
			m.tabIdx = (m.tabIdx - 1 + len(folders)) % len(folders)
			m.loading = true
			m.messages = nil
			m.cursor = 0
			return m, m.fetchMessages()

		case key.Matches(msg, common.Keys.Open):
			if len(m.messages) > 0 {
				id := m.messages[m.cursor].ID
				return m, func() tea.Msg { return common.FetchMessageMsg{ID: id} }
			}

		case key.Matches(msg, common.Keys.Compose):
			tmpl := markdown.ComposeTemplate()
			return m, func() tea.Msg { return common.ComposeMsg{Template: tmpl} }

		case key.Matches(msg, common.Keys.Preview):
			m.showPreview = !m.showPreview

		case key.Matches(msg, common.Keys.Trash):
			if len(m.messages) > 0 {
				id := m.messages[m.cursor].ID
				return m, m.trashMessage(id)
			}
		}
	}
	return m, nil
}

// View renders the inbox.
func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	var b strings.Builder

	// Tab bar
	tabs := make([]string, len(folders))
	for i, f := range folders {
		if i == m.tabIdx {
			tabs[i] = common.ActiveTab.Render(f.name)
		} else {
			tabs[i] = common.InactiveTab.Render(f.name)
		}
	}
	tabRow := common.TabBar.Width(m.width).Render(strings.Join(tabs, ""))
	b.WriteString(tabRow + "\n")

	// Calculate content area height (subtract tabbar, statusbar)
	contentHeight := m.height - 3

	if m.loading {
		b.WriteString("\n  Loading messages...\n")
		return b.String()
	}
	if m.err != "" {
		b.WriteString("\n" + common.ErrorStyle.Render("Error: "+m.err) + "\n")
		return b.String()
	}
	if len(m.messages) == 0 {
		b.WriteString("\n  No messages.\n")
		return b.String()
	}

	// Layout: full list or split pane depending on preview toggle
	listWidth := m.width
	if m.showPreview {
		listWidth = m.width * 6 / 10
	}
	previewWidth := m.width - listWidth - 1

	// Build message list
	var listLines []string
	start := 0
	if m.cursor >= contentHeight {
		start = m.cursor - contentHeight + 1
	}
	end := start + contentHeight
	if end > len(m.messages) {
		end = len(m.messages)
	}

	for i := start; i < end; i++ {
		msg := m.messages[i]
		line := formatMessageLine(msg, listWidth-4)
		if i == m.cursor {
			line = common.SelectedMessage.Width(listWidth - 2).Render(line)
		} else if msg.Unread {
			line = common.UnreadMessage.Width(listWidth - 2).Render(line)
		} else {
			line = common.ReadMessage.Width(listWidth - 2).Render(line)
		}
		listLines = append(listLines, line)
	}
	// Pad to fill content area
	for len(listLines) < contentHeight {
		listLines = append(listLines, strings.Repeat(" ", listWidth))
	}

	if m.showPreview {
		// Build preview pane
		var previewLines []string
		if m.cursor < len(m.messages) {
			cur := m.messages[m.cursor]
			previewLines = buildPreview(cur, previewWidth, contentHeight)
		}
		for len(previewLines) < contentHeight {
			previewLines = append(previewLines, "")
		}

		// Combine side by side
		divider := lipgloss.NewStyle().Foreground(common.Secondary)
		for i := 0; i < contentHeight; i++ {
			left := ""
			right := ""
			if i < len(listLines) {
				left = listLines[i]
			}
			if i < len(previewLines) {
				right = previewLines[i]
			}
			b.WriteString(left + divider.Render("│") + right + "\n")
		}
	} else {
		for i := 0; i < contentHeight; i++ {
			if i < len(listLines) {
				b.WriteString(listLines[i] + "\n")
			} else {
				b.WriteString("\n")
			}
		}
	}

	// Status bar
	statusText := m.status
	if statusText == "" {
		statusText = fmt.Sprintf(" %d messages  [%s]  j/k=nav  o=open  c=compose  d=trash  p=preview  tab=folder  q=quit",
			len(m.messages), folders[m.tabIdx].name)
	}
	b.WriteString(common.StatusBar.Width(m.width).Render(statusText))

	return b.String()
}

func formatMessageLine(msg gmail.MessageSummary, width int) string {
	from := msg.From
	if len(from) > 20 {
		from = from[:18] + ".."
	}
	from = fmt.Sprintf("%-20s", from)

	dateStr := ""
	if !msg.Date.IsZero() {
		now := time.Now()
		if msg.Date.Year() == now.Year() && msg.Date.YearDay() == now.YearDay() {
			dateStr = msg.Date.Format("15:04")
		} else {
			dateStr = msg.Date.Format("Jan 02")
		}
	}
	dateStr = fmt.Sprintf("%6s", dateStr)

	subjectWidth := width - 20 - 6 - 2
	if subjectWidth < 0 {
		subjectWidth = 0
	}
	subject := msg.Subject
	if len(subject) > subjectWidth {
		subject = subject[:subjectWidth]
	}
	subject = fmt.Sprintf("%-*s", subjectWidth, subject)

	unreadMark := " "
	if msg.Unread {
		unreadMark = "●"
	}

	return fmt.Sprintf("%s %s %s %s", unreadMark, from, subject, dateStr)
}

func buildPreview(msg gmail.MessageSummary, width, height int) []string {
	var lines []string

	header := fmt.Sprintf("From: %s", msg.From)
	lines = append(lines, truncate(header, width))
	subject := fmt.Sprintf("Subj: %s", msg.Subject)
	lines = append(lines, truncate(subject, width))
	lines = append(lines, strings.Repeat("─", width))

	// Wrap snippet across lines
	snippet := msg.Snippet
	for len(snippet) > 0 {
		if len(lines) >= height {
			break
		}
		end := width
		if end > len(snippet) {
			end = len(snippet)
		}
		lines = append(lines, snippet[:end])
		snippet = snippet[end:]
	}

	return lines
}

func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if len(s) > width {
		return s[:width-1] + "…"
	}
	return s
}
