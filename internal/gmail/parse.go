package gmail

import (
	"encoding/base64"
	"strings"
	"time"

	gapi "google.golang.org/api/gmail/v1"
)

func parseMessageSummary(msg *gapi.Message) MessageSummary {
	headers := msg.Payload.Headers
	date, _ := time.Parse(time.RFC1123Z, getHeader(headers, "Date"))
	unread := false
	for _, l := range msg.LabelIds {
		if l == "UNREAD" {
			unread = true
			break
		}
	}
	return MessageSummary{
		ID:      msg.Id,
		From:    getHeader(headers, "From"),
		Subject: getHeader(headers, "Subject"),
		Snippet: msg.Snippet,
		Date:    date,
		Unread:  unread,
	}
}

func parseMessage(msg *gapi.Message) *Message {
	headers := msg.Payload.Headers
	date, _ := time.Parse(time.RFC1123Z, getHeader(headers, "Date"))
	plain, html := extractBodies(msg.Payload)
	return &Message{
		ID:       msg.Id,
		ThreadID: msg.ThreadId,
		From:     getHeader(headers, "From"),
		To:       getHeader(headers, "To"),
		Subject:  getHeader(headers, "Subject"),
		Date:     date,
		Body:     plain,
		HTMLBody: html,
	}
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

func getHeader(headers []*gapi.MessagePartHeader, name string) string {
	for _, h := range headers {
		if h.Name == name {
			return h.Value
		}
	}
	return ""
}
