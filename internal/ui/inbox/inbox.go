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
	tabIdx   int // which folder this response belongs to
}

// trashDoneMsg signals a trash operation completed.
type trashDoneMsg struct{ err error }

// pollTickMsg triggers a background refresh.
type pollTickMsg struct{}

// folderCache stores per-folder state.
type folderCache struct {
	messages []gmail.MessageSummary
	cursor   int
	lastSync time.Time
}

// Model is the inbox Bubble Tea model.
type Model struct {
	ctx         context.Context
	client      gmail.Client
	width       int
	height      int
	tabIdx      int
	cache       map[int]*folderCache // per-folder cache keyed by tabIdx
	syncing     bool                 // true when fetching in background
	err         string
	status      string
	showPreview bool
}

// New creates a new inbox model.
func New(ctx context.Context, client gmail.Client) Model {
	return Model{
		ctx:    ctx,
		client: client,
		cache:  make(map[int]*folderCache),
	}
}

// fc returns the cache for the active folder, creating it if needed.
func (m *Model) fc() *folderCache {
	if m.cache[m.tabIdx] == nil {
		m.cache[m.tabIdx] = &folderCache{}
	}
	return m.cache[m.tabIdx]
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
	tabIdx := m.tabIdx
	labelID := folders[tabIdx].labelID
	query := ""
	if labelID == "INBOX" {
		query = "category:primary"
	}
	return func() tea.Msg {
		list, err := m.client.ListMessages(m.ctx, labelID, query, "")
		if err != nil {
			return messagesLoadedMsg{err: err, tabIdx: tabIdx}
		}
		return messagesLoadedMsg{messages: list.Messages, tabIdx: tabIdx}
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
		// Always update the cache for the folder this response belongs to
		if m.cache[msg.tabIdx] == nil {
			m.cache[msg.tabIdx] = &folderCache{}
		}
		target := m.cache[msg.tabIdx]
		target.lastSync = time.Now()

		if msg.err != nil {
			if msg.tabIdx == m.tabIdx {
				m.err = msg.err.Error()
				m.syncing = false
			}
		} else {
			// Preserve cursor position
			prevID := ""
			if target.cursor < len(target.messages) {
				prevID = target.messages[target.cursor].ID
			}
			target.messages = msg.messages
			if prevID != "" {
				for i, m := range target.messages {
					if m.ID == prevID {
						target.cursor = i
						break
					}
				}
			}
			if target.cursor >= len(target.messages) {
				target.cursor = 0
			}

			if msg.tabIdx == m.tabIdx {
				m.syncing = false
				m.err = ""
			}
		}

	case trashDoneMsg:
		if msg.err != nil {
			m.status = "Error trashing message: " + msg.err.Error()
			m.syncing = true
			return m, m.fetchMessages()
		} else {
			m.status = "Message trashed."
			m.syncing = true
			return m, m.fetchMessages()
		}

	case pollTickMsg:
		m.syncing = true
		return m, tea.Batch(m.fetchMessages(), m.pollTick())

	case common.StatusMsg:
		m.status = msg.Text

	case tea.MouseMsg:
		fc := m.fc()
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			if fc.cursor > 0 {
				fc.cursor--
			}
		case tea.MouseButtonWheelDown:
			if fc.cursor < len(fc.messages)-1 {
				fc.cursor++
			}
		case tea.MouseButtonLeft:
			if msg.Action == tea.MouseActionRelease {
				row := msg.Y - 3 // tabs(2) + sync(1) = 3 header rows
				contentHeight := m.height - 5
				start := 0
				if fc.cursor >= contentHeight {
					start = fc.cursor - contentHeight + 1
				}
				idx := start + row
				if idx >= 0 && idx < len(fc.messages) {
					fc.cursor = idx
				}
			}
		}

	case tea.KeyMsg:
		fc := m.fc()
		switch {
		case key.Matches(msg, common.Keys.Quit):
			return m, tea.Quit

		case key.Matches(msg, common.Keys.Up):
			if fc.cursor > 0 {
				fc.cursor--
			}

		case key.Matches(msg, common.Keys.Down):
			if fc.cursor < len(fc.messages)-1 {
				fc.cursor++
			}

		case key.Matches(msg, common.Keys.NextTab):
			m.tabIdx = (m.tabIdx + 1) % len(folders)
			m.syncing = true
			m.err = ""
			return m, m.fetchMessages()

		case key.Matches(msg, common.Keys.PrevTab):
			m.tabIdx = (m.tabIdx - 1 + len(folders)) % len(folders)
			m.syncing = true
			m.err = ""
			return m, m.fetchMessages()

		case key.Matches(msg, common.Keys.Open):
			if len(fc.messages) > 0 && fc.cursor < len(fc.messages) {
				id := fc.messages[fc.cursor].ID
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
			if len(fc.messages) > 0 && fc.cursor < len(fc.messages) {
				trashed := fc.messages[fc.cursor]
				m.status = fmt.Sprintf("Trashing \"%s\"...", truncate(trashed.Subject, 40))
				// Optimistically remove from list
				fc.messages = append(fc.messages[:fc.cursor], fc.messages[fc.cursor+1:]...)
				if fc.cursor >= len(fc.messages) && fc.cursor > 0 {
					fc.cursor--
				}
				return m, m.trashMessage(trashed.ID)
			}
		}
	}
	return m, nil
}

