package inbox

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
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
	tabIdx   int    // which folder this response belongs to
	query    string // search query this response belongs to
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

	// Search
	searching   bool             // true when search input is visible
	searchInput textinput.Model
	searchQuery string           // active search query (empty = no filter)
	searchCache *folderCache     // separate cache for search results
}

// New creates a new inbox model.
func New(ctx context.Context, client gmail.Client) Model {
	ti := textinput.New()
	ti.Placeholder = "Search Gmail (from:, subject:, has:attachment, ...)"
	ti.CharLimit = 256

	return Model{
		ctx:         ctx,
		client:      client,
		cache:       make(map[int]*folderCache),
		searchInput: ti,
	}
}

// fc returns the active message cache — search results if searching, otherwise folder cache.
func (m *Model) fc() *folderCache {
	if m.searchQuery != "" && m.searchCache != nil {
		return m.searchCache
	}
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

func (m Model) fetchSearch(query string) tea.Cmd {
	return func() tea.Msg {
		list, err := m.client.ListMessages(m.ctx, "INBOX", query, "")
		if err != nil {
			return messagesLoadedMsg{err: err, tabIdx: -1, query: query}
		}
		return messagesLoadedMsg{messages: list.Messages, tabIdx: -1, query: query}
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
		if msg.tabIdx == -1 {
			// Search result
			if m.searchCache == nil {
				m.searchCache = &folderCache{}
			}
			m.searchCache.lastSync = time.Now()
			if msg.err != nil {
				m.err = msg.err.Error()
			} else {
				m.searchCache.messages = msg.messages
				m.searchCache.cursor = 0
				m.err = ""
			}
			m.syncing = false
			return m, nil
		}

		// Folder result — update cache for that folder
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
		} else {
			m.status = "Message trashed."
		}
		m.syncing = true
		return m, m.fetchMessages()

	case pollTickMsg:
		if m.searchQuery == "" {
			m.syncing = true
			return m, tea.Batch(m.fetchMessages(), m.pollTick())
		}
		return m, m.pollTick() // don't poll while searching

	case common.StatusMsg:
		m.status = msg.Text

	case tea.MouseMsg:
		if m.searching {
			return m, nil
		}
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
				headerRows := 4 // tabs(2) + sync(1) + padding(1)
				if m.searching || m.searchQuery != "" {
					headerRows = 5 // + search bar
				}
				row := msg.Y - headerRows
				contentHeight := m.contentHeight()
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
		// Search input mode — capture all keys
		if m.searching {
			switch {
			case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
				query := strings.TrimSpace(m.searchInput.Value())
				m.searching = false
				if query == "" {
					m.searchQuery = ""
					m.searchCache = nil
					return m, nil
				}
				m.searchQuery = query
				m.searchCache = &folderCache{}
				m.syncing = true
				return m, m.fetchSearch(query)

			case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
				m.searching = false
				m.searchInput.SetValue(m.searchQuery) // restore previous query
				return m, nil
			}

			var cmd tea.Cmd
			m.searchInput, cmd = m.searchInput.Update(msg)
			return m, cmd
		}

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
			m.searchQuery = ""
			m.searchCache = nil
			m.searching = false
			m.syncing = true
			m.err = ""
			return m, m.fetchMessages()

		case key.Matches(msg, common.Keys.PrevTab):
			m.tabIdx = (m.tabIdx - 1 + len(folders)) % len(folders)
			m.searchQuery = ""
			m.searchCache = nil
			m.searching = false
			m.syncing = true
			m.err = ""
			return m, m.fetchMessages()

		case key.Matches(msg, key.NewBinding(key.WithKeys("/"))):
			m.searching = true
			m.searchInput.SetValue("")
			m.searchInput.Focus()
			return m, textinput.Blink

		case key.Matches(msg, common.Keys.Back):
			// Esc clears search if active
			if m.searchQuery != "" {
				m.searchQuery = ""
				m.searchCache = nil
				m.searchInput.SetValue("")
				return m, nil
			}

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
			if m.searchQuery != "" {
				return m, m.fetchSearch(m.searchQuery)
			}
			return m, m.fetchMessages()

		case key.Matches(msg, common.Keys.Preview):
			m.showPreview = !m.showPreview

		case key.Matches(msg, common.Keys.Trash):
			if len(fc.messages) > 0 && fc.cursor < len(fc.messages) {
				trashed := fc.messages[fc.cursor]
				m.status = fmt.Sprintf("Trashing \"%s\"...", truncate(trashed.Subject, 40))
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

func (m Model) contentHeight() int {
	h := m.height - 6 // tabs(2) + sync(1) + padding(1) + keybinds(2)
	if m.searching || m.searchQuery != "" {
		h-- // search bar takes 1 line
	}
	if h < 1 {
		h = 1
	}
	return h
}

// View renders the inbox.
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

	// 2. Sync status line — left: sync info + count, right: status aligned to date column
	leftParts := ""
	if m.syncing {
		leftParts = common.SyncingStyle.Render(" Syncing...")
	} else if m.err != "" {
		leftParts = common.ErrorStyle.Render(" Error: " + m.err)
	} else if !fc.lastSync.IsZero() {
		ago := time.Since(fc.lastSync).Truncate(time.Second)
		if ago < 5*time.Second {
			leftParts = common.SyncedStyle.Render(" Synced")
		} else if ago < time.Minute {
			leftParts = common.SyncedStyle.Render(fmt.Sprintf(" Synced %ds ago", int(ago.Seconds())))
		} else {
			leftParts = common.MutedStyle.Render(fmt.Sprintf(" Synced %dm ago", int(ago.Minutes())))
		}
	}
	if len(fc.messages) > 0 {
		leftParts += common.MutedStyle.Render(fmt.Sprintf("  %d messages", len(fc.messages)))
	}
	if m.searchQuery != "" {
		leftParts += common.SyncingStyle.Render(fmt.Sprintf("  Search: \"%s\"", m.searchQuery))
	}

	rightParts := ""
	if m.status != "" {
		rightParts = common.MutedStyle.Render(m.status)
	}

	// Right-align status to the date column position (last ~8 chars of list width)
	listWidth := m.width
	if m.showPreview {
		listWidth = m.width * 6 / 10
	}
	rightPos := listWidth - 2 // align with date column end
	leftW := rw.StringWidth(lipgloss.NewStyle().Render(leftParts))
	rightW := rw.StringWidth(lipgloss.NewStyle().Render(rightParts))
	gap := rightPos - leftW - rightW
	if gap < 1 {
		gap = 1
	}
	b.WriteString(leftParts + strings.Repeat(" ", gap) + rightParts + "\n")

	// Padding below status line
	b.WriteString("\n")

	// 3. Search bar (if active or has query)
	if m.searching {
		searchBar := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#374151")).
			Width(m.width).
			Padding(0, 1).
			Render("/ " + m.searchInput.View())
		b.WriteString(searchBar + "\n")
	} else if m.searchQuery != "" {
		searchBar := lipgloss.NewStyle().
			Foreground(common.Muted).
			Width(m.width).
			Padding(0, 1).
			Render(fmt.Sprintf("/ %s  (esc to clear)", m.searchQuery))
		b.WriteString(searchBar + "\n")
	}

	// 4. Keybinds bar (appended at the bottom)
	keybindText := " j/k=nav  o=open  c=compose  d=trash  p=preview  /=search  R=refresh  tab=folder  q=quit"
	keybinds := common.StatusBar.Width(m.width).Render(keybindText)

	contentHeight := m.contentHeight()

	// 5. Message list
	if len(fc.messages) == 0 {
		if m.syncing {
			b.WriteString("\n  Loading messages...\n")
		} else if m.searchQuery != "" {
			b.WriteString("\n  No results.\n")
		} else {
			b.WriteString("\n  No messages.\n")
		}
		for i := 2; i < contentHeight; i++ {
			b.WriteString("\n")
		}
		b.WriteString(keybinds)
		return b.String()
	}

	previewWidth := 0
	if m.showPreview {
		listWidth = m.width * 6 / 10
		previewWidth = m.width - listWidth - 1
	}
	visibleRows := contentHeight

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
		line := formatMessageLine(msg, listWidth-2)
		// Pad line to exact width with spaces, then apply style (no lipgloss Width/Padding)
		line = runewidthPadRight(line, listWidth-2)
		if i == fc.cursor {
			line = common.SelectedMessage.Padding(0, 0).Width(0).Render(" " + line + " ")
		} else if msg.Unread {
			line = common.UnreadMessage.Padding(0, 0).Width(0).Render(" " + line + " ")
		} else {
			line = common.ReadMessage.Padding(0, 0).Width(0).Render(" " + line + " ")
		}
		listLines = append(listLines, line)
	}
	for len(listLines) < contentHeight {
		listLines = append(listLines, "")
	}

	if m.showPreview {
		var previewLines []string
		if fc.cursor < len(fc.messages) {
			cur := fc.messages[fc.cursor]
			previewLines = buildPreview(cur, previewWidth, contentHeight)
		}
		for len(previewLines) < contentHeight {
			previewLines = append(previewLines, "")
		}
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

	// 6. Keybinds at bottom
	b.WriteString(keybinds)

	return b.String()
}

func formatMessageLine(msg gmail.MessageSummary, width int) string {
	if width < 10 {
		return ""
	}

	dateW := 6
	fromW := width / 4
	if fromW > 24 {
		fromW = 24
	}
	if fromW < 12 {
		fromW = 12
	}

	unread := " "
	if msg.Unread {
		unread = "●"
	}

	from := msg.From
	if idx := strings.Index(from, "<"); idx > 1 {
		from = strings.TrimSpace(from[:idx])
	}
	from = runewidthTruncate(from, fromW)
	from = runewidthPadRight(from, fromW)

	dateStr := ""
	if !msg.Date.IsZero() {
		now := time.Now()
		if msg.Date.Year() == now.Year() && msg.Date.YearDay() == now.YearDay() {
			dateStr = msg.Date.Format("15:04")
		} else if msg.Date.Year() == now.Year() {
			dateStr = msg.Date.Format("Jan 02")
		} else {
			dateStr = msg.Date.Format("Jan 06")
		}
	}
	dateStr = fmt.Sprintf("%*s", dateW, dateStr)

	subjectW := width - 2 - fromW - 2 - 3 - dateW
	if subjectW < 0 {
		subjectW = 0
	}
	subject := msg.Subject
	if subject == "" {
		subject = "(no subject)"
	}
	subject = runewidthTruncate(subject, subjectW)
	subject = runewidthPadRight(subject, subjectW)

	return fmt.Sprintf("%s %s  %s   %s", unread, from, subject, dateStr)
}

func buildPreview(msg gmail.MessageSummary, width, height int) []string {
	var lines []string
	header := fmt.Sprintf("From: %s", msg.From)
	lines = append(lines, truncate(header, width))
	subject := fmt.Sprintf("Subj: %s", msg.Subject)
	lines = append(lines, truncate(subject, width))
	lines = append(lines, strings.Repeat("─", width))
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

func runewidthTruncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return rw.Truncate(s, width, "…")
}

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
