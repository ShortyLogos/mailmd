package common

import "github.com/deric/mailmd/internal/gmail"

// OpenMessageMsg is sent when a full message has been fetched and should be displayed.
type OpenMessageMsg struct{ Message *gmail.Message }

// ComposeMsg is sent to start a compose/reply/forward flow with the given template.
type ComposeMsg struct {
	To          []string            // pre-populated recipients
	CC          []string            // pre-populated CC
	Subject     string              // pre-populated subject
	Body        string              // pre-populated body (quoted text for replies)
	ThreadID    string              // Gmail thread ID for reply threading
	InReplyTo   string              // RFC 2822 Message-ID for In-Reply-To header
	DraftID     string              // original draft ID for edit
	Title       string              // dialog title (e.g. "Compose", "Reply", "Forward")
	Attachments []gmail.AttachmentFile
}

// BackToInboxMsg is sent when returning from reader or composer to the inbox.
type BackToInboxMsg struct{}

// SendResultMsg is sent when the compose flow completes (Err is nil on success).
type SendResultMsg struct{ Err error }

// QueueSendMsg is emitted by the composer when the user presses send.
// The App queues the message with an undo countdown instead of sending immediately.
type QueueSendMsg struct {
	To          string
	CC          string
	Subject     string
	HTMLBody    string
	PlainBody   string
	ThreadID    string
	InReplyTo   string
	DraftID     string
	Attachments []gmail.AttachmentFile
}

// UndoSendTickMsg is the countdown tick for the undo-send timer.
type UndoSendTickMsg struct{}

// UndoSendMsg is sent when the user presses U to cancel a queued send.
type UndoSendMsg struct{}

// StatusMsg carries a status-bar text update.
type StatusMsg struct{ Text string }

// FetchMessageMsg is sent when the inbox wants to open a message by ID.
// The app shell handles this by fetching the full message, then sends OpenMessageMsg.
type FetchMessageMsg struct{ ID string }

// FetchAndReplyMsg is sent when the inbox wants to reply to a message directly.
// The app shell fetches the full message, then opens the compose flow with a reply template.
type FetchAndReplyMsg struct{ ID string }

// TrashFromReaderMsg is sent when the user trashes/deletes a message from the reader view.
type TrashFromReaderMsg struct{ ID string }

// EditDraftMsg is sent when the user wants to edit a draft message.
type EditDraftMsg struct{ ID string }

// SaveDraftMsg is sent when the compose flow is canceled with content worth saving.
type SaveDraftMsg struct {
	To          string
	CC          string
	Subject     string
	Body        string
	Attachments []gmail.AttachmentFile
}

// DraftSavedMsg is the result of a draft save attempt.
type DraftSavedMsg struct{ Err error }

// EditHeadersMsg is sent from the composer preview to re-open the compose dialog
// so the user can modify recipients and subject.
type EditHeadersMsg struct {
	To          []string
	CC          []string
	Subject     string
	Body        string
	ThreadID    string
	InReplyTo   string
	DraftID     string
	Attachments []gmail.AttachmentFile
}
