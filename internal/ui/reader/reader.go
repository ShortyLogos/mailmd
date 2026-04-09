package reader

import (
	"context"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/deric/mailmd/internal/gmail"
	"github.com/deric/mailmd/internal/markdown"
	"github.com/deric/mailmd/internal/ui/common"
	rw "github.com/mattn/go-runewidth"
)

// attachmentOpenedMsg signals an attachment was saved and opened.
type attachmentOpenedMsg struct{ err error }

var folders = []string{"Inbox", "Drafts", "Sent", "Trash"}

// Model is the reader Bubble Tea model.
type Model struct {
	ctx             context.Context
	client          gmail.Client
	message         *gmail.Message
	viewport        viewport.Model
	width           int
	height          int
	ready           bool
	tabIdx          int      // active folder tab (for display only)
	links           []string // extracted URLs, indexed from 1
	goLink          bool     // true when waiting for link number after 'g'
	renderedContent string   // cached rendered body for resize
}

// New creates a new reader model for the given message.
// bodyRenderedMsg carries the result of async body rendering.
type bodyRenderedMsg struct {
	content string
	links   []string
}

func New(ctx context.Context, client gmail.Client, msg *gmail.Message, width, height, tabIdx int) Model {
	m := Model{
		ctx:     ctx,
		client:  client,
		message: msg,
		width:   width,
		height:  height,
		tabIdx:  tabIdx,
	}
	m.initViewport("  Loading content...")
	return m
}

func (m *Model) initViewport(content string) {
	// Tab bar(1) + border(1) + From(1) + To(1) + Subject(1) + Date(1) + separator(1) + status(2)
	chrome := 9
	if len(m.message.Attachments) > 0 {
		chrome += len(m.message.Attachments)
	}
	vpHeight := m.height - chrome
	if vpHeight < 1 {
		vpHeight = 1
	}

	m.viewport = viewport.New(m.width, vpHeight)
	m.viewport.SetContent(content)
	m.ready = true
}


// Init starts body rendering (lightweight, no Glamour).
func (m Model) Init() tea.Cmd {
	msg := m.message
	return func() tea.Msg {
		body := msg.Body
		if body == "" && msg.HTMLBody != "" {
			body = stripHTML(msg.HTMLBody)
		}
		if body == "" {
			body = "(No message body)"
		}

		// Extract and number URLs (http/https)
		var links []string
		body = urlRegex.ReplaceAllStringFunc(body, func(rawURL string) string {
			links = append(links, rawURL)
			label := compactURL(rawURL, 50)
			return fmt.Sprintf("[%d: %s]", len(links), label)
		})

		// Strip mailto: prefix, leave plain email address (colorized later)
		body = mailtoRegex.ReplaceAllStringFunc(body, func(rawURL string) string {
			return strings.TrimPrefix(rawURL, "mailto:")
		})

		// Wrap text
		body = wrapText(body, 80)

		// Lightweight styling — colorize link refs and mailto refs
		rendered := renderPlainEmail(body)

		return bodyRenderedMsg{content: rendered, links: links}
	}
}

// renderPlainEmail applies minimal ANSI styling to plain text email body.
// Colors link references [N: ...] and email addresses.
func renderPlainEmail(body string) string {
	linkStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#38BDF8")).Italic(true) // sky blue
	mailStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#10B981"))              // green

	var result strings.Builder
	for _, line := range strings.Split(body, "\n") {
		// Colorize [N: ...] link references
		styled := linkRefRegex.ReplaceAllStringFunc(line, func(match string) string {
			return linkStyle.Render(match)
		})
		// Colorize email addresses
		styled = emailRegex.ReplaceAllStringFunc(styled, func(match string) string {
			return mailStyle.Render(match)
		})
		result.WriteString(styled + "\n")
	}
	return result.String()
}

var linkRefRegex = regexp.MustCompile(`\[\d+: [^\]]+\]`)

// stripHTML converts HTML to readable plain text by removing tags,
// converting block elements to newlines, and decoding entities.
func stripHTML(s string) string {
	// Replace block-level tags with newlines
	for _, tag := range []string{"<br", "<BR", "<p", "<P", "<div", "<DIV", "<tr", "<TR", "<li", "<LI", "<h1", "<h2", "<h3", "<h4", "<h5", "<h6"} {
		s = strings.ReplaceAll(s, tag, "\n"+tag)
	}
	// Remove style and script blocks entirely
	styleRegex := regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	scriptRegex := regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	s = styleRegex.ReplaceAllString(s, "")
	s = scriptRegex.ReplaceAllString(s, "")
	// Strip all HTML tags
	s = htmlTagRegex.ReplaceAllString(s, "")
	// Decode HTML entities
	s = html.UnescapeString(s)
	// Collapse excessive blank lines
	s = whitespaceRegex.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}
