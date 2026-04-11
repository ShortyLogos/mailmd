package composer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/deric/mailmd/internal/gmail"
	"github.com/deric/mailmd/internal/markdown"
	"github.com/deric/mailmd/internal/ui/common"
)

type phase int

const (
	phaseEditing phase = iota
	phasePreview
)

// editorDoneMsg is sent when the external editor process exits.
type editorDoneMsg struct {
	content string
	err     error
}

// Model is the composer Bubble Tea model.
type Model struct {
	ctx       context.Context
	client    gmail.Client
	editor    string
	template  string
	phase     phase
	data      *markdown.ComposeData
	viewport  viewport.Model
	width     int
	height    int
	ready     bool
	err       string
	status    string
	tmpFile   string
	draftID   string // if editing a draft, the original message ID to trash after send
	threadID  string // Gmail thread ID for reply threading
	inReplyTo string // RFC 2822 Message-ID for In-Reply-To header
	// Structured metadata (set by compose dialog, bypasses frontmatter)
	metaTo      string
	metaCC      string
	metaBCC     string
	metaSubject string
	attachments []gmail.AttachmentFile
}

// New creates a new composer model.
func New(ctx context.Context, client gmail.Client, editor, template string, width, height int, threadID, inReplyTo string) Model {
	return Model{
		ctx:       ctx,
		client:    client,
		editor:    editor,
		template:  template,
		phase:     phaseEditing,
		width:     width,
		height:    height,
		threadID:  threadID,
		inReplyTo: inReplyTo,
	}
}

// NewDraftEdit creates a composer for editing an existing draft.
func NewDraftEdit(ctx context.Context, client gmail.Client, editor, template string, width, height int, draftID string) Model {
	m := New(ctx, client, editor, template, width, height, "", "")
	m.draftID = draftID
	return m
}

// NewWithMetadata creates a composer with pre-set metadata from the compose dialog.
// The editor opens with body content only (no frontmatter).
func NewWithMetadata(ctx context.Context, client gmail.Client, editor, body string, width, height int, to, cc, bcc, subject, threadID, inReplyTo, draftID string, attachments []gmail.AttachmentFile) Model {
	return Model{
		ctx:         ctx,
		client:      client,
		editor:      editor,
		template:    body,
		phase:       phaseEditing,
		width:       width,
		height:      height,
		threadID:    threadID,
		inReplyTo:   inReplyTo,
		draftID:     draftID,
		metaTo:      to,
		metaCC:      cc,
		metaBCC:     bcc,
		metaSubject: subject,
		attachments: attachments,
	}
}

// Data returns the current compose data (may be nil before editor completes).
func (m Model) Data() *markdown.ComposeData {
	return m.data
}

// Init launches the editor immediately.
func (m Model) Init() tea.Cmd {
	return m.launchEditor(m.template)
}

func (m Model) launchEditor(content string) tea.Cmd {
	// Write template to a temp file
	f, err := os.CreateTemp("", "mailmd-*.md")
	if err != nil {
		return func() tea.Msg { return editorDoneMsg{err: err} }
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		os.Remove(f.Name())
		return func() tea.Msg { return editorDoneMsg{err: err} }
	}
	f.Close()

	tmpPath := f.Name()

	editorCmd := m.editor
	if editorCmd == "" {
		editorCmd = os.Getenv("EDITOR")
	}
	if editorCmd == "" {
		editorCmd = "vi"
	}

	cmd := exec.Command(editorCmd, tmpPath)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			os.Remove(tmpPath)
			return editorDoneMsg{err: err}
		}
		data, readErr := os.ReadFile(tmpPath)
		os.Remove(tmpPath)
		if readErr != nil {
			return editorDoneMsg{err: readErr}
		}
		return editorDoneMsg{content: string(data)}
	})
}

func (m *Model) initViewport(content string) {
	statusHeight := 3
	vpHeight := m.height - statusHeight
	if vpHeight < 1 {
		vpHeight = 1
	}
	m.viewport = viewport.New(m.width, vpHeight)

	rendered, err := glamour.Render(content, "dark")
	if err != nil {
		rendered = content
	}
	m.viewport.SetContent(rendered)
	m.ready = true
}

