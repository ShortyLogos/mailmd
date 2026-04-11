package markdown

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"
)

var md goldmark.Markdown

func init() {
	md = goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			highlighting.NewHighlighting(
				highlighting.WithStyle("monokai"),
				highlighting.WithFormatOptions(),
			),
		),
		goldmark.WithRendererOptions(
			html.WithUnsafe(),
		),
	)
}

func Convert(source string) (string, error) {
	var buf bytes.Buffer
	if err := md.Convert([]byte(source), &buf); err != nil {
		return "", fmt.Errorf("markdown conversion failed: %w", err)
	}
	return wrapHTML(buf.String()), nil
}

var (
	reImage     = regexp.MustCompile(`!\[([^\]]*)\]\([^)]+\)`)
	reLink      = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	reBoldStar  = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reBoldUnd   = regexp.MustCompile(`__(.+?)__`)
	reItalStar  = regexp.MustCompile(`\*(.+?)\*`)
	reItalUnd   = regexp.MustCompile(`_(.+?)_`)
	reStrike    = regexp.MustCompile(`~~(.+?)~~`)
	reInlineCode = regexp.MustCompile("`([^`]+)`")
	reHeading   = regexp.MustCompile(`^#{1,6}\s+`)
)

// ConvertPlain strips Markdown formatting to produce readable plain text
// for the text/plain MIME alternative.
func ConvertPlain(source string) string {
	lines := strings.Split(source, "\n")
	var out []string
	inFence := false
	for _, line := range lines {
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			out = append(out, line)
			continue
		}
		// Strip heading markers
		line = reHeading.ReplaceAllString(line, "")
		// Images: ![alt](url) → alt
		line = reImage.ReplaceAllString(line, "$1")
		// Links: [text](url) → text (url)
		line = reLink.ReplaceAllString(line, "$1 ($2)")
		// Bold, italic, strikethrough
		line = reBoldStar.ReplaceAllString(line, "$1")
		line = reBoldUnd.ReplaceAllString(line, "$1")
		line = reItalStar.ReplaceAllString(line, "$1")
		line = reItalUnd.ReplaceAllString(line, "$1")
		line = reStrike.ReplaceAllString(line, "$1")
		// Inline code
		line = reInlineCode.ReplaceAllString(line, "$1")
		// Horizontal rules
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" || trimmed == "***" || trimmed == "___" {
			line = strings.Repeat("-", 40)
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// inlineStyles maps HTML tag names to inline style attributes.
// Gmail strips <style> blocks, so all styling must be inline.
var inlineStyles = map[string]string{
	"table":      `style="border-collapse:collapse;margin:0 0 16px 0;width:100%"`,
	"th":         `style="border:1px solid #d0d7de;padding:8px 12px;text-align:left;background:#f6f8fa;font-weight:600"`,
	"td":         `style="border:1px solid #d0d7de;padding:8px 12px;text-align:left"`,
	"blockquote": `style="border-left:4px solid #dfe2e5;margin:0 0 16px 0;padding:0 16px;color:#6a737d"`,
	"pre":        `style="background:#f6f8fa;padding:16px;border-radius:6px;overflow-x:auto"`,
	"code":       `style="background:#f6f8fa;padding:2px 6px;border-radius:3px;font-size:13px"`,
	"h1":         `style="color:#1a1a1a;margin-top:24px;margin-bottom:16px;font-size:24px"`,
	"h2":         `style="color:#1a1a1a;margin-top:24px;margin-bottom:16px;font-size:20px"`,
	"h3":         `style="color:#1a1a1a;margin-top:24px;margin-bottom:16px;font-size:16px"`,
	"p":          `style="margin:0 0 16px 0"`,
	"a":          `style="color:#0366d6"`,
	"hr":         `style="border:none;border-top:1px solid #e1e4e8;margin:24px 0"`,
}

var reOpenTag = regexp.MustCompile(`<(table|th|td|blockquote|pre|code|h[1-3]|p|a|hr)(\s|>)`)

// applyInlineStyles adds inline style attributes to HTML tags.
func applyInlineStyles(html string) string {
	return reOpenTag.ReplaceAllStringFunc(html, func(match string) string {
		// Extract tag name
		tag := match[1 : len(match)-1] // strip < and trailing char
		trailing := match[len(match)-1:]
		if style, ok := inlineStyles[tag]; ok {
			if trailing == ">" {
				return "<" + tag + " " + style + ">"
			}
			return "<" + tag + " " + style + trailing
		}
		return match
	})
}

func wrapHTML(body string) string {
	body = applyInlineStyles(body)
	var buf bytes.Buffer
	io.WriteString(&buf, `<!DOCTYPE html>
<html>
<head><meta charset="utf-8"></head>
<body style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; font-size: 14px; line-height: 1.6; color: #333; max-width: 600px; margin: 0; padding: 20px;">
`)
	io.WriteString(&buf, body)
	io.WriteString(&buf, "\n</body>\n</html>")
	return buf.String()
}