var emailRegex = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)

func (m Model) openAttachment(idx int) tea.Cmd {
	if idx < 0 || idx >= len(m.message.Attachments) {
		return nil
	}
	return m.downloadAndOpen([]gmail.Attachment{m.message.Attachments[idx]})
}

func (m Model) openAllImages() tea.Cmd {
	var images []gmail.Attachment
	for _, att := range m.message.Attachments {
		if isImage(att.MimeType) {
			images = append(images, att)
		}
	}
	if len(images) == 0 {
		return nil
	}
	return m.downloadAndOpen(images)
}

func (m Model) downloadAndOpen(attachments []gmail.Attachment) tea.Cmd {
	msgID := m.message.ID
	ctx := m.ctx
	client := m.client
	return func() tea.Msg {
		type result struct {
			path string
			err  error
		}
		results := make(chan result, len(attachments))
		for _, att := range attachments {
			go func(a gmail.Attachment) {
				data, err := client.GetAttachment(ctx, msgID, a.ID)
				if err != nil {
					results <- result{err: err}
					return
				}
				path := filepath.Join(os.TempDir(), "mailmd-"+a.Filename)
				if err := os.WriteFile(path, data, 0600); err != nil {
					results <- result{err: err}
					return
				}
				results <- result{path: path}
			}(att)
		}
		// Collect all results, then open
		var paths []string
		for range attachments {
			r := <-results
			if r.err != nil {
				return attachmentOpenedMsg{err: r.err}
			}
			paths = append(paths, r.path)
		}
		for _, p := range paths {
			openFile(p)
		}
		return attachmentOpenedMsg{}
	}
}

func openFile(path string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "linux":
		cmd = exec.Command("xdg-open", path)
	}
	if cmd != nil {
		cmd.Start()
	}
}

func isImage(mimeType string) bool {
	return strings.HasPrefix(mimeType, "image/")
}

