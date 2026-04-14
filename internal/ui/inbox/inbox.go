package inbox

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/deric/mailmd/internal/gmail"
	"github.com/deric/mailmd/internal/ui/common"
	rw "github.com/mattn/go-runewidth"
)

// folder represents a Gmail label/folder tab.
type folder struct {
	name    string
	labelID string
}

var defaultFolders = []folder{
	{name: "Inbox", labelID: "INBOX"},
	{name: "Drafts", labelID: "DRAFT"},
	{name: "Sent", labelID: "SENT"},
	{name: "Trash", labelID: "TRASH"},
}

// systemLabels are Gmail built-in labels shown in the label picker's system section.
var systemLabels = []folder{
	{name: "Inbox", labelID: "INBOX"},
	{name: "Important", labelID: "IMPORTANT"},
	{name: "Starred", labelID: "STARRED"},
}

// messagesLoadedMsg carries the result of fetching messages.
type messagesLoadedMsg struct {
	messages      []gmail.MessageSummary
	nextPageToken string
	err           error
	tabIdx        int    // which folder this response belongs to
	query         string // search query this response belongs to
}

// moreMessagesMsg carries the result of loading the next page.
type moreMessagesMsg struct {
	messages      []gmail.MessageSummary
	nextPageToken string
	err           error
	tabIdx        int
	query         string // non-empty for search "load more"
}

// trashDoneMsg signals a trash operation completed.
type trashDoneMsg struct {
	err   error
	count int
}

// deleteDoneMsg signals a permanent delete completed.
type deleteDoneMsg struct{ err error }

// restoreDoneMsg signals a restore/untrash completed.
type restoreDoneMsg struct{ err error }

// toggleReadDoneMsg signals a mark read/unread toggle completed.
type toggleReadDoneMsg struct {
	err   error
	count int
}

// blockDoneMsg signals a block sender operation completed.
type blockDoneMsg struct{ err error }

// archiveDoneMsg signals an archive operation completed.
type archiveDoneMsg struct {
	err   error
	count int
}

// starDoneMsg signals a star/unstar toggle completed.
type starDoneMsg struct {
	err      error
	count    int
	starred  bool
}

// pollTickMsg triggers a background refresh.
type pollTickMsg struct{}

// labelsLoadedMsg carries the result of fetching custom Gmail labels.
type labelsLoadedMsg struct {
	labels []gmail.Label
	err    error
}

// labelCreatedMsg signals a new label was created via the API.
type labelCreatedMsg struct {
	label *gmail.Label
	err   error
	mode  int      // original picker mode to resume after creation
	ids   []string // message IDs to tag (if mode was 1 or 2)
}

// labelDeletedMsg signals a label was deleted.
type labelDeletedMsg struct {
	labelID string
	err     error
}

// labelRenamedMsg signals a label was renamed.
type labelRenamedMsg struct {
	labelID string
	newName string
	err     error
}

// attachmentEnrichMsg carries background attachment detection results.
type attachmentEnrichMsg struct {
	attachments map[string]bool // message ID → has real attachments
	tabIdx      int             // -1 for search results
}

// folderCache stores per-folder state.
type folderCache struct {
	messages      []gmail.MessageSummary
	cursor        int
	lastSync      time.Time
	selected      map[string]bool // message ID → selected
	nextPageToken string          // for loading more messages
	loadingMore   bool            // true while fetching next page
	highWaterMark int             // max messages ever loaded this session (for re-fetch sizing)
}

// Model is the inbox Bubble Tea model.
type Model struct {
	ctx         context.Context
	client      gmail.Client
	folders     []folder
	customLabels []folder   // user's Gmail labels (loaded async, shown in picker)
	width       int
	height      int
	tabIdx      int
	prevTabIdx  int                    // folder before entering a label folder (for ESC-back)
	cache       map[int]*folderCache // per-folder cache keyed by tabIdx
	syncing       bool                 // true when fetching in background
	err           string
	status        string
	statusLoading bool                 // true to show spinner next to status
	showPreview     bool
	attachmentCache map[string]bool // message ID → has attachments (persists across polls)
	AccountName   string               // current account display name (shown in tab bar)
	AccountEmail  string               // current account email (shown in tab bar)

	// New mail tracking
	lastInboxIDs map[string]bool  // message IDs from last inbox fetch
	skipNextPoll bool             // skip one poll cycle after optimistic updates

	// Label picker dialog
	showLabelPicker    bool
	labelPickerMode    int // 0 = browse folder, 1 = tag messages, 2 = move
	labelPickerTagIDs  []string // message IDs to tag (mode 1/2)
	labelPickerInput   textinput.Model
	labelPickerCursor    int
	labelPickerCursors   [2]int // saved cursor per section (0=user, 1=system)
	labelPickerFiltered  []int  // indices into active label list (custom or system)
	labelPickerSection   int    // 0 = user labels, 1 = system labels
	labelRenaming      bool   // true when editing a label name inline
	labelRenameInput   textinput.Model

	// Search
	searching   bool             // true when search input is visible
	searchInput textinput.Model
	searchQuery string           // active search query (empty = no filter)
	searchCache *folderCache     // separate cache for search results

	// Jump-to
	jumping   bool   // true when typing a line number
	jumpInput string // accumulated digits

	// Spinner
	spinner  spinner.Model
	dotFrame int // cycles 0-2 for animated ellipsis
}

// New creates a new inbox model.
func New(ctx context.Context, client gmail.Client) Model {
	ti := textinput.New()
	ti.Placeholder = "Search Gmail (from:, subject:, has:attachment, ...)"
	ti.CharLimit = 256

	lpi := textinput.New()
	lpi.Placeholder = "Filter labels..."
	lpi.CharLimit = 128

	lri := textinput.New()
	lri.Placeholder = "New label name..."
	lri.CharLimit = 128

	return Model{
		ctx:              ctx,
		client:           client,
		folders:          append([]folder{}, defaultFolders...),
		cache:            make(map[int]*folderCache),
		attachmentCache:  make(map[string]bool),
		searchInput:      ti,
		labelPickerInput: lpi,
		labelRenameInput: lri,
		syncing:          true, // first load
		spinner:          spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(common.SyncingStyle)),
	}
}

// IsInputActive reports whether the inbox is capturing keyboard input
// (search field or jump-to mode), so parent key handlers should not intercept.
func (m Model) IsInputActive() bool {
	return m.searching || m.jumping || m.showLabelPicker
}

// fc returns the active message cache — search results if searching, otherwise folder cache.
func (m *Model) fc() *folderCache {
	if m.searchQuery != "" && m.searchCache != nil {
		if m.searchCache.selected == nil {
			m.searchCache.selected = make(map[string]bool)
		}
		return m.searchCache
	}
	if m.cache[m.tabIdx] == nil {
		m.cache[m.tabIdx] = &folderCache{selected: make(map[string]bool)}
	}
	if m.cache[m.tabIdx].selected == nil {
		m.cache[m.tabIdx].selected = make(map[string]bool)
	}
	return m.cache[m.tabIdx]
}

// RecentAddresses returns deduplicated email addresses from all cached folder messages.
func (m Model) RecentAddresses() []string {
	seen := make(map[string]bool)
	var result []string

	addAddrs := func(raw string) {
		// Split comma-separated address lists into individual entries
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part != "" && !seen[part] {
				seen[part] = true
				result = append(result, part)
			}
		}
	}

	for _, fc := range m.cache {
		if fc == nil {
			continue
		}
		for _, msg := range fc.messages {
			addAddrs(msg.From)
			addAddrs(msg.To)
		}
	}
	if m.searchCache != nil {
		for _, msg := range m.searchCache.messages {
			addAddrs(msg.From)
			addAddrs(msg.To)
		}
	}
	return result
}

// selectedIDs returns the IDs of all selected messages, or the cursor message if none selected.
func (m *Model) selectedIDs(fc *folderCache) []string {
	var ids []string
	for id := range fc.selected {
		if fc.selected[id] {
			ids = append(ids, id)
		}
	}
	return ids
}

