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
	ctx      context.Context
	client   gmail.Client
	editor   string
	template string
	phase    phase
	data     *markdown.ComposeData
	viewport viewport.Model
	width    int
	height   int
	ready    bool
	err      string
	status   string
	tmpFile  string
	draftID  string // if editing a draft, the original message ID to trash after send
}

// New creates a new composer model.
func New(ctx context.Context, client gmail.Client, editor, template string, width, height int) Model {
	return Model{
		ctx:      ctx,
		client:   client,
		editor:   editor,
		template: template,
		phase:    phaseEditing,
		width:    width,
		height:   height,
	}
}

// NewDraftEdit creates a composer for editing an existing draft.
func NewDraftEdit(ctx context.Context, client gmail.Client, editor, template string, width, height int, draftID string) Model {
	m := New(ctx, client, editor, template, width, height)
	m.draftID = draftID
	return m
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

		data, err := markdown.ParseCompose(msg.content)
		if err != nil {
			m.err = "Parse error: " + err.Error()
			// Allow re-edit
			m.phase = phasePreview
			m.initViewport("# Parse Error\n\n" + err.Error() + "\n\nPress **e** to edit again or **esc** to cancel.")
			return m, nil
		}

		m.data = data
		m.phase = phasePreview
		m.err = ""
		m.status = fmt.Sprintf("To: %s | Subject: %s", data.To, data.Subject)
		m.initViewport(data.Body)

	case tea.KeyMsg:
		if m.phase == phasePreview {
			switch {
			case key.Matches(msg, common.Keys.Send):
				if m.data != nil {
					return m, m.sendMessage()
				}

			case key.Matches(msg, common.Keys.Edit):
				if m.data != nil {
					// Re-open editor with current content
					var sb strings.Builder
					sb.WriteString("---\n")
					sb.WriteString("to: " + m.data.To + "\n")
					sb.WriteString("subject: " + m.data.Subject + "\n")
					sb.WriteString("---\n\n")
					sb.WriteString(m.data.Body)
					content := sb.String()
					m.phase = phaseEditing
					return m, m.launchEditor(content)
				}

			case key.Matches(msg, common.Keys.BPreview):
				if m.data != nil {
					return m, m.openBrowserPreview()
				}

			case key.Matches(msg, common.Keys.Back):
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

func (m Model) sendMessage() tea.Cmd {
	data := m.data
	draftID := m.draftID
	return func() tea.Msg {
		htmlBody, err := markdown.Convert(data.Body)
		if err != nil {
			return common.SendResultMsg{Err: fmt.Errorf("markdown conversion: %w", err)}
		}
		plainBody := markdown.ConvertPlain(data.Body)
		if err := m.client.SendMessage(m.ctx, data.To, data.Subject, htmlBody, plainBody); err != nil {
			return common.SendResultMsg{Err: fmt.Errorf("send failed: %w", err)}
		}
		// Trash the original draft after successful send
		if draftID != "" {
			m.client.TrashMessage(m.ctx, draftID)
		}
		return common.SendResultMsg{Err: nil}
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
	b.WriteString(common.StatusBar.Width(m.width).Render(" y=send  e=edit  P=browser preview  esc=cancel  q=quit"))

	return b.String()
}
