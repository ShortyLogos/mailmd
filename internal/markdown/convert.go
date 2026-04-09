package markdown

import (
	"bytes"
	"fmt"
	"io"

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

func ConvertPlain(source string) string {
	return source
}

func wrapHTML(body string) string {
	var buf bytes.Buffer
	io.WriteString(&buf, `<!DOCTYPE html>
<html>
<head><meta charset="utf-8"></head>
<body style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; font-size: 14px; line-height: 1.6; color: #333; max-width: 600px; margin: 0; padding: 20px;">
<style>
h1, h2, h3, h4, h5, h6 { color: #1a1a1a; margin-top: 24px; margin-bottom: 16px; }
h1 { font-size: 24px; }
h2 { font-size: 20px; }
h3 { font-size: 16px; }
p { margin: 0 0 16px 0; }
a { color: #0366d6; }
code { background: #f6f8fa; padding: 2px 6px; border-radius: 3px; font-size: 13px; }
pre { background: #f6f8fa; padding: 16px; border-radius: 6px; overflow-x: auto; }
pre code { background: none; padding: 0; }
blockquote { border-left: 4px solid #dfe2e5; margin: 0 0 16px 0; padding: 0 16px; color: #6a737d; }
ul, ol { margin: 0 0 16px 0; padding-left: 24px; }
li { margin-bottom: 4px; }
strong { color: #1a1a1a; }
hr { border: none; border-top: 1px solid #e1e4e8; margin: 24px 0; }
</style>
`)
	io.WriteString(&buf, body)
	io.WriteString(&buf, "\n</body>\n</html>")
	return buf.String()
}
