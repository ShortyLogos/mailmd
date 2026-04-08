package common

import "github.com/deric/mailmd/internal/gmail"

// OpenMessageMsg is sent when a full message has been fetched and should be displayed.
type OpenMessageMsg struct{ Message *gmail.Message }

// ComposeMsg is sent to start a compose/reply/forward flow with the given template.
type ComposeMsg struct{ Template string }

// BackToInboxMsg is sent when returning from reader or composer to the inbox.
type BackToInboxMsg struct{}

// SendResultMsg is sent when the compose flow completes (Err is nil on success).
type SendResultMsg struct{ Err error }

// StatusMsg carries a status-bar text update.
type StatusMsg struct{ Text string }

// FetchMessageMsg is sent when the inbox wants to open a message by ID.
// The app shell handles this by fetching the full message, then sends OpenMessageMsg.
type FetchMessageMsg struct{ ID string }