// Update handles editor lifecycle and key events.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.phase == phasePreview && m.ready {
			body := ""
			if m.data != nil {
				body = m.data.Body
			}
			m.initViewport(body)
		}

	case editorDoneMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, func() tea.Msg {
				return common.SendResultMsg{Err: fmt.Errorf("editor error: %w", msg.err)}
			}
		}

		if m.metaTo != "" {
			// Metadata mode: body-only editor, skip frontmatter parsing
			m.data = &markdown.ComposeData{
				To:      m.metaTo,
				CC:      m.metaCC,
				BCC:     m.metaBCC,
				Subject: m.metaSubject,
				Body:    msg.content,
			}
		} else {
			data, err := markdown.ParseCompose(msg.content)
			if err != nil {
				m.err = "Parse error: " + err.Error()
				m.phase = phasePreview
				m.initViewport("# Parse Error\n\n" + err.Error() + "\n\nPress **e** to edit again or **esc** to cancel.")
				return m, nil
			}
			m.data = data
		}

		m.phase = phasePreview
		m.err = ""
		status := fmt.Sprintf("To: %s | Subject: %s", m.data.To, m.data.Subject)
		if m.data.CC != "" {
			status = fmt.Sprintf("To: %s | CC: %s | Subject: %s", m.data.To, m.data.CC, m.data.Subject)
		}
		if m.data.BCC != "" {
			status += fmt.Sprintf(" | BCC: %s", m.data.BCC)
		}
		if len(m.attachments) > 0 {
			status += fmt.Sprintf(" | %d attachment(s)", len(m.attachments))
		}
		m.status = status
		m.initViewport(m.data.Body)

	case tea.KeyMsg:
		if m.phase == phasePreview {
			switch {
			case key.Matches(msg, common.Keys.Send):
				if m.data != nil {
					return m, m.sendMessage()
				}

			case key.Matches(msg, common.Keys.Edit):
				if m.data != nil {
					var content string
					if m.metaTo != "" {
						// Metadata mode: body only
						content = m.data.Body
					} else {
						// Legacy mode: reconstruct frontmatter
						var sb strings.Builder
						sb.WriteString("---\n")
						sb.WriteString("to: " + m.data.To + "\n")
						sb.WriteString("subject: " + m.data.Subject + "\n")
						sb.WriteString("---\n\n")
						sb.WriteString(m.data.Body)
						content = sb.String()
					}
					m.phase = phaseEditing
					return m, m.launchEditor(content)
				}

			case key.Matches(msg, key.NewBinding(key.WithKeys("H"))):
				if m.data != nil {
					to := splitList(m.data.To)
					cc := splitList(m.data.CC)
					bcc := splitList(m.data.BCC)
					data := m.data
					draftID := m.draftID
					threadID := m.threadID
					inReplyTo := m.inReplyTo
					atts := m.attachments
					return m, func() tea.Msg {
						return common.EditHeadersMsg{
							To: to, CC: cc, BCC: bcc,
							Subject:     data.Subject,
							Body:        data.Body,
							ThreadID:    threadID,
							InReplyTo:   inReplyTo,
							DraftID:     draftID,
							Attachments: atts,
						}
					}
				}

			case key.Matches(msg, common.Keys.BPreview):
				if m.data != nil {
					return m, m.openBrowserPreview()
				}

			case key.Matches(msg, common.Keys.Back):
				if m.data != nil && (m.data.To != "" || m.data.Body != "") {
					data := m.data
					atts := m.attachments
					return m, func() tea.Msg {
						return common.SaveDraftMsg{
							To: data.To, CC: data.CC, BCC: data.BCC,
							Subject: data.Subject, Body: data.Body,
							Attachments: atts,
						}
					}
				}
				return m, func() tea.Msg { return common.BackToInboxMsg{} }

			case key.Matches(msg, common.Keys.Quit):
				return m, tea.Quit

			default:
				var cmd tea.Cmd
				m.viewport, cmd = m.viewport.Update(msg)
				return m, cmd
			}
		}
	}
	return m, nil
}

// Attachments returns the list of attachment file paths.
func (m Model) Attachments() []gmail.AttachmentFile {
	return m.attachments
}

func (m Model) sendMessage() tea.Cmd {
	data := m.data
	draftID := m.draftID
	threadID := m.threadID
	inReplyTo := m.inReplyTo
	atts := m.attachments
	return func() tea.Msg {
		htmlBody, err := markdown.Convert(data.Body)
		if err != nil {
			return common.SendResultMsg{Err: fmt.Errorf("markdown conversion: %w", err)}
		}
		plainBody := markdown.ConvertPlain(data.Body)
		return common.QueueSendMsg{
			To:          data.To,
			CC:          data.CC,
			BCC:         data.BCC,
			Subject:     data.Subject,
			HTMLBody:    htmlBody,
			PlainBody:   plainBody,
			ThreadID:    threadID,
			InReplyTo:   inReplyTo,
			DraftID:     draftID,
			Attachments: atts,
		}
	}
}

func (m Model) openBrowserPreview() tea.Cmd {
	data := m.data
	return func() tea.Msg {
		htmlBody, err := markdown.Convert(data.Body)
		if err != nil {
			return common.StatusMsg{Text: "Preview error: " + err.Error()}
		}
		srv, err := markdown.NewPreviewServer(htmlBody)
		if err != nil {
			return common.StatusMsg{Text: "Preview server error: " + err.Error()}
		}
		url := srv.URL()
		openBrowser(url)
		return common.StatusMsg{Text: "Opened browser preview: " + url}
	}
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch {
	case commandExists("open"):
		cmd = exec.Command("open", url)
	case commandExists("xdg-open"):
		cmd = exec.Command("xdg-open", url)
	default:
		return
	}
	cmd.Start()
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func splitList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var result []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// View renders the composer preview screen.
func (m Model) View() string {
	if m.phase == phaseEditing {
		return "Opening editor..."
	}

	if !m.ready {
		return "Loading preview..."
	}

	var b strings.Builder

	if m.err != "" {
		b.WriteString(common.ErrorStyle.Render(m.err) + "\n")
	}

	b.WriteString(m.viewport.View() + "\n")

	statusLine := m.status
	if statusLine == "" {
		statusLine = "Preview"
	}
	b.WriteString(common.StatusBar.Width(m.width).Render(statusLine) + "\n")
	b.WriteString(common.StatusBar.Width(m.width).Render(" y=send  e=edit body  H=edit headers  P=browser preview  esc=cancel"))

	return b.String()
}