// selectedOrCursor returns IDs to act on: selected messages if any, otherwise the cursor message.
func (m *Model) selectedOrCursor(fc *folderCache) (ids []string, subjects []string) {
	for _, msg := range fc.messages {
		if fc.selected[msg.ID] {
			ids = append(ids, msg.ID)
			subjects = append(subjects, msg.Subject)
		}
	}
	if len(ids) == 0 && fc.cursor < len(fc.messages) {
		msg := fc.messages[fc.cursor]
		ids = []string{msg.ID}
		subjects = []string{msg.Subject}
	}
	return
}

// Init loads messages for the default folder and starts polling.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.fetchMessages(), m.pollTick(), m.spinner.Tick, m.fetchLabels())
}

func (m Model) fetchLabels() tea.Cmd {
	return func() tea.Msg {
		labels, err := m.client.ListLabels(m.ctx)
		return labelsLoadedMsg{labels: labels, err: err}
	}
}

func (m Model) pollTick() tea.Cmd {
	return tea.Tick(20*time.Second, func(time.Time) tea.Msg {
		return pollTickMsg{}
	})
}

func (m Model) fetchMessages() tea.Cmd {
	tabIdx := m.tabIdx
	labelID := m.folders[tabIdx].labelID
	query := ""
	// Use the high-water mark so re-fetches don't shrink a list the user has scrolled through
	var maxResults int64
	if c := m.cache[tabIdx]; c != nil && c.highWaterMark > 50 {
		maxResults = int64(c.highWaterMark)
	}
	return func() tea.Msg {
		list, err := m.client.ListMessages(m.ctx, labelID, query, "", maxResults)
		if err != nil {
			return messagesLoadedMsg{err: err, tabIdx: tabIdx}
		}
		return messagesLoadedMsg{messages: list.Messages, nextPageToken: list.NextPageToken, tabIdx: tabIdx}
	}
}

func (m Model) fetchSearch(query string) tea.Cmd {
	labelID := m.folders[m.tabIdx].labelID
	return func() tea.Msg {
		list, err := m.client.ListMessages(m.ctx, labelID, query, "")
		if err != nil {
			return messagesLoadedMsg{err: err, tabIdx: -1, query: query}
		}
		return messagesLoadedMsg{messages: list.Messages, nextPageToken: list.NextPageToken, tabIdx: -1, query: query}
	}
}


// maybePrefetch returns a command to load more messages if cursor is near the end.
func (m Model) maybePrefetch(fc *folderCache) tea.Cmd {
	if fc.loadingMore || fc.nextPageToken == "" {
		return nil
	}
	// Trigger when within 10 messages of the end
	if fc.cursor >= len(fc.messages)-10 {
		fc.loadingMore = true
		return m.fetchMoreMessages(fc.nextPageToken)
	}
	return nil
}

func (m Model) fetchMoreMessages(pageToken string) tea.Cmd {
	labelID := m.folders[m.tabIdx].labelID
	// If searching, pass the active query and tag as search (tabIdx -1)
	if m.searchQuery != "" {
		query := m.searchQuery
		return func() tea.Msg {
			list, err := m.client.ListMessages(m.ctx, labelID, query, pageToken)
			if err != nil {
				return moreMessagesMsg{err: err, tabIdx: -1, query: query}
			}
			return moreMessagesMsg{messages: list.Messages, nextPageToken: list.NextPageToken, tabIdx: -1, query: query}
		}
	}
	tabIdx := m.tabIdx
	return func() tea.Msg {
		list, err := m.client.ListMessages(m.ctx, labelID, "", pageToken)
		if err != nil {
			return moreMessagesMsg{err: err, tabIdx: tabIdx}
		}
		return moreMessagesMsg{messages: list.Messages, nextPageToken: list.NextPageToken, tabIdx: tabIdx}
	}
}

func (m *Model) enrichAttachments(msgs []gmail.MessageSummary, tabIdx int) tea.Cmd {
	// Filter out messages we've already checked — only fetch uncached ones
	var uncheckedIDs []string
	for _, msg := range msgs {
		if _, ok := m.attachmentCache[msg.ID]; !ok {
			uncheckedIDs = append(uncheckedIDs, msg.ID)
		}
	}
	if len(uncheckedIDs) == 0 {
		return nil // all cached, nothing to fetch
	}
	return func() tea.Msg {
		result, _ := m.client.CheckAttachments(m.ctx, uncheckedIDs)
		return attachmentEnrichMsg{attachments: result, tabIdx: tabIdx}
	}
}

func (m Model) deleteMessages(ids []string) tea.Cmd {
	return func() tea.Msg {
		err := m.client.DeleteMessages(m.ctx, ids)
		return deleteDoneMsg{err: err}
	}
}

func (m Model) restoreMessages(ids []string) tea.Cmd {
	return func() tea.Msg {
		errs := make(chan error, len(ids))
		for _, id := range ids {
			go func(id string) { errs <- m.client.UntrashMessage(m.ctx, id) }(id)
		}
		for range ids {
			if err := <-errs; err != nil {
				return restoreDoneMsg{err: err}
			}
		}
		return restoreDoneMsg{}
	}
}

func (m Model) trashMessages(ids []string) tea.Cmd {
	count := len(ids)
	return func() tea.Msg {
		err := m.client.TrashMessages(m.ctx, ids)
		return trashDoneMsg{err: err, count: count}
	}
}

func (m Model) toggleStarMessages(ids []string, star bool) tea.Cmd {
	count := len(ids)
	return func() tea.Msg {
		var add, remove []string
		if star {
			add = []string{"STARRED"}
		} else {
			remove = []string{"STARRED"}
		}
		err := m.client.ModifyMessages(m.ctx, ids, add, remove)
		return starDoneMsg{err: err, count: count, starred: star}
	}
}

func (m Model) archiveMessages(ids []string) tea.Cmd {
	count := len(ids)
	return func() tea.Msg {
		err := m.client.ModifyMessages(m.ctx, ids, nil, []string{"INBOX"})
		return archiveDoneMsg{err: err, count: count}
	}
}

func (m Model) toggleReadMessages(ids []string, markUnread bool) tea.Cmd {
	count := len(ids)
	return func() tea.Msg {
		var add, remove []string
		if markUnread {
			add = []string{"UNREAD"}
		} else {
			remove = []string{"UNREAD"}
		}
		err := m.client.ModifyMessages(m.ctx, ids, add, remove)
		return toggleReadDoneMsg{err: err, count: count}
	}
}

// optimisticRemove removes messages by ID from the folder cache and clears selection.
func (m *Model) optimisticRemove(fc *folderCache, ids []string) {
	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}
	var remaining []gmail.MessageSummary
	for _, msg := range fc.messages {
		if !idSet[msg.ID] {
			remaining = append(remaining, msg)
		}
	}
	fc.messages = remaining
	fc.selected = make(map[string]bool)
	if len(fc.messages) == 0 {
		fc.cursor = 0
	} else if fc.cursor >= len(fc.messages) {
		fc.cursor = len(fc.messages) - 1
	}
}

// currentLabelID returns the label ID of the active folder.
func (m Model) currentLabelID() string {
	return m.folders[m.tabIdx].labelID
}

// TabIdx returns the active folder tab index.
func (m Model) TabIdx() int {
	return m.tabIdx
}

// FolderNames returns the display names of all folders for use in the reader tab bar.
func (m Model) FolderNames() []string {
	names := make([]string, len(m.folders))
	for i, f := range m.folders {
		names[i] = f.name
	}
	return names
}

// SpinnerTick returns the spinner's tick command to keep it animating.
func (m Model) SpinnerTick() tea.Cmd {
	return m.spinner.Tick
}

// SetStatus sets the action text in the status line.
func (m *Model) SetStatus(text string) {
	m.status = text
	m.statusLoading = false
}

// SetLoadingStatus sets the action text with a spinner prefix.
func (m *Model) SetLoadingStatus(text string) {
	m.status = text
	m.statusLoading = true
}