// View renders the inbox.
// Layout: tabs (top) → sync status → message list → keybinds (bottom)
func (m Model) View() string {
	if m.width == 0 {
		return " Initializing mailmd..."
	}

	fc := m.fc()
	var b strings.Builder

	// 1. Tab bar at top
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

	// 2. Sync status line
	syncLine := ""
	if m.syncing {
		syncLine = common.SyncingStyle.Render(" Syncing...")
	} else if m.err != "" {
		syncLine = common.ErrorStyle.Render(" Error: " + m.err)
	} else if !fc.lastSync.IsZero() {
		ago := time.Since(fc.lastSync).Truncate(time.Second)
		if ago < 5*time.Second {
			syncLine = common.SyncedStyle.Render(" Synced")
		} else if ago < time.Minute {
			syncLine = common.SyncedStyle.Render(fmt.Sprintf(" Synced %ds ago", int(ago.Seconds())))
		} else {
			syncLine = common.MutedStyle.Render(fmt.Sprintf(" Synced %dm ago", int(ago.Minutes())))
		}
	}
	if len(fc.messages) > 0 {
		syncLine += common.MutedStyle.Render(fmt.Sprintf("  %d messages", len(fc.messages)))
	}
	if m.status != "" {
		syncLine += "  " + common.MutedStyle.Render(m.status)
	}
	b.WriteString(syncLine + "\n")

	// 3. Keybinds bar (appended at the bottom)
	keybinds := common.StatusBar.Width(m.width).Render(
		" j/k=nav  o=open  c=compose  d=trash  p=preview  R=refresh  tab=folder  q=quit")

	// Content area height: total - tabs(2) - sync(1) - keybinds(2)
	contentHeight := m.height - 5
	if contentHeight < 1 {
		contentHeight = 1
	}

	// 4. Message list — always show cached messages, even while syncing
	if len(fc.messages) == 0 {
		emptyMsg := "  No messages."
		if m.syncing {
			emptyMsg = "  Loading messages..."
		}
		b.WriteString("\n" + emptyMsg + "\n")
		for i := 2; i < contentHeight; i++ {
			b.WriteString("\n")
		}
		b.WriteString(keybinds)
		return b.String()
	}

	// Layout: full list or split pane depending on preview toggle
	listWidth := m.width
	if m.showPreview {
		listWidth = m.width * 6 / 10
	}
	previewWidth := m.width - listWidth - 1
	visibleRows := contentHeight

	// Build message list
	var listLines []string
	start := 0
	if fc.cursor >= visibleRows {
		start = fc.cursor - visibleRows + 1
	}
	end := start + visibleRows
	if end > len(fc.messages) {
		end = len(fc.messages)
	}

	for i := start; i < end; i++ {
		msg := fc.messages[i]
		line := formatMessageLine(msg, listWidth-4)
		if i == fc.cursor {
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
		listLines = append(listLines, "")
	}

	if m.showPreview {
		// Build preview pane
		var previewLines []string
		if fc.cursor < len(fc.messages) {
			cur := fc.messages[fc.cursor]
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

	// 5. Keybinds at bottom
	b.WriteString(keybinds)

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
