package markdown

import (
	"fmt"
	"strings"
)

type ComposeData struct {
	To      string
	CC      string
	Subject string
	Body    string
}

func ParseCompose(content string) (*ComposeData, error) {
	if !strings.HasPrefix(content, "---\n") {
		return nil, fmt.Errorf("missing frontmatter: file must start with ---")
	}

	rest := content[4:] // skip opening "---\n"
	endIdx := strings.Index(rest, "\n---\n")
	if endIdx == -1 {
		return nil, fmt.Errorf("missing frontmatter: no closing ---")
	}

	header := rest[:endIdx]
	body := rest[endIdx+5:] // skip "\n---\n"
	// Strip single leading newline (blank separator line between frontmatter and body)
	body = strings.TrimPrefix(body, "\n")

	data := &ComposeData{
		Body: body,
	}

	for _, line := range strings.Split(header, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, val, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)

		switch key {
		case "to":
			data.To = val
		case "subject":
			data.Subject = val
		}
	}

	if data.To == "" {
		return nil, fmt.Errorf("missing required 'to' field in frontmatter")
	}

	return data, nil
}

func ComposeTemplate() string {
	return `---
to:
subject:
---

`
}

func ReplyTemplate(to, subject, quotedBody string) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("to: " + to + "\n")
	b.WriteString("subject: " + subject + "\n")
	b.WriteString("---\n\n\n")
	for _, line := range strings.Split(quotedBody, "\n") {
		b.WriteString("> " + line + "\n")
	}
	return b.String()
}

func DraftTemplate(to, subject, body string) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("to: " + to + "\n")
	b.WriteString("subject: " + subject + "\n")
	b.WriteString("---\n\n")
	b.WriteString(body)
	return b.String()
}

func ForwardTemplate(subject, originalBody, originalFrom string) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("to: \n")
	b.WriteString("subject: Fwd: " + subject + "\n")
	b.WriteString("---\n\n")
	b.WriteString("---------- Forwarded message ----------\n")
	b.WriteString("From: " + originalFrom + "\n\n")
	b.WriteString(originalBody + "\n")
	return b.String()
}
