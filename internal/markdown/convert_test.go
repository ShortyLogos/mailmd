package markdown

import (
	"strings"
	"testing"
)

func TestConvertBasicMarkdown(t *testing.T) {
	md := "Hello **world**"
	html, err := Convert(md)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "<strong>world</strong>") {
		t.Errorf("expected <strong> tag, got: %s", html)
	}
}

func TestConvertHeadings(t *testing.T) {
	md := "# Title\n\nSome text"
	html, err := Convert(md)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "<h1") {
		t.Errorf("expected h1 tag, got: %s", html)
	}
}

func TestConvertCodeBlock(t *testing.T) {
	md := "```go\nfmt.Println(\"hello\")\n```"
	html, err := Convert(md)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "Println") {
		t.Errorf("expected code content, got: %s", html)
	}
}

func TestConvertWrapsInHTMLDocument(t *testing.T) {
	md := "Hello"
	html, err := Convert(md)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "<!DOCTYPE html>") {
		t.Errorf("expected full HTML document, got: %s", html)
	}
	if !strings.Contains(html, "</html>") {
		t.Errorf("expected closing html tag")
	}
}

func TestConvertInlineStyles(t *testing.T) {
	md := "Hello **world**"
	html, err := Convert(md)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "style=") {
		t.Errorf("expected inline styles for email compatibility, got: %s", html)
	}
}

func TestConvertLinks(t *testing.T) {
	md := "[Click here](https://example.com)"
	html, err := Convert(md)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, `href="https://example.com"`) {
		t.Errorf("expected link, got: %s", html)
	}
}
