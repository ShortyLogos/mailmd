package gmail

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"sync"

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
	TrashMessages(ctx context.Context, ids []string) error
	UntrashMessage(ctx context.Context, id string) error
	DeleteMessage(ctx context.Context, id string) error
	DeleteMessages(ctx context.Context, ids []string) error
	MoveMessage(ctx context.Context, id string, addLabels, removeLabels []string) error
	ModifyMessages(ctx context.Context, ids []string, addLabels, removeLabels []string) error
	GetAttachment(ctx context.Context, messageID, attachmentID string) ([]byte, error)
	CheckAttachments(ctx context.Context, ids []string) (map[string]bool, error)
	GetProfile(ctx context.Context) (string, error)
	BlockSender(ctx context.Context, senderEmail string) error
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

	// Fetch message metadata concurrently (throttled to avoid rate limits)
	const maxConcurrent = 10
	sem := make(chan struct{}, maxConcurrent)

	messages := make([]MessageSummary, len(resp.Messages))
	var wg sync.WaitGroup
	for i, m := range resp.Messages {
		wg.Add(1)
		go func(idx int, id string) {
			defer wg.Done()
			sem <- struct{}{}        // acquire
			defer func() { <-sem }() // release

			msg, err := c.svc.Users.Messages.Get(c.user, id).
				Format("metadata").
				MetadataHeaders("From", "Subject", "Date").
				Context(ctx).
				Do()
			if err != nil {
				// Retry once on transient failure
				msg, err = c.svc.Users.Messages.Get(c.user, id).
					Format("metadata").
					MetadataHeaders("From", "Subject", "Date").
					Context(ctx).
					Do()
			}
			if err != nil {
				// Use minimal placeholder so message count stays consistent
				messages[idx] = MessageSummary{ID: id, Subject: "(failed to load)"}
				return
			}
			messages[idx] = parseMessageSummary(msg)
		}(i, m.Id)
	}
	wg.Wait()

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

func (c *gmailClient) UntrashMessage(ctx context.Context, id string) error {
	if _, err := c.svc.Users.Messages.Untrash(c.user, id).Context(ctx).Do(); err != nil {
		return err
	}
	// Untrash removes from Trash but doesn't restore the INBOX label — add it back
	req := &gapi.ModifyMessageRequest{AddLabelIds: []string{"INBOX"}}
	_, err := c.svc.Users.Messages.Modify(c.user, id, req).Context(ctx).Do()
	return err
}

func (c *gmailClient) DeleteMessage(ctx context.Context, id string) error {
	return c.svc.Users.Messages.Delete(c.user, id).Context(ctx).Do()
}

func (c *gmailClient) DeleteMessages(ctx context.Context, ids []string) error {
	if len(ids) == 1 {
		return c.DeleteMessage(ctx, ids[0])
	}
	req := &gapi.BatchDeleteMessagesRequest{Ids: ids}
	return c.svc.Users.Messages.BatchDelete(c.user, req).Context(ctx).Do()
}

func (c *gmailClient) TrashMessages(ctx context.Context, ids []string) error {
	if len(ids) == 1 {
		return c.TrashMessage(ctx, ids[0])
	}
	req := &gapi.BatchModifyMessagesRequest{
		Ids:            ids,
		AddLabelIds:    []string{"TRASH"},
		RemoveLabelIds: []string{"INBOX"},
	}
	return c.svc.Users.Messages.BatchModify(c.user, req).Context(ctx).Do()
}

func (c *gmailClient) MoveMessage(ctx context.Context, id string, addLabels, removeLabels []string) error {
	req := &gapi.ModifyMessageRequest{AddLabelIds: addLabels, RemoveLabelIds: removeLabels}
	_, err := c.svc.Users.Messages.Modify(c.user, id, req).Context(ctx).Do()
	return err
}

func (c *gmailClient) ModifyMessages(ctx context.Context, ids []string, addLabels, removeLabels []string) error {
	if len(ids) == 1 {
		return c.MoveMessage(ctx, ids[0], addLabels, removeLabels)
	}
	req := &gapi.BatchModifyMessagesRequest{
		Ids:            ids,
		AddLabelIds:    addLabels,
		RemoveLabelIds: removeLabels,
	}
	return c.svc.Users.Messages.BatchModify(c.user, req).Context(ctx).Do()
}

func (c *gmailClient) GetAttachment(ctx context.Context, messageID, attachmentID string) ([]byte, error) {
	resp, err := c.svc.Users.Messages.Attachments.Get(c.user, messageID, attachmentID).Context(ctx).Do()
	if err != nil {
		return nil, err
	}
	return base64.URLEncoding.DecodeString(resp.Data)
}

// CheckAttachments fetches lightweight part structure for each message
// and returns which ones have real (non-inline) attachments.
func (c *gmailClient) CheckAttachments(ctx context.Context, ids []string) (map[string]bool, error) {
	type result struct {
		id  string
		has bool
	}
	ch := make(chan result, len(ids))
	for _, id := range ids {
		go func(id string) {
			msg, err := c.svc.Users.Messages.Get(c.user, id).
				Format("full").
				Fields("payload(filename,headers,body(attachmentId,size),parts(filename,headers,body(attachmentId,size),parts(filename,headers,body(attachmentId,size),parts(filename,headers,body(attachmentId,size)))))").
				Context(ctx).
				Do()
			if err != nil {
				ch <- result{id: id, has: false}
				return
			}
			ch <- result{id: id, has: hasAttachments(msg.Payload)}
		}(id)
	}
	out := make(map[string]bool, len(ids))
	for range ids {
		r := <-ch
		if r.has {
			out[r.id] = true
		}
	}
	return out, nil
}

func (c *gmailClient) BlockSender(ctx context.Context, senderEmail string) error {
	filter := &gapi.Filter{
		Criteria: &gapi.FilterCriteria{
			From: senderEmail,
		},
		Action: &gapi.FilterAction{
			AddLabelIds:    []string{"TRASH"},
			RemoveLabelIds: []string{"INBOX"},
		},
	}
	_, err := c.svc.Users.Settings.Filters.Create(c.user, filter).Context(ctx).Do()
	return err
}

func (c *gmailClient) GetProfile(ctx context.Context) (string, error) {
	profile, err := c.svc.Users.GetProfile(c.user).Context(ctx).Do()
	if err != nil {
		return "", err
	}
	return profile.EmailAddress, nil
}
