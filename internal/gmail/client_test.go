package gmail

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildMIMEMessage(t *testing.T) {
	mime := buildMIMEMessage("bob@example.com", "", "Test Subject", "<p>HTML</p>", "Plain text", "", nil)
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
	if strings.Contains(mime, "Cc:") {
		t.Errorf("expected no Cc header when cc is empty")
	}
	if strings.Contains(mime, "multipart/mixed") {
		t.Errorf("expected no multipart/mixed without attachments")
	}
}

func TestBuildMIMEMessageWithCC(t *testing.T) {
	mime := buildMIMEMessage("bob@example.com", "carol@example.com", "Test Subject", "<p>HTML</p>", "Plain text", "", nil)
	if !strings.Contains(mime, "Cc: carol@example.com") {
		t.Errorf("expected Cc header, got: %s", mime)
	}
}

func TestBuildReplyMessage(t *testing.T) {
	msgID := "<CABx+abc123@mail.gmail.com>"
	mime := buildMIMEMessage("bob@example.com", "", "Re: Test", "<p>Reply</p>", "Reply", msgID, nil)
	if !strings.Contains(mime, "In-Reply-To: "+msgID) {
		t.Errorf("expected In-Reply-To header with Message-ID")
	}
	if !strings.Contains(mime, "References: "+msgID) {
		t.Errorf("expected References header with Message-ID")
	}
}

func TestBuildMIMEMessageWithAttachment(t *testing.T) {
	// Create a temp file to attach
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("hello world"), 0644)

	attachments := []AttachmentFile{{Path: path}}
	mime := buildMIMEMessage("bob@example.com", "", "Test", "<p>Hi</p>", "Hi", "", attachments)

	if !strings.Contains(mime, "multipart/mixed") {
		t.Errorf("expected multipart/mixed with attachments")
	}
	if !strings.Contains(mime, "multipart/alternative") {
		t.Errorf("expected multipart/alternative for body")
	}
	if !strings.Contains(mime, `filename="test.txt"`) {
		t.Errorf("expected attachment filename")
	}
	if !strings.Contains(mime, "Content-Transfer-Encoding: base64") {
		t.Errorf("expected base64 encoding for attachment")
	}
}