// Update handles key events for the reader.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.initViewport(m.renderedContent)

	case bodyRenderedMsg:
		m.links = msg.links
		m.renderedContent = msg.content
		m.viewport.SetContent(msg.content)
		m.viewport.GotoTop()
		return m, nil

	case attachmentOpenedMsg:
		// Could show status, for now just ignore errors silently
		return m, nil

	case tea.MouseMsg:
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		// Link open mode: g was pressed, waiting for number
		if m.goLink {
			m.goLink = false
			if len(msg.String()) == 1 {
				c := msg.String()[0]
				if c >= '1' && c <= '9' {
					idx := int(c - '1')
					if idx < len(m.links) {
						openFile(m.links[idx])
					}
				}
			}
			return m, nil
		}

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

		case key.Matches(msg, common.Keys.Trash):
			if m.message != nil {
				id := m.message.ID
				return m, func() tea.Msg { return common.TrashFromReaderMsg{ID: id} }
			}

		case key.Matches(msg, common.Keys.Up):
			m.viewport.LineUp(7)
			return m, nil

		case key.Matches(msg, common.Keys.Down):
			m.viewport.LineDown(7)
			return m, nil

		case key.Matches(msg, common.Keys.BPreview):
			if m.message != nil {
				url := "https://mail.google.com/mail/u/0/#inbox/" + m.message.ID
				openFile(url)
			}
			return m, nil

		case key.Matches(msg, common.Keys.Quit):
			return m, tea.Quit

		default:
			if len(msg.String()) == 1 {
				c := msg.String()[0]

				// g = open link mode
				if c == 'g' && len(m.links) > 0 {
					m.goLink = true
					return m, nil
				}

				// Number keys open attachments (1-9)
				if c >= '1' && c <= '9' && len(m.message.Attachments) > 0 {
					idx := int(c - '1')
					if idx < len(m.message.Attachments) {
						return m, m.openAttachment(idx)
					}
				}

				// I = open all images
				if c == 'I' && len(m.message.Attachments) > 0 {
					return m, m.openAllImages()
				}
			}

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

	// Tab bar
	tabs := make([]string, len(folders))
	for i, f := range folders {
		if i == m.tabIdx {
			tabs[i] = common.ActiveTab.Render(f)
		} else {
			tabs[i] = common.InactiveTab.Render(f)
		}
	}
	b.WriteString(common.TabBar.Width(m.width).Render(strings.Join(tabs, "")) + "\n")

	// Header block — truncate values to terminal width to prevent line wrapping
	maxValW := m.width - 10 // "Subject: " is 9 chars + margin
	truncVal := func(s string) string {
		if rw.StringWidth(s) > maxValW {
			return rw.Truncate(s, maxValW, "...")
		}
		return s
	}
	b.WriteString(common.ReaderHeader.Render(fmt.Sprintf("From:    %s", truncVal(m.message.From))) + "\n")
	b.WriteString(common.ReaderHeader.Render(fmt.Sprintf("To:      %s", truncVal(m.message.To))) + "\n")
	b.WriteString(common.ReaderHeader.Render(fmt.Sprintf("Subject: %s", truncVal(m.message.Subject))) + "\n")
	dateStr := ""
	if !m.message.Date.IsZero() {
		dateStr = m.message.Date.Format("Mon, 02 Jan 2006 15:04:05 MST")
	}
	b.WriteString(common.ReaderHeader.Render(fmt.Sprintf("Date:    %s", dateStr)) + "\n")

	// Attachments
	if len(m.message.Attachments) > 0 {
		attStyle := common.SyncingStyle
		for i, att := range m.message.Attachments {
			size := formatSize(att.Size)
			b.WriteString(attStyle.Render(fmt.Sprintf("  [%d] %s (%s)", i+1, att.Filename, size)) + "\n")
		}
	}

	b.WriteString(strings.Repeat("─", m.width) + "\n")

	// Scrollable body
	b.WriteString(m.viewport.View())

	// Status bar
	status := " esc=back  r=reply  f=forward  d=trash  P=browser  j/k=scroll  q=quit"
	if len(m.links) > 0 {
		if m.goLink {
			status = " Press 1-9 to open link (esc=cancel)" + strings.Repeat(" ", 20) // pad to prevent flicker
		} else {
			status = " esc=back  r=reply  f=forward  P=browser  g=open link  j/k=scroll  q=quit"
		}
	}
	if len(m.message.Attachments) > 0 {
		status += "  1-9=open attachment"
		hasImages := false
		for _, att := range m.message.Attachments {
			if isImage(att.MimeType) {
				hasImages = true
				break
			}
		}
		if hasImages {
			status += "  I=open all images"
		}
	}
	status += fmt.Sprintf("  [%d%%]", int(m.viewport.ScrollPercent()*100))
	b.WriteString(common.StatusBar.Width(m.width).Render(status))

	return b.String()
}

var urlRegex = regexp.MustCompile(`https?://[^\s<>\[\]()]+`)
var mailtoRegex = regexp.MustCompile(`mailto:[^\s<>\[\]()]+`)
var htmlTagRegex = regexp.MustCompile(`<[^>]*>`)
var whitespaceRegex = regexp.MustCompile(`\n{3,}`)

// compactURL returns a short readable form: "host/path..." truncated to maxLen.
func compactURL(rawURL string, maxLen int) string {
	// Strip scheme
	s := rawURL
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	// Strip www.
	s = strings.TrimPrefix(s, "www.")
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}


// wrapText wraps lines at maxWidth on word boundaries, but leaves URLs intact.
func wrapText(text string, maxWidth int) string {
	var result strings.Builder
	for _, line := range strings.Split(text, "\n") {
		// Don't wrap lines that are URLs or start with whitespace (code/quotes)
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") ||
			strings.HasPrefix(trimmed, "mailto:") || strings.HasPrefix(line, " ") ||
			strings.HasPrefix(line, "\t") || strings.HasPrefix(line, ">") {
			result.WriteString(line + "\n")
			continue
		}
		if len(line) <= maxWidth {
			result.WriteString(line + "\n")
			continue
		}
		// Word-wrap this line
		words := strings.Fields(line)
		col := 0
		for i, word := range words {
			wordLen := len(word)
			// Never break a URL even if it exceeds maxWidth
			isURL := strings.HasPrefix(word, "http://") || strings.HasPrefix(word, "https://")
			if col+wordLen > maxWidth && col > 0 && !isURL {
				result.WriteString("\n")
				col = 0
			} else if i > 0 && col > 0 {
				result.WriteString(" ")
				col++
			}
			result.WriteString(word)
			col += wordLen
		}
		result.WriteString("\n")
	}
	return result.String()
}

func formatSize(bytes int64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}
	if bytes < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(bytes)/(1024*1024))
}
