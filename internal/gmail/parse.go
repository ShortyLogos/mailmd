package gmail

import (
	"encoding/base64"
	"html"
	"net/mail"
	"strings"
	"time"

	gapi "google.golang.org/api/gmail/v1"
)

func parseMessageSummary(msg *gapi.Message) MessageSummary {
	headers := msg.Payload.Headers
	date := parseDate(getHeader(headers, "Date"), msg.InternalDate)
	unread := false
	for _, l := range msg.LabelIds {
		if l == "UNREAD" {
			unread = true
			break
		}
	}
	return MessageSummary{
		ID:             msg.Id,
		From:           getHeader(headers, "From"),
		To:             getHeader(headers, "To"),
		Subject:        getHeader(headers, "Subject"),
		Snippet:        html.UnescapeString(msg.Snippet),
		Date:           date,
		Unread:         unread,
		HasAttachments: hasAttachments(msg.Payload),
	}
}

func parseMessage(msg *gapi.Message) *Message {
	headers := msg.Payload.Headers
	date := parseDate(getHeader(headers, "Date"), msg.InternalDate)
	plain, html := extractBodies(msg.Payload)
	attachments := extractAttachments(msg.Payload)
	return &Message{
		ID:          msg.Id,
		ThreadID:    msg.ThreadId,
		MessageID:   getHeader(headers, "Message-ID"),
		From:        getHeader(headers, "From"),
		To:          getHeader(headers, "To"),
		CC:          getHeader(headers, "Cc"),
		Subject:     getHeader(headers, "Subject"),
		Date:        date,
		Body:        plain,
		HTMLBody:    html,
		Attachments: attachments,
	}
}

func hasAttachments(part *gapi.MessagePart) bool {
	if part == nil {
		return false
	}
	// Check for real attachment parts — skip small inline images (tracking pixels, signature logos)
	// and calendar invitations (.ics files)
	if part.Filename != "" && part.Body != nil && part.Body.AttachmentId != "" {
		if strings.HasSuffix(strings.ToLower(part.Filename), ".ics") || part.MimeType == "text/calendar" || part.MimeType == "application/ics" {
			// Calendar invitation — skip
		} else if strings.HasPrefix(getHeader(part.Headers, "Content-Disposition"), "inline") && part.Body.Size < 50*1024 {
			// Small inline image — likely a tracking pixel or signature logo, skip
		} else {
			return true
		}
	}
	for _, p := range part.Parts {
		if hasAttachments(p) {
			return true
		}
	}
	return false
}

func extractAttachments(part *gapi.MessagePart) []Attachment {
	var attachments []Attachment
	if part == nil {
		return attachments
	}
	if part.Filename != "" && part.Body != nil && part.Body.AttachmentId != "" {
		attachments = append(attachments, Attachment{
			ID:       part.Body.AttachmentId,
			Filename: part.Filename,
			MimeType: part.MimeType,
			Size:     part.Body.Size,
		})
	}
	for _, p := range part.Parts {
		attachments = append(attachments, extractAttachments(p)...)
	}
	return attachments
}

func extractBodies(part *gapi.MessagePart) (plain, html string) {
	if part == nil {
		return "", ""
	}
	switch {
	case part.MimeType == "text/plain" && part.Body != nil:
		plain = decodeBody(part.Body.Data)
	case part.MimeType == "text/html" && part.Body != nil:
		html = decodeBody(part.Body.Data)
	case strings.HasPrefix(part.MimeType, "multipart/"):
		for _, p := range part.Parts {
			pp, ph := extractBodies(p)
			if pp != "" {
				plain = pp
			}
			if ph != "" {
				html = ph
			}
		}
	}
	return plain, html
}

func decodeBody(data string) string {
	decoded, err := base64.URLEncoding.DecodeString(data)
	if err != nil {
		decoded, err = base64.StdEncoding.DecodeString(data)
		if err != nil {
			return data
		}
	}
	return string(decoded)
}

// parseDate tries the standard mail.ParseDate (handles RFC 2822 and variants),
// then falls back to Gmail's InternalDate (Unix millis).
func parseDate(header string, internalDate int64) time.Time {
	if header != "" {
		if t, err := mail.ParseDate(header); err == nil {
			return t
		}
	}
	// Fallback to InternalDate (milliseconds since epoch)
	if internalDate > 0 {
		return time.UnixMilli(internalDate)
	}
	return time.Time{}
}

func getHeader(headers []*gapi.MessagePartHeader, name string) string {
	for _, h := range headers {
		if h.Name == name {
			return h.Value
		}
	}
	return ""
}
