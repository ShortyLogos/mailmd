package gmail

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	gapi "google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

// Client is the Gmail API client interface.
type Client interface {
	ListLabels(ctx context.Context) ([]Label, error)
	ListMessages(ctx context.Context, labelID string, query string, pageToken string) (*MessageList, error)
	GetMessage(ctx context.Context, id string) (*Message, error)
	SendMessage(ctx context.Context, to, subject, htmlBody, plainBody string) error
	ReplyMessage(ctx context.Context, threadID, to, subject, htmlBody, plainBody string) error
	ForwardMessage(ctx context.Context, messageID, to string) error
	TrashMessage(ctx context.Context, id string) error
	MoveMessage(ctx context.Context, id string, addLabels, removeLabels []string) error
}

type gmailClient struct {
	svc  *gapi.Service
	user string
}

// NewClient creates a new Gmail API client using the provided HTTP client for auth.
func NewClient(ctx context.Context, httpClient *http.Client) (Client, error) {
	svc, err := gapi.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		return nil, err
	}
	return &gmailClient{svc: svc, user: "me"}, nil
}

// --- Read operations ---

func (c *gmailClient) ListLabels(ctx context.Context) ([]Label, error) {
	resp, err := c.svc.Users.Labels.List(c.user).Context(ctx).Do()
	if err != nil {
		return nil, err
	}
	labels := make([]Label, 0, len(resp.Labels))
	for _, l := range resp.Labels {
		labels = append(labels, Label{ID: l.Id, Name: l.Name})
	}
	return labels, nil
}

func (c *gmailClient) ListMessages(ctx context.Context, labelID string, query string, pageToken string) (*MessageList, error) {
	req := c.svc.Users.Messages.List(c.user).LabelIds(labelID).MaxResults(50).Context(ctx)
	if query != "" {
		req = req.Q(query)
	}
	if pageToken != "" {
		req = req.PageToken(pageToken)
	}
	resp, err := req.Do()
	if err != nil {
		return nil, err
	}

	// Fetch message metadata concurrently
	type result struct {
		idx     int
		summary MessageSummary
		err     error
	}

	results := make(chan result, len(resp.Messages))
	for i, m := range resp.Messages {
		go func(idx int, id string) {
			msg, err := c.svc.Users.Messages.Get(c.user, id).
				Format("metadata").
				MetadataHeaders("From", "Subject", "Date").
				Context(ctx).
				Do()
			if err != nil {
				results <- result{idx: idx, err: err}
				return
			}
			results <- result{idx: idx, summary: parseMessageSummary(msg)}
		}(i, m.Id)
	}

	// Collect results in original order
	ordered := make([]MessageSummary, len(resp.Messages))
	valid := make([]bool, len(resp.Messages))
	for range resp.Messages {
		r := <-results
		if r.err == nil {
			ordered[r.idx] = r.summary
			valid[r.idx] = true
		}
	}

	messages := make([]MessageSummary, 0, len(resp.Messages))
	for i, s := range ordered {
		if valid[i] {
			messages = append(messages, s)
		}
	}

	return &MessageList{Messages: messages, NextPageToken: resp.NextPageToken}, nil
}

func (c *gmailClient) GetMessage(ctx context.Context, id string) (*Message, error) {
	msg, err := c.svc.Users.Messages.Get(c.user, id).Format("full").Context(ctx).Do()
	if err != nil {
		return nil, err
	}
	return parseMessage(msg), nil
}

// --- Write operations ---

func buildMIMEMessage(to, subject, htmlBody, plainBody, inReplyTo string) string {
	boundary := "mailmd-boundary-1234567890"
	var b strings.Builder
	b.WriteString("To: " + to + "\r\n")
	b.WriteString("Subject: " + subject + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	if inReplyTo != "" {
		b.WriteString("In-Reply-To: " + inReplyTo + "\r\n")
		b.WriteString("References: " + inReplyTo + "\r\n")
	}
	b.WriteString("Content-Type: multipart/alternative; boundary=" + boundary + "\r\n\r\n")
	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n\r\n")
	b.WriteString(plainBody + "\r\n")
	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: text/html; charset=utf-8\r\n\r\n")
	b.WriteString(htmlBody + "\r\n")
	b.WriteString("--" + boundary + "--\r\n")
	return b.String()
}

func buildRawMessage(to, subject, htmlBody, plainBody string) string {
	mime := buildMIMEMessage(to, subject, htmlBody, plainBody, "")
	return base64.URLEncoding.EncodeToString([]byte(mime))
}

func (c *gmailClient) SendMessage(ctx context.Context, to, subject, htmlBody, plainBody string) error {
	raw := buildRawMessage(to, subject, htmlBody, plainBody)
	msg := &gapi.Message{Raw: raw}
	_, err := c.svc.Users.Messages.Send(c.user, msg).Context(ctx).Do()
	return err
}

func (c *gmailClient) ReplyMessage(ctx context.Context, threadID, to, subject, htmlBody, plainBody string) error {
	mime := buildMIMEMessage(to, subject, htmlBody, plainBody, threadID)
	raw := base64.URLEncoding.EncodeToString([]byte(mime))
	msg := &gapi.Message{Raw: raw, ThreadId: threadID}
	_, err := c.svc.Users.Messages.Send(c.user, msg).Context(ctx).Do()
	return err
}

func (c *gmailClient) ForwardMessage(ctx context.Context, messageID, to string) error {
	original, err := c.GetMessage(ctx, messageID)
	if err != nil {
		return fmt.Errorf("failed to fetch original: %w", err)
	}
	subject := "Fwd: " + original.Subject
	body := original.Body
	if original.HTMLBody != "" {
		body = original.HTMLBody
	}
	return c.SendMessage(ctx, to, subject, body, original.Body)
}

func (c *gmailClient) TrashMessage(ctx context.Context, id string) error {
	_, err := c.svc.Users.Messages.Trash(c.user, id).Context(ctx).Do()
	return err
}

func (c *gmailClient) MoveMessage(ctx context.Context, id string, addLabels, removeLabels []string) error {
	req := &gapi.ModifyMessageRequest{AddLabelIds: addLabels, RemoveLabelIds: removeLabels}
	_, err := c.svc.Users.Messages.Modify(c.user, id, req).Context(ctx).Do()
	return err
}
