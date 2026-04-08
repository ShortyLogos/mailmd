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
	rw "github.com/mattn/go-runewidth"
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

// pollTickMsg triggers a background refresh.
type pollTickMsg struct{}

// Model is the inbox Bubble Tea model.
type Model struct {
	ctx         context.Context
	client      gmail.Client
	width       int
	height      int
	tabIdx      int
	cursor      int
	messages    []gmail.MessageSummary
	loading     bool // true only on first load (no cached data yet)
	syncing     bool // true when fetching in background (cached data visible)
	lastSync    time.Time
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

// Init loads messages for the default folder and starts polling.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.fetchMessages(), m.pollTick())
}

func (m Model) pollTick() tea.Cmd {
	return tea.Tick(2*time.Minute, func(time.Time) tea.Msg {
		return pollTickMsg{}
	})
}

func (m Model) fetchMessages() tea.Cmd {
	labelID := folders[m.tabIdx].labelID
	query := ""
	if labelID == "INBOX" {
		query = "category:primary"
	}
	return func() tea.Msg {
		list, err := m.client.ListMessages(m.ctx, labelID, query, "")
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
		m.syncing = false
		m.lastSync = time.Now()
		if msg.err != nil {
			m.err = msg.err.Error()
		} else {
			// Preserve cursor position on background refresh
			prevID := ""
			if m.cursor < len(m.messages) {
				prevID = m.messages[m.cursor].ID
			}
			m.messages = msg.messages
			m.err = ""
			// Try to restore cursor to the same message
			if prevID != "" {
				for i, msg := range m.messages {
					if msg.ID == prevID {
						m.cursor = i
						break
					}
				}
			}
			if m.cursor >= len(m.messages) {
				m.cursor = 0
			}
		}

	case trashDoneMsg:
		if msg.err != nil {
			m.status = "Error trashing message: " + msg.err.Error()
		} else {
			m.status = "Message moved to Trash."
			m.loading = true
			return m, m.fetchMessages()
		}

	case pollTickMsg:
		m.syncing = true
		return m, tea.Batch(m.fetchMessages(), m.pollTick())

	case common.StatusMsg:
		m.status = msg.Text

	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			if m.cursor > 0 {
				m.cursor--
			}
		case tea.MouseButtonWheelDown:
			if m.cursor < len(m.messages)-1 {
				m.cursor++
			}
		case tea.MouseButtonLeft:
			if msg.Action == tea.MouseActionRelease {
				// Calculate which message was clicked (account for tab bar)
				row := msg.Y - 2 // subtract tab bar height
				start := 0
				contentHeight := m.height - 3
				if m.cursor >= contentHeight {
					start = m.cursor - contentHeight + 1
				}
				idx := start + row
				if idx >= 0 && idx < len(m.messages) {
					m.cursor = idx
				}
			}
		}

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

		case key.Matches(msg, common.Keys.Refresh):
			m.syncing = true
			return m, m.fetchMessages()

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

	// Tab bar with sync indicator
	tabs := make([]string, len(folders))
	for i, f := range folders {
		if i == m.tabIdx {
			tabs[i] = common.ActiveTab.Render(f.name)
		} else {
			tabs[i] = common.InactiveTab.Render(f.name)
		}
	}

	syncIndicator := ""
	if m.syncing {
		syncIndicator = common.SyncingStyle.Render("  Syncing...")
	} else if !m.lastSync.IsZero() {
		ago := time.Since(m.lastSync).Truncate(time.Second)
		if ago < 5*time.Second {
			syncIndicator = common.SyncedStyle.Render("  Synced")
		} else if ago < time.Minute {
			syncIndicator = common.SyncedStyle.Render(fmt.Sprintf("  Synced %ds ago", int(ago.Seconds())))
		} else {
			syncIndicator = common.MutedStyle.Render(fmt.Sprintf("  Synced %dm ago", int(ago.Minutes())))
		}
	}

	tabRow := common.TabBar.Width(m.width).Render(strings.Join(tabs, "") + syncIndicator)
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
		statusText = fmt.Sprintf(" %d messages  [%s]  j/k=nav  o=open  c=compose  d=trash  p=preview  R=refresh  tab=folder  q=quit",
			len(m.messages), folders[m.tabIdx].name)
	}
	b.WriteString(common.StatusBar.Width(m.width).Render(statusText))

	return b.String()
}

func formatMessageLine(msg gmail.MessageSummary, width int) string {
	if width < 10 {
		return ""
	}

	// Column widths: unread(2) + from(fromW) + gap(2) + subject(flex) + gap(1) + date(dateW)
	dateW := 6
	fromW := width / 4
	if fromW > 24 {
		fromW = 24
	}
	if fromW < 12 {
		fromW = 12
	}

	// Unread indicator
	unread := " "
	if msg.Unread {
		unread = "●"
	}

	// From — extract display name if possible
	from := msg.From
	if idx := strings.Index(from, "<"); idx > 1 {
		from = strings.TrimSpace(from[:idx])
	}
	from = runewidthTruncate(from, fromW)
	from = runewidthPadRight(from, fromW)

	// Date — always right-aligned, fixed width (ASCII only, so fmt is fine)
	dateStr := ""
	if !msg.Date.IsZero() {
		now := time.Now()
		if msg.Date.Year() == now.Year() && msg.Date.YearDay() == now.YearDay() {
			dateStr = msg.Date.Format("15:04")
		} else if msg.Date.Year() == now.Year() {
			dateStr = msg.Date.Format("Jan 02")
		} else {
			dateStr = msg.Date.Format("01/2006")
		}
	}
	dateStr = fmt.Sprintf("%*s", dateW, dateStr)

	// Subject — fills remaining space
	subjectW := width - 2 - fromW - 2 - 1 - dateW
	if subjectW < 0 {
		subjectW = 0
	}
	subject := msg.Subject
	if subject == "" {
		subject = "(no subject)"
	}
	subject = runewidthTruncate(subject, subjectW)
	subject = runewidthPadRight(subject, subjectW)

	return fmt.Sprintf("%s %s  %s %s", unread, from, subject, dateStr)
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

// runewidthTruncate truncates a string to fit within the given display width,
// accounting for multi-byte characters and wide glyphs (CJK, emojis).
func runewidthTruncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return rw.Truncate(s, width, "…")
}

// runewidthPadRight pads a string with spaces to reach the given display width.
func runewidthPadRight(s string, width int) string {
	sw := rw.StringWidth(s)
	if sw >= width {
		return s
	}
	return s + strings.Repeat(" ", width-sw)
}

func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return rw.Truncate(s, width, "…")
}
