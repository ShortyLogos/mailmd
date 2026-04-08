package markdown

import "testing"

func TestParseFrontmatter(t *testing.T) {
	input := `---
to: alice@example.com
subject: Meeting tomorrow
---

Hey Alice,

Are we still on for **tomorrow**?
`
	data, err := ParseCompose(input)
	if err != nil {
		t.Fatal(err)
	}
	if data.To != "alice@example.com" {
		t.Errorf("expected to 'alice@example.com', got %q", data.To)
	}
	if data.Subject != "Meeting tomorrow" {
		t.Errorf("expected subject 'Meeting tomorrow', got %q", data.Subject)
	}
	expected := "Hey Alice,\n\nAre we still on for **tomorrow**?\n"
	if data.Body != expected {
		t.Errorf("expected body %q, got %q", expected, data.Body)
	}
}

func TestParseFrontmatterMissingTo(t *testing.T) {
	input := `---
subject: Test
---

Body here.
`
	_, err := ParseCompose(input)
	if err == nil {
		t.Error("expected error for missing 'to' field")
	}
}

func TestParseFrontmatterNoDelimiters(t *testing.T) {
	input := "Just some text without frontmatter"
	_, err := ParseCompose(input)
	if err == nil {
		t.Error("expected error for missing frontmatter")
	}
}

func TestParseFrontmatterMultipleRecipients(t *testing.T) {
	input := `---
to: alice@example.com, bob@example.com
subject: Group thread
---

Hello everyone.
`
	data, err := ParseCompose(input)
	if err != nil {
		t.Fatal(err)
	}
	if data.To != "alice@example.com, bob@example.com" {
		t.Errorf("expected multiple recipients, got %q", data.To)
	}
}

func TestCreateReplyTemplate(t *testing.T) {
	tmpl := ReplyTemplate("alice@example.com", "Re: Meeting", "Original message here")
	data, err := ParseCompose(tmpl)
	if err != nil {
		t.Fatal(err)
	}
	if data.To != "alice@example.com" {
		t.Errorf("expected to 'alice@example.com', got %q", data.To)
	}
	if data.Subject != "Re: Meeting" {
		t.Errorf("expected subject 'Re: Meeting', got %q", data.Subject)
	}
}
