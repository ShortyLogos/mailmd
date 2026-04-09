package reader

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/deric/mailmd/internal/gmail"
	"github.com/deric/mailmd/internal/markdown"
	"github.com/deric/mailmd/internal/ui/common"
)

// attachmentOpenedMsg signals an attachment was saved and opened.
type attachmentOpenedMsg struct{ err error }

// Model is the reader Bubble Tea model.
type Model struct {
	ctx      context.Context
	client   gmail.Client
	message  *gmail.Message
	viewport viewport.Model
	width    int
	height   int
	ready    bool
}

// New creates a new reader model for the given message.
func New(ctx context.Context, client gmail.Client, msg *gmail.Message, width, height int) Model {
	m := Model{
		ctx:     ctx,
		client:  client,
		message: msg,
		width:   width,
		height:  height,
	}
	m.initViewport()
	return m
}

func (m *Model) initViewport() {
	headerHeight := 5 // From, To, Subject, Date, separator
	if len(m.message.Attachments) > 0 {
		headerHeight += 1 + len(m.message.Attachments) // blank line + one per attachment
	}
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

	// Convert bare URLs to markdown links with short display text
	// Glamour renders these as OSC 8 clickable hyperlinks in supported terminals
	body = shortenURLs(body)

	// Wrap text at 80 chars but preserve URLs on single lines
	body = wrapText(body, 80)

	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(0), // we handle wrapping ourselves
	)
	if err != nil {
		return markdown.ConvertPlain(body)
	}
	rendered, err := r.Render(body)
	if err != nil {
		return markdown.ConvertPlain(body)
	}
	return rendered
}

// Init is a no-op for reader.
func (m Model) Init() tea.Cmd {
	return nil
}

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
		m.initViewport()

	case attachmentOpenedMsg:
		// Could show status, for now just ignore errors silently
		return m, nil

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

		case key.Matches(msg, common.Keys.Up):
			m.viewport.LineUp(7)
			return m, nil

		case key.Matches(msg, common.Keys.Down):
			m.viewport.LineDown(7)
			return m, nil

		case key.Matches(msg, common.Keys.Quit):
			return m, tea.Quit

		default:
			if len(m.message.Attachments) > 0 && len(msg.String()) == 1 {
				c := msg.String()[0]
				// Number keys open individual attachments (1-9)
				if c >= '1' && c <= '9' {
					idx := int(c - '1')
					if idx < len(m.message.Attachments) {
						return m, m.openAttachment(idx)
					}
				}
				// I = open all images
				if c == 'I' {
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

	// Header block
	b.WriteString(common.ReaderHeader.Render(fmt.Sprintf("From:    %s", m.message.From)) + "\n")
	b.WriteString(common.ReaderHeader.Render(fmt.Sprintf("To:      %s", m.message.To)) + "\n")
	b.WriteString(common.ReaderHeader.Render(fmt.Sprintf("Subject: %s", m.message.Subject)) + "\n")
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
	b.WriteString(m.viewport.View() + "\n")

	// Status bar
	status := " esc=back  r=reply  f=forward  j/k=scroll  q=quit"
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

// shortenURLs converts bare URLs to markdown links with truncated display text.
// e.g. "https://example.com/very/long/path?q=1" → "[example.com/very/long/...](https://example.com/very/long/path?q=1)"
// Glamour renders these as OSC 8 clickable hyperlinks in terminals that support it (Ghostty, Kitty, iTerm2).
func shortenURLs(text string) string {
	return urlRegex.ReplaceAllStringFunc(text, func(rawURL string) string {
		display := shortenURL(rawURL, 50)
		// Don't convert if already inside a markdown link
		return "[" + display + "](" + rawURL + ")"
	})
}

func shortenURL(rawURL string, maxLen int) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		if len(rawURL) > maxLen {
			return rawURL[:maxLen-3] + "..."
		}
		return rawURL
	}
	// Show host + truncated path
	display := u.Host + u.Path
	if u.RawQuery != "" {
		display += "?" + u.RawQuery
	}
	if len(display) > maxLen {
		return display[:maxLen-3] + "..."
	}
	return display
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