// CurrentLabelID returns the active folder's label ID.
func (m Model) CurrentLabelID() string {
	return m.folders[m.tabIdx].labelID
}

// OptimisticRemove removes a message by ID from the active folder cache.
func (m *Model) OptimisticRemove(id string) {
	fc := m.fc()
	for i, msg := range fc.messages {
		if msg.ID == id {
			fc.messages = append(fc.messages[:i], fc.messages[i+1:]...)
			if len(fc.messages) == 0 {
				fc.cursor = 0
			} else if fc.cursor >= len(fc.messages) {
				fc.cursor = len(fc.messages) - 1
			}
			return
		}
	}
}

// MarkRead optimistically marks a message as read in the local cache.
func (m *Model) MarkRead(id string) {
	for tabIdx, fc := range m.cache {
		for i, msg := range fc.messages {
			if msg.ID == id {
				m.cache[tabIdx].messages[i].Unread = false
				return
			}
		}
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
				m.searchCache.nextPageToken = msg.nextPageToken
				m.searchCache.cursor = 0
				m.searchCache.loadingMore = false
				m.err = ""
				// Apply cached attachment status immediately
				for i := range m.searchCache.messages {
					if has, ok := m.attachmentCache[m.searchCache.messages[i].ID]; ok {
						m.searchCache.messages[i].HasAttachments = has
					}
				}
			}
			m.syncing = false
			if msg.err == nil && len(msg.messages) > 0 {
				return m, m.enrichAttachments(msg.messages, -1)
			}
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
			target.nextPageToken = msg.nextPageToken
			target.loadingMore = false
			// Apply cached attachment status immediately to prevent flicker
			for i := range target.messages {
				if has, ok := m.attachmentCache[target.messages[i].ID]; ok {
					target.messages[i].HasAttachments = has
				}
			}
			if len(target.messages) > target.highWaterMark {
				target.highWaterMark = len(target.messages)
			}
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
		// Fire background attachment enrichment + new mail notification
		if msg.err == nil && len(msg.messages) > 0 {
			cmds := []tea.Cmd{m.enrichAttachments(msg.messages, msg.tabIdx)}
			// New mail notification for inbox tab
			if m.folders[msg.tabIdx].labelID == "INBOX" && m.lastInboxIDs != nil {
				var newMsgs []gmail.MessageSummary
				for _, newMsg := range msg.messages {
					if newMsg.Unread && !m.lastInboxIDs[newMsg.ID] {
						newMsgs = append(newMsgs, newMsg)
					}
				}
				if len(newMsgs) > 0 {
					var statusText, notifyText string
					if len(newMsgs) == 1 {
						from := newMsgs[0].From
						if idx := strings.Index(from, "<"); idx > 1 {
							from = strings.TrimSpace(from[:idx])
						}
						statusText = fmt.Sprintf("New from %s", from)
						notifyText = fmt.Sprintf("%s — %s", from, newMsgs[0].Subject)
					} else {
						statusText = fmt.Sprintf("%d new messages", len(newMsgs))
						from := newMsgs[0].From
						if idx := strings.Index(from, "<"); idx > 1 {
							from = strings.TrimSpace(from[:idx])
						}
						notifyText = fmt.Sprintf("%d new — %s: %s", len(newMsgs), from, newMsgs[0].Subject)
					}
					m.status = statusText
					// OSC 9 desktop notification (Ghostty, iTerm2, etc.)
					cmds = append(cmds, func() tea.Msg {
						fmt.Fprintf(os.Stderr, "\x1b]9;%s\a", notifyText)
						return nil
					})
				}
			}
			// Track inbox IDs
			if m.folders[msg.tabIdx].labelID == "INBOX" {
				m.lastInboxIDs = make(map[string]bool, len(msg.messages))
				for _, newMsg := range msg.messages {
					m.lastInboxIDs[newMsg.ID] = true
				}
			}
			return m, tea.Batch(cmds...)
		}

	case moreMessagesMsg:
		if msg.query != "" {
			// Search "load more"
			if m.searchCache == nil {
				return m, nil
			}
			m.searchCache.loadingMore = false
			if msg.err != nil {
				return m, nil
			}
			m.searchCache.messages = append(m.searchCache.messages, msg.messages...)
			m.searchCache.nextPageToken = msg.nextPageToken
			if len(msg.messages) > 0 {
				return m, m.enrichAttachments(msg.messages, -1)
			}
			return m, nil
		}
		if m.cache[msg.tabIdx] == nil {
			return m, nil
		}
		target := m.cache[msg.tabIdx]
		target.loadingMore = false
		if msg.err != nil {
			return m, nil // silently ignore — messages already visible
		}
		target.messages = append(target.messages, msg.messages...)
		target.nextPageToken = msg.nextPageToken
		if len(target.messages) > target.highWaterMark {
			target.highWaterMark = len(target.messages)
		}
		if len(msg.messages) > 0 {
			return m, m.enrichAttachments(msg.messages, msg.tabIdx)
		}

	case attachmentEnrichMsg:
		// Store results in persistent cache
		for id, has := range msg.attachments {
			m.attachmentCache[id] = has
		}
		if msg.tabIdx == -1 {
			if m.searchCache != nil {
				for i := range m.searchCache.messages {
					if has, ok := msg.attachments[m.searchCache.messages[i].ID]; ok {
						m.searchCache.messages[i].HasAttachments = has
					}
				}
			}
		} else if m.cache[msg.tabIdx] != nil {
			target := m.cache[msg.tabIdx]
			for i := range target.messages {
				if has, ok := msg.attachments[target.messages[i].ID]; ok {
					target.messages[i].HasAttachments = has
				}
			}
		}

	case trashDoneMsg:
		if msg.err != nil {
			m.status = "Error: " + msg.err.Error()
			// Re-fetch to restore correct state since optimistic removal was wrong
			m.syncing = true
			return m, m.fetchMessages()
		} else if msg.count > 1 {
			m.status = fmt.Sprintf("%d messages trashed.", msg.count)
		} else {
			m.status = "Message trashed."
		}
		// Success: trust the optimistic removal, no re-fetch needed

	case starDoneMsg:
		if msg.err != nil {
			m.status = "Error: " + msg.err.Error()
		}
		// Invalidate Starred tab cache
		for i, f := range m.folders {
			if f.labelID == "STARRED" {
				delete(m.cache, i)
				break
			}
		}

	case archiveDoneMsg:
		if msg.err != nil {
			m.status = "Error: " + msg.err.Error()
			m.syncing = true
			return m, m.fetchMessages()
		} else if msg.count > 1 {
			m.status = fmt.Sprintf("%d messages archived.", msg.count)
		} else {
			m.status = "Message archived."
		}

	case deleteDoneMsg:
		if msg.err != nil {
			m.status = "Error: " + msg.err.Error()
			m.syncing = true
			return m, m.fetchMessages()
		}
		m.status = "Message permanently deleted."

	case restoreDoneMsg:
		if msg.err != nil {
			m.status = "Error: " + msg.err.Error()
			m.syncing = true
			return m, m.fetchMessages()
		}
		m.status = "Message restored to Inbox."
		// Invalidate the Inbox tab cache so it refreshes when the user switches to it
		delete(m.cache, 0)

	case toggleReadDoneMsg:
		if msg.err != nil {
			m.status = "Error: " + msg.err.Error()
		}

	case pollTickMsg:
		if m.skipNextPoll {
			m.skipNextPoll = false
			return m, m.pollTick()
		}
		if m.searchQuery == "" {
			m.syncing = true
			return m, tea.Batch(m.fetchMessages(), m.pollTick())
		}
		return m, m.pollTick() // don't poll while searching

	case blockDoneMsg:
		if msg.err != nil {
			m.status = "Error: " + msg.err.Error()
		} else {
			m.status = "Sender blocked — future emails will be trashed."
		}

	case labelDeletedMsg:
		if msg.err != nil {
			m.status = "Error deleting label: " + msg.err.Error()
			return m, nil
		}
		// Remove from customLabels
		for i, l := range m.customLabels {
			if l.labelID == msg.labelID {
				m.customLabels = append(m.customLabels[:i], m.customLabels[i+1:]...)
				break
			}
		}
		// If we're viewing this label, switch back to inbox
		for i, f := range m.folders {
			if f.labelID == msg.labelID {
				delete(m.cache, i)
				if m.tabIdx == i {
					m.tabIdx = 0
					m.syncing = true
					m.status = "Label deleted."
					return m, m.fetchMessages()
				}
				break
			}
		}
		m.status = "Label deleted."

	case labelRenamedMsg:
		if msg.err != nil {
			m.status = "Error renaming label: " + msg.err.Error()
			return m, nil
		}
		// Update in customLabels
		for i, l := range m.customLabels {
			if l.labelID == msg.labelID {
				m.customLabels[i].name = msg.newName
				break
			}
		}
		// Update in folders if currently viewing
		for i, f := range m.folders {
			if f.labelID == msg.labelID {
				m.folders[i].name = msg.newName
				break
			}
		}
		m.status = fmt.Sprintf("Label renamed to \"%s\".", msg.newName)

	case labelCreatedMsg:
		if msg.err != nil {
			m.status = "Error creating label: " + msg.err.Error()
			return m, nil
		}
		// Add to custom labels
		newFolder := folder{name: msg.label.Name, labelID: msg.label.ID}
		m.customLabels = append(m.customLabels, newFolder)

		if msg.mode == 1 || msg.mode == 2 {
			// Apply the new label to the tagged messages
			ids := msg.ids
			move := msg.mode == 2
			if move {
				fc := m.fc()
				m.optimisticRemove(fc, ids)
				m.skipNextPoll = true
			}
			m.status = fmt.Sprintf("Label \"%s\" created and applied.", msg.label.Name)
			return m, func() tea.Msg {
				add := []string{msg.label.ID}
				var remove []string
				if move {
					remove = []string{"INBOX"}
				}
				err := m.client.ModifyMessages(m.ctx, ids, add, remove)
				if err != nil {
					return common.StatusMsg{Text: "Error: " + err.Error()}
				}
				return common.StatusMsg{Text: fmt.Sprintf("Label \"%s\" created and applied.", msg.label.Name)}
			}
		}

		// Browse mode: open the new label as folder
		labelSlot := len(defaultFolders)
		if len(m.folders) > labelSlot {
			m.folders[labelSlot] = newFolder
		} else {
			m.folders = append(m.folders, newFolder)
		}
		if m.tabIdx < len(defaultFolders) {
			m.prevTabIdx = m.tabIdx
		}
		m.tabIdx = labelSlot
		delete(m.cache, labelSlot)
		m.searchQuery = ""
		m.searchCache = nil
		m.syncing = true
		m.status = fmt.Sprintf("Label \"%s\" created.", msg.label.Name)
		return m, m.fetchMessages()

	case labelsLoadedMsg:
		if msg.err == nil {
			// User-created labels have IDs starting with "Label_".
			// System labels (INBOX, STARRED, IMPORTANT) are in the separate
			// systemLabels slice shown in the picker's second section.
			m.customLabels = nil
			for _, l := range msg.labels {
				if strings.HasPrefix(l.ID, "Label_") {
					m.customLabels = append(m.customLabels, folder{name: l.Name, labelID: l.ID})
				}
			}
		}

	case common.StatusMsg:
		m.status = msg.Text
		m.statusLoading = false

	case spinner.TickMsg:
		// Always keep the spinner ticking so it animates immediately
		// when syncing or statusLoading becomes true.
		// Advance ellipsis every 2nd tick (~500ms per dot).
		m.dotFrame = (m.dotFrame + 1) % 6
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

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
			if cmd := m.maybePrefetch(fc); cmd != nil {
				return m, cmd
			}
		case tea.MouseButtonLeft:
			if msg.Action == tea.MouseActionRelease {
				headerRows := 4 // tabs(2) + sync(1) + padding(1)
				if m.searching || m.searchQuery != "" {
					headerRows = 5 // + search bar
				}
				row := msg.Y - headerRows
				contentHeight := m.contentHeight()
				start := viewStart(fc.cursor, contentHeight, len(fc.messages))
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

		// Jump-to mode — typing digits
		if m.jumping {
			switch {
			case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
				fc := m.fc()
				n, _ := strconv.Atoi(m.jumpInput)
				m.jumping = false
				m.jumpInput = ""
				if n > 0 && len(fc.messages) > 0 {
					if n > len(fc.messages) {
						n = len(fc.messages)
					}
					fc.cursor = n - 1 // 1-indexed → 0-indexed
				}
				return m, m.maybePrefetch(fc)

			case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
				m.jumping = false
				m.jumpInput = ""
				return m, nil

			case key.Matches(msg, key.NewBinding(key.WithKeys("backspace"))):
				if len(m.jumpInput) > 0 {
					m.jumpInput = m.jumpInput[:len(m.jumpInput)-1]
				}
				if len(m.jumpInput) == 0 {
					m.jumping = false
				}
				return m, nil

			default:
				if len(msg.String()) == 1 && msg.String()[0] >= '0' && msg.String()[0] <= '9' {
					m.jumpInput += msg.String()
					return m, nil
				}
				// Non-digit cancels jump mode
				m.jumping = false
				m.jumpInput = ""
			}
		}

		// Label picker mode
		if m.showLabelPicker {
			// Rename sub-mode
			if m.labelRenaming {
				switch {
				case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
					newName := strings.TrimSpace(m.labelRenameInput.Value())
					if newName != "" && len(m.labelPickerFiltered) > 0 && m.labelPickerCursor < len(m.labelPickerFiltered) {
						chosen := m.customLabels[m.labelPickerFiltered[m.labelPickerCursor]]
						m.labelRenaming = false
						m.status = fmt.Sprintf("Renaming to \"%s\"...", newName)
						return m, func() tea.Msg {
							err := m.client.RenameLabel(m.ctx, chosen.labelID, newName)
							return labelRenamedMsg{labelID: chosen.labelID, newName: newName, err: err}
						}
					}
					m.labelRenaming = false
					return m, nil
				case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
					m.labelRenaming = false
					return m, nil
				default:
					var cmd tea.Cmd
					m.labelRenameInput, cmd = m.labelRenameInput.Update(msg)
					return m, cmd
				}
			}

			switch {
			case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
				m.showLabelPicker = false
				return m, nil

			case key.Matches(msg, common.Keys.NextTab):
				// TAB: toggle between user labels and system labels sections
				m.labelPickerCursors[m.labelPickerSection] = m.labelPickerCursor
				if m.labelPickerSection == 0 {
					m.labelPickerSection = 1
				} else {
					m.labelPickerSection = 0
				}
				m.labelPickerFiltered = filterLabels(m.pickerLabels(), m.labelPickerInput.Value())
				m.labelPickerCursor = m.labelPickerCursors[m.labelPickerSection]
				if m.labelPickerCursor >= len(m.labelPickerFiltered) {
					m.labelPickerCursor = 0
				}
				return m, nil

			case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
				activeLabels := m.pickerLabels()
				if len(m.labelPickerFiltered) > 0 && m.labelPickerCursor >= 0 && m.labelPickerCursor < len(m.labelPickerFiltered) {
					chosen := activeLabels[m.labelPickerFiltered[m.labelPickerCursor]]
					m.showLabelPicker = false

					if m.labelPickerMode == 1 || m.labelPickerMode == 2 {
						ids := m.labelPickerTagIDs
						m.labelPickerTagIDs = nil
						move := m.labelPickerMode == 2
						if move {
							// Move: optimistically remove from current view
							fc := m.fc()
							m.optimisticRemove(fc, ids)
							m.skipNextPoll = true
							m.status = fmt.Sprintf("Moving to \"%s\"...", chosen.name)
						} else {
							m.status = fmt.Sprintf("Labeling as \"%s\"...", chosen.name)
						}
						return m, func() tea.Msg {
							add := []string{chosen.labelID}
							var remove []string
							if move {
								remove = []string{"INBOX"}
							}
							err := m.client.ModifyMessages(m.ctx, ids, add, remove)
							if err != nil {
								return common.StatusMsg{Text: "Error: " + err.Error()}
							}
							if move {
								return common.StatusMsg{Text: fmt.Sprintf("Moved to \"%s\".", chosen.name)}
							}
							return common.StatusMsg{Text: fmt.Sprintf("Labeled as \"%s\".", chosen.name)}
						}
					}

					// Browse mode: open label as folder
					labelSlot := len(defaultFolders)
					if len(m.folders) > labelSlot {
						m.folders[labelSlot] = chosen
					} else {
						m.folders = append(m.folders, chosen)
					}
					if m.tabIdx < len(defaultFolders) {
						m.prevTabIdx = m.tabIdx
					}
					m.tabIdx = labelSlot
					delete(m.cache, labelSlot)
					m.searchQuery = ""
					m.searchCache = nil
					m.syncing = true
					return m, m.fetchMessages()
				}
				// No match in list — create new label from typed text (user section only)
				if m.labelPickerSection == 0 {
					name := strings.TrimSpace(m.labelPickerInput.Value())
					if name != "" {
						mode := m.labelPickerMode
						ids := m.labelPickerTagIDs
						m.showLabelPicker = false
						m.status = fmt.Sprintf("Creating label \"%s\"...", name)
						return m, func() tea.Msg {
							label, err := m.client.CreateLabel(m.ctx, name)
							return labelCreatedMsg{label: label, err: err, mode: mode, ids: ids}
						}
					}
				}
				return m, nil

			case key.Matches(msg, key.NewBinding(key.WithKeys("up"))):
				if m.labelPickerCursor > 0 {
					m.labelPickerCursor--
				}
				return m, nil

			case key.Matches(msg, key.NewBinding(key.WithKeys("down"))):
				if m.labelPickerCursor < len(m.labelPickerFiltered)-1 {
					m.labelPickerCursor++
				}
				return m, nil

			case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+d"))):
				// Delete selected label (user-created only)
				if m.labelPickerSection == 0 && m.labelPickerMode == 0 && len(m.labelPickerFiltered) > 0 && m.labelPickerCursor >= 0 && m.labelPickerCursor < len(m.labelPickerFiltered) {
					chosen := m.customLabels[m.labelPickerFiltered[m.labelPickerCursor]]
					if !strings.HasPrefix(chosen.labelID, "Label_") {
						return m, nil
					}
					m.showLabelPicker = false
					m.status = fmt.Sprintf("Deleting label \"%s\"...", chosen.name)
					return m, func() tea.Msg {
						err := m.client.DeleteLabel(m.ctx, chosen.labelID)
						return labelDeletedMsg{labelID: chosen.labelID, err: err}
					}
				}
				return m, nil

			case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+r"))):
				// Rename selected label (user-created only)
				if m.labelPickerSection == 0 && m.labelPickerMode == 0 && len(m.labelPickerFiltered) > 0 && m.labelPickerCursor >= 0 && m.labelPickerCursor < len(m.labelPickerFiltered) {
					chosen := m.customLabels[m.labelPickerFiltered[m.labelPickerCursor]]
					if !strings.HasPrefix(chosen.labelID, "Label_") {
						return m, nil
					}
					m.labelRenaming = true
					m.labelRenameInput.SetValue(chosen.name)
					m.labelRenameInput.Focus()
					m.labelPickerInput.Blur()
				}
				return m, nil

			default:
				var cmd tea.Cmd
				m.labelPickerInput, cmd = m.labelPickerInput.Update(msg)
				m.labelPickerFiltered = filterLabels(m.pickerLabels(), m.labelPickerInput.Value())
				if m.labelPickerCursor >= len(m.labelPickerFiltered) {
					m.labelPickerCursor = 0
				}
				return m, cmd
			}
		}

		fc := m.fc()

		// Start jump mode on digit key press
		if len(msg.String()) == 1 && msg.String()[0] >= '1' && msg.String()[0] <= '9' {
			m.jumping = true
			m.jumpInput = msg.String()
			return m, nil
		}

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
			if cmd := m.maybePrefetch(fc); cmd != nil {
				return m, cmd
			}

		case key.Matches(msg, common.Keys.Home):
			fc.cursor = 0

		case key.Matches(msg, common.Keys.End):
			if len(fc.messages) > 0 {
				fc.cursor = len(fc.messages) - 1
			}
			if cmd := m.maybePrefetch(fc); cmd != nil {
				return m, cmd
			}

		case key.Matches(msg, common.Keys.NextTab):
			// Cycle through default folders only; remove custom label tab if present
			nDefault := len(defaultFolders)
			m.tabIdx = (m.tabIdx + 1) % nDefault
			if len(m.folders) > len(defaultFolders) {
				m.folders = m.folders[:len(defaultFolders)]
			}
			m.searchQuery = ""
			m.searchCache = nil
			m.searching = false
			m.syncing = true
			m.err = ""
			return m, m.fetchMessages()

		case key.Matches(msg, common.Keys.PrevTab):
			nDefault := len(defaultFolders)
			m.tabIdx = (m.tabIdx - 1 + nDefault) % nDefault
			if len(m.folders) > len(defaultFolders) {
				m.folders = m.folders[:len(defaultFolders)]
			}
			m.searchQuery = ""
			m.searchCache = nil
			m.searching = false
			m.syncing = true
			m.err = ""
			return m, m.fetchMessages()

		case key.Matches(msg, key.NewBinding(key.WithKeys("/", "f"))):
			m.searching = true
			m.searchInput.SetValue("")
			m.searchInput.Focus()
			return m, textinput.Blink

		case key.Matches(msg, common.Keys.Back):
			// Esc: clear selection first, then search, then leave label folder
			if len(fc.selected) > 0 {
				fc.selected = make(map[string]bool)
				return m, nil
			}
			if m.searchQuery != "" {
				m.searchQuery = ""
				m.searchCache = nil
				m.searchInput.SetValue("")
				return m, nil
			}
			// Leave label folder → return to previous folder
			if m.tabIdx >= len(defaultFolders) {
				m.tabIdx = m.prevTabIdx
				if len(m.folders) > len(defaultFolders) {
					m.folders = m.folders[:len(defaultFolders)]
				}
				m.searchQuery = ""
				m.searchCache = nil
				m.syncing = true
				return m, m.fetchMessages()
			}

		case key.Matches(msg, common.Keys.Open):
			if len(fc.messages) > 0 && fc.cursor < len(fc.messages) {
				id := fc.messages[fc.cursor].ID
				return m, func() tea.Msg { return common.FetchMessageMsg{ID: id} }
			}

		case key.Matches(msg, common.Keys.Edit):
			// Edit draft from inbox
			if m.currentLabelID() == "DRAFT" && len(fc.selected) == 0 && len(fc.messages) > 0 && fc.cursor < len(fc.messages) {
				id := fc.messages[fc.cursor].ID
				return m, func() tea.Msg { return common.EditDraftMsg{ID: id} }
			}

		case key.Matches(msg, common.Keys.Send):
			// Send draft directly from inbox
			if m.currentLabelID() == "DRAFT" && len(fc.selected) == 0 && len(fc.messages) > 0 && fc.cursor < len(fc.messages) {
				id := fc.messages[fc.cursor].ID
				return m, func() tea.Msg { return common.SendDraftMsg{ID: id} }
			}

		case key.Matches(msg, common.Keys.Reply):
			// Quick reply from inbox — only when no multi-select
			if len(fc.selected) == 0 && len(fc.messages) > 0 && fc.cursor < len(fc.messages) {
				id := fc.messages[fc.cursor].ID
				return m, func() tea.Msg { return common.FetchAndReplyMsg{ID: id} }
			}

		case key.Matches(msg, common.Keys.ReplyAll):
			// Quick reply-all from inbox — only when no multi-select
			if len(fc.selected) == 0 && len(fc.messages) > 0 && fc.cursor < len(fc.messages) {
				id := fc.messages[fc.cursor].ID
				return m, func() tea.Msg { return common.FetchAndReplyAllMsg{ID: id} }
			}

		case key.Matches(msg, common.Keys.Compose):
			return m, func() tea.Msg { return common.ComposeMsg{Title: "Compose"} }

		case key.Matches(msg, common.Keys.Refresh):
			m.syncing = true
			if m.searchQuery != "" {
				return m, m.fetchSearch(m.searchQuery)
			}
			return m, m.fetchMessages()

		case key.Matches(msg, common.Keys.Preview):
			m.showPreview = !m.showPreview

		case key.Matches(msg, common.Keys.Select):
			if len(fc.messages) > 0 && fc.cursor < len(fc.messages) {
				id := fc.messages[fc.cursor].ID
				fc.selected[id] = !fc.selected[id]
				if !fc.selected[id] {
					delete(fc.selected, id)
				}
				// Move cursor down after selecting
				if fc.cursor < len(fc.messages)-1 {
					fc.cursor++
				}
			}

		case key.Matches(msg, common.Keys.SelectAll):
			if len(fc.messages) > 0 {
				// Any selected → deselect all, none selected → select all
				if len(fc.selected) > 0 {
					fc.selected = make(map[string]bool)
				} else {
					for _, msg := range fc.messages {
						fc.selected[msg.ID] = true
					}
				}
			}

		case key.Matches(msg, common.Keys.Trash):
			ids, subjects := m.selectedOrCursor(fc)
			if len(ids) > 0 {
				// Optimistically remove from list
				m.optimisticRemove(fc, ids)

				label := m.currentLabelID()
				switch label {
				case "TRASH":
					// Permanent delete not supported by Gmail API; ignore
					return m, nil
				default:
					if len(ids) == 1 {
						m.status = fmt.Sprintf("Trashing \"%s\"...", truncate(subjects[0], 40))
					} else {
						m.status = fmt.Sprintf("Trashing %d messages...", len(ids))
					}
					return m, m.trashMessages(ids)
				}
			}

		case key.Matches(msg, common.Keys.Restore):
			label := m.currentLabelID()
			if label == "TRASH" {
				ids, subjects := m.selectedOrCursor(fc)
				if len(ids) > 0 {
					m.optimisticRemove(fc, ids)
					if len(ids) == 1 {
						m.status = fmt.Sprintf("Restoring \"%s\"...", truncate(subjects[0], 40))
					} else {
						m.status = fmt.Sprintf("Restoring %d messages...", len(ids))
					}
					return m, m.restoreMessages(ids)
				}
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("m"))):
			if len(fc.messages) > 0 {
				// Collect target messages: selected if any, else cursor
				type target struct {
					idx int
					id  string
				}
				var targets []target
				for i, msg := range fc.messages {
					if fc.selected[msg.ID] {
						targets = append(targets, target{i, msg.ID})
					}
				}
				if len(targets) == 0 && fc.cursor < len(fc.messages) {
					targets = []target{{fc.cursor, fc.messages[fc.cursor].ID}}
				}
				if len(targets) == 0 {
					return m, nil
				}
				// Determine direction from the first target
				markUnread := !fc.messages[targets[0].idx].Unread
				var ids []string
				for _, t := range targets {
					fc.messages[t.idx].Unread = markUnread
					ids = append(ids, t.id)
				}
				if markUnread {
					if len(ids) == 1 {
						m.status = "Marked as unread"
					} else {
						m.status = fmt.Sprintf("%d messages marked as unread", len(ids))
					}
				} else {
					if len(ids) == 1 {
						m.status = "Marked as read"
					} else {
						m.status = fmt.Sprintf("%d messages marked as read", len(ids))
					}
				}
				return m, m.toggleReadMessages(ids, markUnread)
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("b"))):
			if len(fc.messages) > 0 && fc.cursor < len(fc.messages) {
				from := fc.messages[fc.cursor].From
				email := extractEmail(from)
				if email != "" {
					m.status = fmt.Sprintf("Blocking %s...", email)
					return m, func() tea.Msg {
						err := m.client.BlockSender(m.ctx, email)
						return blockDoneMsg{err: err}
					}
				}
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("L"))):
			m.showLabelPicker = true
			m.labelPickerMode = 0
			m.labelPickerSection = 0
			m.labelPickerInput.SetValue("")
			m.labelPickerInput.Focus()
			m.labelPickerCursor = 0
			m.labelPickerFiltered = filterLabels(m.pickerLabels(), "")
			return m, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("t"))):
			ids, _ := m.selectedOrCursor(fc)
			if len(ids) > 0 {
				m.showLabelPicker = true
				m.labelPickerMode = 1
				m.labelPickerSection = 0
				m.labelPickerTagIDs = ids
				m.labelPickerInput.SetValue("")
				m.labelPickerInput.Focus()
				m.labelPickerCursor = 0
				m.labelPickerFiltered = filterLabels(m.pickerLabels(), "")
			}
			return m, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("T"))):
			ids, _ := m.selectedOrCursor(fc)
			if len(ids) > 0 {
				m.showLabelPicker = true
				m.labelPickerMode = 2 // move = tag + archive
				m.labelPickerSection = 0
				m.labelPickerTagIDs = ids
				m.labelPickerInput.SetValue("")
				m.labelPickerInput.Focus()
				m.labelPickerCursor = 0
				m.labelPickerFiltered = filterLabels(m.pickerLabels(), "")
			}
			return m, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("s"))):
			if len(fc.messages) > 0 {
				type target struct {
					idx int
					id  string
				}
				var targets []target
				for i, msg := range fc.messages {
					if fc.selected[msg.ID] {
						targets = append(targets, target{i, msg.ID})
					}
				}
				if len(targets) == 0 && fc.cursor < len(fc.messages) {
					targets = []target{{fc.cursor, fc.messages[fc.cursor].ID}}
				}
				if len(targets) == 0 {
					return m, nil
				}
				// Toggle based on first target's state
				star := !fc.messages[targets[0].idx].Starred
				var ids []string
				for _, t := range targets {
					fc.messages[t.idx].Starred = star
					ids = append(ids, t.id)
				}
				if star {
					m.status = "Starred"
				} else {
					m.status = "Unstarred"
				}
				return m, m.toggleStarMessages(ids, star)
			}

		case key.Matches(msg, common.Keys.Archive):
			label := m.currentLabelID()
			if label == "INBOX" {
				ids, subjects := m.selectedOrCursor(fc)
				if len(ids) > 0 {
					m.optimisticRemove(fc, ids)
					if len(ids) == 1 {
						m.status = fmt.Sprintf("Archiving \"%s\"...", truncate(subjects[0], 40))
					} else {
						m.status = fmt.Sprintf("Archiving %d messages...", len(ids))
					}
					return m, m.archiveMessages(ids)
				}
			}
		}
	}
	return m, nil
}

