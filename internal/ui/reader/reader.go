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
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/deric/mailmd/internal/gmail"
	"github.com/deric/mailmd/internal/markdown"
	"github.com/deric/mailmd/internal/ui/common"
	rw "github.com/mattn/go-runewidth"
	nethtml "golang.org/x/net/html"
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
	accountName     string   // current account name (for tab bar)
	accountEmail    string   // current account email (for tab bar)
	links           []string // extracted URLs, indexed from 1
	linkJumping     bool     // true when typing a link number
	linkJumpInput   string   // accumulated digits for link number
	renderedContent string   // cached rendered body for resize
}

// New creates a new reader model for the given message.
// bodyRenderedMsg carries the result of async body rendering.
type bodyRenderedMsg struct {
	content string
	links   []string
}

func New(ctx context.Context, client gmail.Client, msg *gmail.Message, width, height, tabIdx int, accountName, accountEmail string) Model {
	m := Model{
		ctx:          ctx,
		client:       client,
		message:      msg,
		width:        width,
		height:       height,
		tabIdx:       tabIdx,
		accountName:  accountName,
		accountEmail: accountEmail,
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
	width := m.width
	return func() tea.Msg {
		body := msg.Body
		// Prefer HTML body when it contains tables, since Gmail's
		// auto-generated plain text flattens table structure.
		if msg.HTMLBody != "" && strings.Contains(strings.ToLower(msg.HTMLBody), "<table") {
			body = stripHTML(msg.HTMLBody, width)
		} else if body == "" && msg.HTMLBody != "" {
			body = stripHTML(msg.HTMLBody, width)
		}
		if body == "" {
			body = "(No message body)"
		}

		// Extract and number URLs (http/https)
		var links []string
		body = urlRegex.ReplaceAllStringFunc(body, func(rawURL string) string {
			links = append(links, rawURL)
			label := compactURL(rawURL, 50)
			return fmt.Sprintf("[%d] %s", len(links), label)
		})

		// Strip mailto: prefix, leave plain email address (colorized later)
		body = mailtoRegex.ReplaceAllStringFunc(body, func(rawURL string) string {
			return strings.TrimPrefix(rawURL, "mailto:")
		})

		// Deduplicate "email@addr<email@addr>" → "email@addr"
		body = dupEmailRegex.ReplaceAllString(body, "$1")

		// Wrap text at terminal width
		body = wrapText(body, width)

		// Lightweight styling — colorize link refs and mailto refs
		rendered := renderPlainEmail(body)

		return bodyRenderedMsg{content: rendered, links: links}
	}
}

// renderPlainEmail applies minimal ANSI styling to plain text email body.
// Colors link references [N: ...] and email addresses, bolds headings.
func renderPlainEmail(body string) string {
	linkStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#38BDF8")).Bold(true) // sky blue
	mailStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#10B981"))              // green
	headingStyle := lipgloss.NewStyle().Bold(true)

	var result strings.Builder
	for _, line := range strings.Split(body, "\n") {
		// Bold headings (tagged with headingMarker by stripHTML)
		if strings.HasPrefix(line, headingMarker) {
			result.WriteString(headingStyle.Render(strings.TrimPrefix(line, headingMarker)) + "\n")
			continue
		}
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

var linkRefRegex = regexp.MustCompile(`\[\d+\] [^\s]+`)

// headingMarker is a non-printable prefix used to tag heading lines
// so renderPlainEmail can apply bold styling.
const headingMarker = "\x1f"

var headingRegex = regexp.MustCompile(`(?is)<h[1-6][^>]*>(.*?)</h[1-6]>`)

// stripHTML converts HTML to readable plain text by removing tags,
// converting block elements to newlines, and decoding entities.
// maxWidth is used to constrain table column widths.
func stripHTML(s string, maxWidth int) string {
	// Remove style and script blocks first — before table extraction,
	// so CSS inside table cells doesn't leak as text content.
	s = styleBlockRegex.ReplaceAllString(s, "")
	s = scriptBlockRegex.ReplaceAllString(s, "")
	// Extract HTML tables, render them, and replace with placeholders.
	// This keeps rendered table text out of the tag-stripping and
	// entity-decoding passes so column widths stay correct.
	var renderedTables []string
	s = tableRegex.ReplaceAllStringFunc(s, func(tableHTML string) string {
		rows := parseHTMLTable(tableHTML)
		if len(rows) == 0 {
			return ""
		}
		idx := len(renderedTables)
		renderedTables = append(renderedTables, renderTextTable(rows, maxWidth))
		return fmt.Sprintf("\x00T%d\x00", idx)
	})
	// Convert headings to marked plain text before general tag stripping
	s = headingRegex.ReplaceAllStringFunc(s, func(match string) string {
		inner := headingRegex.FindStringSubmatch(match)[1]
		inner = htmlTagRegex.ReplaceAllString(inner, "")
		inner = strings.TrimSpace(inner)
		return "\n" + headingMarker + inner + "\n"
	})
	// Replace block-level tags with newlines
	for _, tag := range []string{"<br", "<BR", "<p", "<P", "<div", "<DIV", "<tr", "<TR", "<li", "<LI"} {
		s = strings.ReplaceAll(s, tag, "\n"+tag)
	}
	// Second pass for style/script blocks that were inside extracted tables
	// or generated by block-tag newline insertion
	s = styleBlockRegex.ReplaceAllString(s, "")
	s = scriptBlockRegex.ReplaceAllString(s, "")
	// Strip all HTML tags
	s = htmlTagRegex.ReplaceAllString(s, "")
	// Decode HTML entities
	s = html.UnescapeString(s)
	// Replace non-breaking spaces with regular spaces (common in HTML emails)
	s = strings.ReplaceAll(s, "\u00a0", " ")
	// Restore rendered tables
	for i, table := range renderedTables {
		s = strings.ReplaceAll(s, fmt.Sprintf("\x00T%d\x00", i), "\n"+table)
	}
	// Strip control characters that corrupt terminal rendering
	// (carriage returns from \r\n emails, escape sequences, zero-width chars, etc.)
	// Must run after table restoration since placeholders use \x00.
	s = stripControlChars(s)
	// Collapse blank lines: any run of 2+ lines that are empty or
	// whitespace-only becomes a single blank line.
	s = collapseBlankLines(s)
	return strings.TrimSpace(s)
}

var tableRegex = regexp.MustCompile(`(?is)<table[^>]*>.*?</table>`)

// parseHTMLTable extracts rows and cells from an HTML table fragment.
// The HTML parser decodes entities, so cell text is already plain text.
func parseHTMLTable(tableHTML string) [][]string {
	doc, err := nethtml.Parse(strings.NewReader(tableHTML))
	if err != nil {
		return nil
	}
	var rows [][]string
	var walk func(*nethtml.Node)
	walk = func(n *nethtml.Node) {
		if n.Type == nethtml.ElementNode && n.Data == "tr" {
			var cells []string
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == nethtml.ElementNode && (c.Data == "td" || c.Data == "th") {
					cells = append(cells, nodeText(c))
				}
			}
			// Skip rows where all cells are empty (layout spacers, image-only rows)
			hasContent := false
			for _, c := range cells {
				if strings.TrimSpace(c) != "" {
					hasContent = true
					break
				}
			}
			if hasContent {
				rows = append(rows, cells)
			}
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return rows
}

// nodeText recursively extracts visible text from an HTML node,
// skipping style, script, and other non-visible elements.
func nodeText(n *nethtml.Node) string {
	if n.Type == nethtml.TextNode {
		return n.Data
	}
	if n.Type == nethtml.ElementNode {
		switch n.Data {
		case "style", "script", "link", "meta":
			return ""
		case "br":
			return " "
		}
	}
	var sb strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		sb.WriteString(nodeText(c))
	}
	return strings.TrimSpace(sb.String())
}

// renderTextTable formats rows as a pipe-delimited text table with a
// separator line after the first row (assumed header).
// When maxWidth > 0 and the table would overflow, columns are shrunk
// proportionally and cell content wraps across multiple display lines.
func renderTextTable(rows [][]string, maxWidth int) string {
	// Normalize column count
	maxCols := 0
	for _, row := range rows {
		if len(row) > maxCols {
			maxCols = len(row)
		}
	}
	if maxCols == 0 {
		return ""
	}
	for i := range rows {
		for len(rows[i]) < maxCols {
			rows[i] = append(rows[i], "")
		}
	}

	// Calculate natural column widths using display width
	natural := make([]int, maxCols)
	for _, row := range rows {
		for j, cell := range row {
			if w := rw.StringWidth(cell); w > natural[j] {
				natural[j] = w
			}
		}
	}

	// Minimum column width = widest single word (can't break words)
	minWidths := make([]int, maxCols)
	for _, row := range rows {
		for j, cell := range row {
			for _, word := range strings.Fields(cell) {
				if w := rw.StringWidth(word); w > minWidths[j] {
					minWidths[j] = w
				}
			}
		}
	}
	for j := range minWidths {
		if minWidths[j] < 3 {
			minWidths[j] = 3
		}
	}

	// Per-column overhead: "| " before each column + trailing "|" = 3*maxCols + 1
	overhead := maxCols*3 + 1
	totalNatural := 0
	for _, w := range natural {
		totalNatural += w
	}

	widths := make([]int, maxCols)
	copy(widths, natural)

	if maxWidth > 0 && totalNatural+overhead > maxWidth {
		available := maxWidth - overhead
		if available < maxCols {
			available = maxCols
		}
		// Distribute space proportionally, respecting minimum word widths
		for j := range widths {
			widths[j] = natural[j] * available / totalNatural
			if widths[j] < minWidths[j] {
				widths[j] = minWidths[j]
			}
		}
		// Distribute rounding remainder to columns with the largest deficit
		used := 0
		for _, w := range widths {
			used += w
		}
		for used < available {
			best := -1
			bestDeficit := 0
			for j := range widths {
				if deficit := natural[j] - widths[j]; deficit > bestDeficit {
					best = j
					bestDeficit = deficit
				}
			}
			if best < 0 {
				break
			}
			widths[best]++
			used++
		}
	}

	// Render rows, wrapping cells that exceed their column width
	var sb strings.Builder
	for i, row := range rows {
		wrapped := make([][]string, maxCols)
		maxLines := 1
		for j, cell := range row {
			wrapped[j] = wrapCell(cell, widths[j])
			if len(wrapped[j]) > maxLines {
				maxLines = len(wrapped[j])
			}
		}
		for line := 0; line < maxLines; line++ {
			for j := range widths {
				sb.WriteString("| ")
				content := ""
				if line < len(wrapped[j]) {
					content = wrapped[j][line]
				}
				sb.WriteString(content)
				pad := widths[j] - rw.StringWidth(content)
				if pad > 0 {
					sb.WriteString(strings.Repeat(" ", pad))
				}
				sb.WriteString(" ")
			}
			sb.WriteString("|\n")
		}
		if i == 0 {
			for j := range widths {
				sb.WriteString("| ")
				sb.WriteString(strings.Repeat("-", widths[j]))
				sb.WriteString(" ")
			}
			sb.WriteString("|\n")
		}
	}
	return sb.String()
}

// wrapCell splits text into lines that fit within maxWidth display columns.
func wrapCell(text string, maxWidth int) []string {
	if rw.StringWidth(text) <= maxWidth {
		return []string{text}
	}
	var lines []string
	words := strings.Fields(text)
	var cur strings.Builder
	col := 0
	for _, word := range words {
		wLen := rw.StringWidth(word)
		if col > 0 && col+1+wLen > maxWidth {
			lines = append(lines, cur.String())
			cur.Reset()
			col = 0
		}
		if col > 0 {
			cur.WriteString(" ")
			col++
		}
		cur.WriteString(word)
		col += wLen
	}
	if cur.Len() > 0 {
		lines = append(lines, cur.String())
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
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
		// Number input mode: digits accumulate, then l=link / a=attachment
		if m.linkJumping {
			switch {
			case key.Matches(msg, key.NewBinding(key.WithKeys("l"))):
				n, _ := strconv.Atoi(m.linkJumpInput)
				m.linkJumping = false
				m.linkJumpInput = ""
				if n > 0 && n <= len(m.links) {
					openFile(m.links[n-1])
				}
				return m, nil
			case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
				n, _ := strconv.Atoi(m.linkJumpInput)
				m.linkJumping = false
				m.linkJumpInput = ""
				if n > 0 && n <= len(m.message.Attachments) {
					return m, m.openAttachment(n - 1)
				}
				return m, nil
			case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
				m.linkJumping = false
				m.linkJumpInput = ""
				return m, nil
			case key.Matches(msg, key.NewBinding(key.WithKeys("backspace"))):
				if len(m.linkJumpInput) > 0 {
					m.linkJumpInput = m.linkJumpInput[:len(m.linkJumpInput)-1]
				}
				if len(m.linkJumpInput) == 0 {
					m.linkJumping = false
				}
				return m, nil
			default:
				if len(msg.String()) == 1 && msg.String()[0] >= '0' && msg.String()[0] <= '9' {
					m.linkJumpInput += msg.String()
					return m, nil
				}
				// Non-digit/non-action cancels
				m.linkJumping = false
				m.linkJumpInput = ""
			}
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

				// Digit starts number input mode (N+l=link, N+a=attachment)
				if c >= '1' && c <= '9' {
					m.linkJumping = true
					m.linkJumpInput = string(c)
					return m, nil
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
	tabContent := strings.Join(tabs, "")
	acctDisplay := m.accountEmail
	if acctDisplay == "" {
		acctDisplay = m.accountName
	}
	if acctDisplay != "" {
		acctLabel := lipgloss.NewStyle().Foreground(common.Muted).Render(acctDisplay + "  ")
		tabsW := lipgloss.Width(tabContent)
		acctW := lipgloss.Width(acctLabel)
		gap := m.width - tabsW - acctW - 2
		if gap > 0 {
			tabContent += strings.Repeat(" ", gap) + acctLabel
		}
	}
	b.WriteString(common.TabBar.Width(m.width).Render(tabContent) + "\n")

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
	status := " esc=back  r=reply  f=forward  d=trash  P=browser  j/k=scroll  K=keys  q=quit"
	if m.linkJumping {
		hints := ""
		if len(m.links) > 0 {
			hints += " l=link"
		}
		if len(m.message.Attachments) > 0 {
			hints += " enter=attach"
		}
		status = fmt.Sprintf(" %s_%s  esc=cancel", m.linkJumpInput, hints) + strings.Repeat(" ", 20)
	} else {
		extras := ""
		if len(m.links) > 0 {
			extras += "  N+l=link"
		}
		if len(m.message.Attachments) > 0 {
			extras += "  N+enter=attach  I=images"
		}
		status = " esc=back  r=reply  f=forward  d=trash  P=browser" + extras + "  j/k=scroll  K=keys  q=quit"
	}
	status += fmt.Sprintf("  [%d%%]", int(m.viewport.ScrollPercent()*100))
	b.WriteString(common.StatusBar.Width(m.width).Render(status))

	return b.String()
}

var urlRegex = regexp.MustCompile(`https?://[^\s<>\[\]()]+`)
var mailtoRegex = regexp.MustCompile(`mailto:[^\s<>\[\]()]+`)
var dupEmailRegex = regexp.MustCompile(`([a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,})<[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}>`)
var htmlTagRegex = regexp.MustCompile(`<[^>]*>`)
var styleBlockRegex = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
var scriptBlockRegex = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
var junkLineRegex = regexp.MustCompile(`^\d{1,4}$`)

// stripControlChars removes control characters that corrupt terminal
// rendering (carriage returns from \r\n emails, escape sequences,
// zero-width Unicode, etc.). Preserves \n, \t, and the heading marker \x1f.
func stripControlChars(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t' || r == rune(headingMarker[0]):
			b.WriteRune(r)
		case r < 0x20: // C0 control chars (\r, \x1b, etc.)
			// skip
		case r == 0x7f: // DEL
			// skip
		case r >= 0x200b && r <= 0x200f: // zero-width and bidi marks
			// skip
		case r >= 0x202a && r <= 0x202e: // bidi embedding/override
			// skip
		case r == 0xfeff: // BOM / zero-width no-break space
			// skip
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// collapseBlankLines reduces any run of consecutive blank (whitespace-only)
// lines to a single empty line, and strips junk lines (lone numbers from
// HTML layout tables). Handles deeply nested HTML emails that produce huge
// whitespace gaps.
func collapseBlankLines(s string) string {
	lines := strings.Split(s, "\n")
	var out []string
	blanks := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			blanks++
			if blanks <= 1 {
				out = append(out, "")
			}
		} else if junkLineRegex.MatchString(trimmed) {
			// Skip lone numbers (layout artifacts from HTML tables)
		} else {
			blanks = 0
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

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


// wrapText wraps lines at maxWidth on word boundaries, but leaves URLs
// and table lines intact. Uses display width for proper Unicode handling.
func wrapText(text string, maxWidth int) string {
	var result strings.Builder
	for _, line := range strings.Split(text, "\n") {
		// Don't wrap lines that are URLs, tables, or start with whitespace (code/quotes)
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") ||
			strings.HasPrefix(trimmed, "mailto:") || strings.HasPrefix(trimmed, "|") ||
			strings.HasPrefix(line, " ") ||
			strings.HasPrefix(line, "\t") || strings.HasPrefix(line, ">") {
			result.WriteString(line + "\n")
			continue
		}
		if rw.StringWidth(line) <= maxWidth {
			result.WriteString(line + "\n")
			continue
		}
		// Word-wrap this line
		words := strings.Fields(line)
		col := 0
		for i, word := range words {
			wordLen := rw.StringWidth(word)
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
