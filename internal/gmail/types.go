package gmail

import "time"

type Label struct {
	ID   string
	Name string
}

type MessageSummary struct {
	ID      string
	From    string
	Subject string
	Snippet string
	Date    time.Time
	Unread  bool
}

type MessageList struct {
	Messages      []MessageSummary
	NextPageToken string
}

type Attachment struct {
	ID       string // Gmail attachment ID (for fetching)
	Filename string
	MimeType string
	Size     int64
}

type Message struct {
	ID          string
	ThreadID    string
	From        string
	To          string
	Subject     string
	Date        time.Time
	Body        string
	HTMLBody    string
	Unread      bool
	Attachments []Attachment
}