func (m Model) keybindsForFolder(fc *folderCache) string {
	sel := ""
	if len(fc.selected) > 0 {
		sel = fmt.Sprintf(" [%d selected]", len(fc.selected))
	}

	base := " j/k=nav  space=select  a=all" + sel
	label := m.currentLabelID()
	labelHint := ""
	if len(m.customLabels) > 0 {
		labelHint = "  L=labels"
	}
	suffix := "  f=search  R=reply all  ctrl+r=refresh  ,=settings  tab=folder" + labelHint + "  K=keys  q=quit"

	noSel := len(fc.selected) == 0

	// Show m=mark read/unread based on current/selected message state
	markHint := ""
	if noSel && fc.cursor < len(fc.messages) {
		if fc.messages[fc.cursor].Unread {
			markHint = "  m=read"
		} else {
			markHint = "  m=unread"
		}
	} else if !noSel {
		markHint = "  m=read/unread"
	}

	switch label {
	case "TRASH":
		return base + "  u=restore" + markHint + suffix
	case "DRAFT":
		extra := "  d=trash"
		if noSel {
			extra += "  e=edit  y=send"
		}
		return base + extra + markHint + suffix
	case "SENT":
		extra := "  d=trash"
		if noSel {
			extra += "  r=reply"
		}
		return base + extra + markHint + suffix
	default: // INBOX and custom labels
		extra := "  s=star  t=label  T=move  A=archive  d=trash  b=block"
		if noSel {
			extra += "  r=reply"
		}
		return base + extra + markHint + suffix
	}
}

