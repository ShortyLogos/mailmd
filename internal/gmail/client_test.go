package gmail

import (
	"strings"
	"testing"
)

func TestBuildMIMEMessage(t *testing.T) {
	mime := buildMIMEMessage("bob@example.com", "Test Subject", "<p>HTML</p>", "Plain text", "")
	if !strings.Contains(mime, "To: bob@example.com") {
		t.Errorf("expected To header")
	}
	if !strings.Contains(mime, "Subject: Test Subject") {
		t.Errorf("expected Subject header")
	}
	if !strings.Contains(mime, "multipart/alternative") {
		t.Errorf("expected multipart/alternative")
	}
	if !strings.Contains(mime, "text/plain") {
		t.Errorf("expected text/plain part")
	}
	if !strings.Contains(mime, "text/html") {
		t.Errorf("expected text/html part")
	}
}

func TestBuildReplyMessage(t *testing.T) {
	mime := buildMIMEMessage("bob@example.com", "Re: Test", "<p>Reply</p>", "Reply", "thread-123")
	if !strings.Contains(mime, "In-Reply-To: thread-123") {
		t.Errorf("expected In-Reply-To header")
	}
}
