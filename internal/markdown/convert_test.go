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

func TestConvertPlainStripsBold(t *testing.T) {
	result := ConvertPlain("Hello **world** and __foo__")
	if strings.Contains(result, "**") || strings.Contains(result, "__") {
		t.Errorf("expected bold markers stripped, got: %s", result)
	}
	if !strings.Contains(result, "Hello world and foo") {
		t.Errorf("expected plain text content, got: %s", result)
	}
}

func TestConvertPlainStripsItalic(t *testing.T) {
	result := ConvertPlain("Hello *world* and _foo_")
	if strings.Contains(result, "*world*") {
		t.Errorf("expected italic markers stripped, got: %s", result)
	}
}

func TestConvertPlainConvertsLinks(t *testing.T) {
	result := ConvertPlain("[Click here](https://example.com)")
	if !strings.Contains(result, "Click here (https://example.com)") {
		t.Errorf("expected link converted to text + url, got: %s", result)
	}
}

func TestConvertPlainStripsHeadings(t *testing.T) {
	result := ConvertPlain("## My Heading")
	if strings.Contains(result, "##") {
		t.Errorf("expected heading markers stripped, got: %s", result)
	}
	if !strings.Contains(result, "My Heading") {
		t.Errorf("expected heading text preserved, got: %s", result)
	}
}

func TestConvertPlainStripsCodeFences(t *testing.T) {
	result := ConvertPlain("Before\n```go\nfmt.Println(\"hi\")\n```\nAfter")
	if strings.Contains(result, "```") {
		t.Errorf("expected code fences stripped, got: %s", result)
	}
	if !strings.Contains(result, "fmt.Println") {
		t.Errorf("expected code content preserved, got: %s", result)
	}
}

func TestConvertPlainStripsImages(t *testing.T) {
	result := ConvertPlain("Check ![screenshot](https://img.png) out")
	if strings.Contains(result, "![") {
		t.Errorf("expected image syntax stripped, got: %s", result)
	}
	if !strings.Contains(result, "screenshot") {
		t.Errorf("expected alt text preserved, got: %s", result)
	}
}

func TestConvertPlainPreservesBlockquotes(t *testing.T) {
	result := ConvertPlain("> quoted text\n> more quoted")
	if !strings.Contains(result, "> quoted text") {
		t.Errorf("expected blockquotes preserved, got: %s", result)
	}
}