// pickerLabels returns the label list for the active picker section.
func (m Model) pickerLabels() []folder {
	if m.labelPickerSection == 1 {
		return systemLabels
	}
	return m.customLabels
}

func filterLabels(labels []folder, query string) []int {
	lower := strings.ToLower(query)
	var result []int
	for i, l := range labels {
		if lower == "" || strings.Contains(strings.ToLower(l.name), lower) {
			result = append(result, i)
		}
	}
	return result
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
	tabs := make([]string, len(m.folders))
	for i, f := range m.folders {
		label := f.name
		if f.labelID == "INBOX" {
			if c := m.cache[i]; c != nil {
				unread := 0
				for _, msg := range c.messages {
					if msg.Unread {
						unread++
					}
				}
				if unread > 0 {
					label = fmt.Sprintf("%s (%d)", f.name, unread)
				}
			}
		}
		if i == m.tabIdx {
			tabs[i] = common.ActiveTab.Render(label)
		} else {
			tabs[i] = common.InactiveTab.Render(label)
		}
	}
	tabContent := strings.Join(tabs, "")
	acctDisplay := m.AccountEmail
	if acctDisplay == "" {
		acctDisplay = m.AccountName
	}
	if acctDisplay != "" {
		acctLabel := lipgloss.NewStyle().Foreground(common.Muted).Render(acctDisplay + "  ")
		tabsW := lipgloss.Width(tabContent)
		acctW := lipgloss.Width(acctLabel)
		gap := m.width - tabsW - acctW - 2 // -2 for border padding
		if gap > 0 {
			tabContent += strings.Repeat(" ", gap) + acctLabel
		}
	}
	tabRow := common.TabBar.Width(m.width).Render(tabContent)
	b.WriteString(tabRow + "\n")

	// 2. Sync status line
	// Left zone: fixed width to align action text with subject column
	// Layout mirrors message row: pad(1) + numCol + check(2) + unread(2) + from
	listWidth := m.width
	if m.showPreview {
		listWidth = m.width * 6 / 10
	}
	numW := len(strconv.Itoa(len(fc.messages)))
	if numW < 2 {
		numW = 2
	}
	fromW := (listWidth - 2) / 4
	if fromW > 24 {
		fromW = 24
	}
	if fromW < 12 {
		fromW = 12
	}
	// Fixed left zone = 1(pad) + numW+1(numCol) + 2(check) + 2(unread+space) + fromW + 2(gap) + 2(attach) + 1(gap)
	leftZoneW := 1 + numW + 1 + 2 + 2 + fromW + 2 + 2 + 1

	// Build left content: count + sync (icon is always 1 char to prevent shift)
	syncText := ""
	if m.syncing {
		syncText = m.spinner.View() + " " + common.SyncingStyle.Render("Syncing...")
	} else if m.err != "" {
		syncText = common.ErrorStyle.Render("! Error: " + m.err)
	} else if !fc.lastSync.IsZero() {
		check := common.SyncedStyle.Render("✓")
		ago := time.Since(fc.lastSync).Truncate(time.Second)
		if ago < 5*time.Second {
			syncText = check + " " + common.SyncedStyle.Render("Synced")
		} else if ago < time.Minute {
			syncText = check + " " + common.SyncedStyle.Render(fmt.Sprintf("Synced %ds ago", int(ago.Seconds())))
		} else {
			syncText = check + " " + common.MutedStyle.Render(fmt.Sprintf("Synced %dm ago", int(ago.Minutes())))
		}
	}
	countText := common.MutedStyle.Render(fmt.Sprintf("%d messages", len(fc.messages)))
	leftContent := " " + countText + "  " + syncText

	// Pad left zone to fixed width using ANSI-aware width measurement
	leftActualW := lipgloss.Width(leftContent)
	leftRendered := leftContent
	if leftActualW < leftZoneW {
		leftRendered += strings.Repeat(" ", leftZoneW-leftActualW)
	}

	// Right zone: action text (starts at subject column)
	rightParts := ""
	if m.jumping {
		rightParts = common.SyncingStyle.Render(fmt.Sprintf("Go to: %s_", m.jumpInput))
	} else if m.status != "" {
		if m.statusLoading {
			rightParts = m.spinner.View() + " " + common.SyncingStyle.Render(m.status)
		} else {
			rightParts = common.MutedStyle.Render(m.status)
		}
	}
	if m.searchQuery != "" {
		if rightParts != "" {
			rightParts += "  "
		}
		rightParts += common.SyncingStyle.Render(fmt.Sprintf("Search: \"%s\"", m.searchQuery))
	}

	b.WriteString(leftRendered + rightParts + "\n")

	// Padding below status line
	b.WriteString("\n")

	// 3. Search bar (if active or has query)
	if m.searching {
		searchBar := common.SearchInputStyle.
			Width(m.width).
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

	// 4. Keybinds bar (appended at the bottom) — adapts to active folder and selection
	keybindText := m.keybindsForFolder(fc)
	// Truncate to prevent line wrapping (StatusBar has padding(0,1) = 2 chars + border)
	maxKeyW := m.width - 4
	if maxKeyW > 0 && rw.StringWidth(keybindText) > maxKeyW {
		keybindText = rw.Truncate(keybindText, maxKeyW, "…")
	}
	keybinds := common.StatusBar.Width(m.width).Render(keybindText)

	contentHeight := m.contentHeight()

	// 5. Message list
	if len(fc.messages) == 0 {
		if m.syncing {
			phase := m.dotFrame / 2 // 0, 1, 2 — each held for 2 spinner ticks
			dots := strings.Repeat(".", phase+1) + strings.Repeat(" ", 2-phase)
			b.WriteString("\n  Loading messages" + dots + "\n")
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
	start := viewStart(fc.cursor, visibleRows, len(fc.messages))
	end := start + visibleRows
	if end > len(fc.messages) {
		end = len(fc.messages)
	}

	// Selection column is always present (2 chars) — no layout shift
	checkW := 2
	numColW := numW + 1 // number + space

	attachStyle := lipgloss.NewStyle().Foreground(common.Warning)

	for i := start; i < end; i++ {
		msg := fc.messages[i]
		// Build raw text line (no ANSI codes) — styled as a whole at the end
		lineNum := fmt.Sprintf("%*d ", numW, i+1)

		check := "  " // always 2 chars
		if fc.selected[msg.ID] {
			check = "→ "
		}

		content := formatMessageLine(msg, listWidth-2-numColW-checkW)
		raw := lineNum + check + content
		raw = runewidthPadRight(raw, listWidth-2)

		// Apply single style to the entire line
		if i == fc.cursor {
			listLines = append(listLines, common.SelectedMessage.Padding(0, 0).Width(0).Render(" "+raw+" "))
		} else if fc.selected[msg.ID] {
			listLines = append(listLines, common.CheckedMessage.Padding(0, 0).Width(0).Render(" "+raw+" "))
		} else if msg.Unread {
			listLines = append(listLines, common.UnreadMessage.Padding(0, 0).Width(0).Render(" "+raw+" "))
		} else {
			// Dim line number, colorize @ indicator, normal style for the rest
			styledNum := common.MutedStyle.Render(lineNum)
			rest := check + content
			rest = runewidthPadRight(rest, listWidth-2-numColW)
			// Colorize the "@ " attachment indicator within the read-message style
			if msg.HasAttachments {
				rest = strings.Replace(rest, "@ ", attachStyle.Render("@")+" ", 1)
			}
			line := common.ReadMessage.Padding(0, 0).Width(0).Render(" " + styledNum + rest + " ")
			listLines = append(listLines, line)
		}
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

	base := b.String()

	// 7. Label picker overlay
	if m.showLabelPicker {
		return m.renderLabelPickerOverlay(base)
	}

	return base
}

func (m Model) renderLabelPickerOverlay(base string) string {
	innerWidth := 44
	maxItems := 8
	var lines []string

	titleText := "Labels"
	switch m.labelPickerMode {
	case 1:
		titleText = "Apply Label"
	case 2:
		titleText = "Move to Label"
	}
	title := lipgloss.NewStyle().Bold(true).Foreground(common.Primary).Render(titleText)
	lines = append(lines, title)
	lines = append(lines, "")
	lines = append(lines, m.labelPickerInput.View())
	lines = append(lines, "")

	highlight := lipgloss.NewStyle().Bold(true).Foreground(common.White).Background(common.Primary)
	muted := lipgloss.NewStyle().Foreground(common.Muted)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("#4B5563"))
	arrowStyle := lipgloss.NewStyle().Foreground(common.Secondary)
	sectionHeader := lipgloss.NewStyle().Bold(true).Foreground(common.Secondary)

	// --- User Labels section ---
	userFiltered := filterLabels(m.customLabels, m.labelPickerInput.Value())
	if len(userFiltered) == 0 {
		lines = append(lines, muted.Render("  (no matching labels)"))
	} else {
		m.renderUserLabels(&lines, m.customLabels, userFiltered, maxItems, innerWidth, highlight, muted, dim, arrowStyle)
	}

	// --- Separator ---
	sep := strings.Repeat("─", innerWidth)
	lines = append(lines, dim.Render(sep))

	// --- System Labels section ---
	if m.labelPickerSection == 1 {
		sysFiltered := filterLabels(systemLabels, m.labelPickerInput.Value())
		lines = append(lines, sectionHeader.Render("  System"))
		lines = append(lines, "")
		if len(sysFiltered) == 0 {
			lines = append(lines, muted.Render("  (no matching labels)"))
		} else {
			m.renderSystemLabels(&lines, systemLabels, sysFiltered, innerWidth, highlight, muted)
		}
	} else {
		lines = append(lines, dim.Render("  System"))
	}

	// Rename input (shown inline below the list when active)
	if m.labelRenaming {
		lines = append(lines, "")
		renameLabel := lipgloss.NewStyle().Bold(true).Foreground(common.Primary).Render("Rename: ")
		lines = append(lines, renameLabel+m.labelRenameInput.View())
	}

	lines = append(lines, "")
	help := lipgloss.NewStyle().Foreground(common.Muted)
	if m.labelRenaming {
		lines = append(lines, help.Render("enter=save  esc=cancel"))
	} else {
		switch m.labelPickerMode {
		case 1:
			lines = append(lines, help.Render("enter=apply  tab=section  esc=cancel"))
		case 2:
			lines = append(lines, help.Render("enter=move  tab=section  esc=cancel"))
		default:
			if m.labelPickerSection == 0 {
				canEdit := true
				if len(m.labelPickerFiltered) > 0 && m.labelPickerCursor >= 0 && m.labelPickerCursor < len(m.labelPickerFiltered) {
					chosen := m.customLabels[m.labelPickerFiltered[m.labelPickerCursor]]
					canEdit = strings.HasPrefix(chosen.labelID, "Label_")
				}
				if canEdit {
					lines = append(lines, help.Render("enter=open  tab=section  ^r=rename  ^d=delete  esc=cancel"))
				} else {
					lines = append(lines, help.Render("enter=open  tab=section  ")+dim.Render("^r=rename  ^d=delete")+help.Render("  esc=cancel"))
				}
			} else {
				lines = append(lines, help.Render("enter=open  tab=section  esc=cancel"))
			}
		}
	}

	content := strings.Join(lines, "\n")

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(common.Secondary).
		Padding(1, 2).
		Width(innerWidth + 6).
		Render(content)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box,
		lipgloss.WithWhitespaceChars(" "),
		lipgloss.WithWhitespaceForeground(lipgloss.Color("#1F2937")))
}

// renderUserLabels renders the user labels section with windowed scrolling.
// When the section is active the cursor is highlighted; when inactive items are dimmed.
func (m Model) renderUserLabels(lines *[]string, labels []folder, filtered []int, maxItems, innerWidth int, highlight, muted, dim, arrowStyle lipgloss.Style) {
	total := len(filtered)
	isActive := m.labelPickerSection == 0

	// Windowed view — always crop to maxItems
	cursor := 0
	if isActive {
		cursor = m.labelPickerCursor
	}
	start := cursor - maxItems/2
	if start < 0 {
		start = 0
	}
	end := start + maxItems
	if end > total {
		end = total
		start = end - maxItems
		if start < 0 {
			start = 0
		}
	}

	if start > 0 {
		*lines = append(*lines, arrowStyle.Render("  ▲ more"))
	}
	for i := start; i < end; i++ {
		label := labels[filtered[i]].name
		if isActive && i == m.labelPickerCursor {
			*lines = append(*lines, highlight.Render(runewidthPadRight("  "+label, innerWidth)))
		} else if isActive {
			*lines = append(*lines, "  "+muted.Render(label))
		} else {
			*lines = append(*lines, "  "+dim.Render(label))
		}
	}
	if end < total {
		*lines = append(*lines, arrowStyle.Render("  ▼ more"))
	}
}

// renderSystemLabels renders the system labels section with cursor highlighting.
func (m Model) renderSystemLabels(lines *[]string, labels []folder, filtered []int, innerWidth int, highlight, muted lipgloss.Style) {
	for i, idx := range filtered {
		label := labels[idx].name
		if i == m.labelPickerCursor {
			*lines = append(*lines, highlight.Render(runewidthPadRight("  "+label, innerWidth)))
		} else {
			*lines = append(*lines, "  "+muted.Render(label))
		}
	}
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
	if msg.Starred {
		unread = "★"
	} else if msg.Unread {
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
		local := msg.Date.Local()
		now := time.Now()
		if local.Year() == now.Year() && local.YearDay() == now.YearDay() {
			dateStr = local.Format("15:04")
		} else if local.Year() == now.Year() {
			dateStr = local.Format("Jan 02")
		} else {
			dateStr = local.Format("Jan 06")
		}
	}
	dateStr = fmt.Sprintf("%*s", dateW, dateStr)

	// Fixed-width attachment indicator column (always 1 char + 1 space)
	attach := "  "
	if msg.HasAttachments {
		attach = "@ "
	}

	subjectW := width - 2 - fromW - 2 - 2 - 1 - 3 - dateW // unread(2) + from + gap(2) + attach(2) + gap(1) + gap(3) + date
	if subjectW < 0 {
		subjectW = 0
	}
	subject := msg.Subject
	if subject == "" {
		subject = "(no subject)"
	}
	subject = runewidthTruncate(subject, subjectW)

	// Build left portion and hard-truncate to exact width to handle emoji width mismatches
	left := fmt.Sprintf("%s %s  %s %s", unread, from, attach, subject)
	leftTarget := width - 3 - dateW // gap(3) + date
	left = runewidthTruncate(left, leftTarget)
	left = runewidthPadRight(left, leftTarget)

	return left + "   " + dateStr
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

// viewStart computes the first visible message index, keeping a scroll margin
// of 10 messages so the cursor stays away from the viewport edges.
func viewStart(cursor, visibleRows, totalMessages int) int {
	const scrollOff = 10
	off := scrollOff
	if off > visibleRows/2 {
		off = visibleRows / 2
	}
	start := cursor - visibleRows + 1 + off
	if start < 0 {
		start = 0
	}
	maxStart := totalMessages - visibleRows
	if maxStart < 0 {
		maxStart = 0
	}
	if start > maxStart {
		start = maxStart
	}
	return start
}

// extractEmail extracts the email address from a From header like "Name <email@example.com>".
func extractEmail(from string) string {
	if start := strings.Index(from, "<"); start >= 0 {
		if end := strings.Index(from[start:], ">"); end >= 0 {
			return from[start+1 : start+end]
		}
	}
	// Might be a bare email address
	if strings.Contains(from, "@") {
		return strings.TrimSpace(from)
	}
	return ""
}
