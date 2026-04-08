package gmail

import (
	"testing"

	gapi "google.golang.org/api/gmail/v1"
)

func TestParseMessageSummary(t *testing.T) {
	msg := &gapi.Message{
		Id:      "msg-1",
		Snippet: "Hey, are we still...",
		Payload: &gapi.MessagePart{
			Headers: []*gapi.MessagePartHeader{
				{Name: "From", Value: "Alice <alice@example.com>"},
				{Name: "Subject", Value: "Meeting tomorrow"},
				{Name: "Date", Value: "Mon, 8 Apr 2026 10:00:00 -0400"},
			},
		},
		LabelIds: []string{"INBOX", "UNREAD"},
	}

	summary := parseMessageSummary(msg)

	if summary.ID != "msg-1" {
		t.Errorf("expected ID 'msg-1', got %q", summary.ID)
	}
	if summary.From != "Alice <alice@example.com>" {
		t.Errorf("expected from, got %q", summary.From)
	}
	if summary.Subject != "Meeting tomorrow" {
		t.Errorf("expected subject, got %q", summary.Subject)
	}
	if !summary.Unread {
		t.Error("expected unread")
	}
}

func TestParseMessageBody(t *testing.T) {
	msg := &gapi.Message{
		Id: "msg-1", ThreadId: "thread-1",
		Payload: &gapi.MessagePart{
			MimeType: "text/plain",
			Body:     &gapi.MessagePartBody{Data: "SGVsbG8gd29ybGQ="},
			Headers: []*gapi.MessagePartHeader{
				{Name: "From", Value: "alice@example.com"},
				{Name: "To", Value: "bob@example.com"},
				{Name: "Subject", Value: "Test"},
				{Name: "Date", Value: "Mon, 8 Apr 2026 10:00:00 -0400"},
			},
		},
	}
	parsed := parseMessage(msg)
	if parsed.Body != "Hello world" {
		t.Errorf("expected 'Hello world', got %q", parsed.Body)
	}
	if parsed.ThreadID != "thread-1" {
		t.Errorf("expected thread-1, got %q", parsed.ThreadID)
	}
}

func TestGetHeader(t *testing.T) {
	headers := []*gapi.MessagePartHeader{
		{Name: "From", Value: "alice@example.com"},
		{Name: "Subject", Value: "Test"},
	}
	if got := getHeader(headers, "From"); got != "alice@example.com" {
		t.Errorf("got %q", got)
	}
	if got := getHeader(headers, "Missing"); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestParseMultipartBody(t *testing.T) {
	msg := &gapi.Message{
		Id: "msg-2",
		Payload: &gapi.MessagePart{
			MimeType: "multipart/alternative",
			Parts: []*gapi.MessagePart{
				{MimeType: "text/plain", Body: &gapi.MessagePartBody{Data: "UGxhaW4gdGV4dA=="}},
				{MimeType: "text/html", Body: &gapi.MessagePartBody{Data: "PHA-SFRNTDwvcD4="}},
			},
			Headers: []*gapi.MessagePartHeader{
				{Name: "From", Value: "alice@example.com"},
				{Name: "To", Value: "bob@example.com"},
				{Name: "Subject", Value: "Test"},
				{Name: "Date", Value: "Mon, 8 Apr 2026 10:00:00 -0400"},
			},
		},
	}
	parsed := parseMessage(msg)
	if parsed.Body != "Plain text" {
		t.Errorf("expected 'Plain text', got %q", parsed.Body)
	}
}
