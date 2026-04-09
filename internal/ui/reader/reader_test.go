package reader

import (
	"strings"
	"testing"
)

func TestStripHTMLWithTable(t *testing.T) {
	input := `<p>Here is the summary:</p>` +
		`<table><tr><th>Name</th><th>Value</th></tr>` +
		`<tr><td>Alpha</td><td>100</td></tr></table>` +
		`<p>End of report.</p>`

	result := stripHTML(input, 0)

	if !strings.Contains(result, "| Name") {
		t.Errorf("expected table preserved in output, got:\n%s", result)
	}
	if !strings.Contains(result, "Here is the summary") {
		t.Errorf("expected surrounding text preserved, got:\n%s", result)
	}
	if !strings.Contains(result, "End of report") {
		t.Errorf("expected trailing text preserved, got:\n%s", result)
	}
}

func TestStripHTMLTableColumnAlignment(t *testing.T) {
	input := `<table><thead><tr><th>Category</th><th>Tool</th><th>Description</th></tr></thead>` +
		`<tbody><tr><td>Monitoring</td><td>Grafana</td><td>Dashboard</td></tr>` +
		`<tr><td>Logging</td><td>Loki</td><td>Log aggregation</td></tr></tbody></table>`

	result := stripHTML(input, 0)

	// All pipe-delimited lines should have the same length (aligned columns)
	var pipeLines []string
	for _, line := range strings.Split(result, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "|") {
			pipeLines = append(pipeLines, line)
		}
	}
	if len(pipeLines) < 3 {
		t.Fatalf("expected at least 3 table lines, got %d:\n%s", len(pipeLines), result)
	}
	firstLen := len(pipeLines[0])
	for i, line := range pipeLines {
		if len(line) != firstLen {
			t.Errorf("line %d has length %d, expected %d (misaligned columns):\n%s", i, len(line), firstLen, line)
		}
	}
}

func TestStripHTMLTableEntitiesAligned(t *testing.T) {
	// Entities like &amp; must not cause column misalignment
	input := `<table><tr><th>A &amp; B</th><th>Column2</th></tr>` +
		`<tr><td>short</td><td>ok</td></tr></table>`

	result := stripHTML(input, 0)

	var pipeLines []string
	for _, line := range strings.Split(result, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "|") {
			pipeLines = append(pipeLines, line)
		}
	}
	if len(pipeLines) < 2 {
		t.Fatalf("expected at least 2 table lines, got %d:\n%s", len(pipeLines), result)
	}
	// All lines must have the same length
	firstLen := len(pipeLines[0])
	for i, line := range pipeLines {
		if len(line) != firstLen {
			t.Errorf("line %d has length %d, expected %d (entity caused misalignment):\n%s",
				i, len(line), firstLen, line)
		}
	}
	// Content should be decoded
	if !strings.Contains(result, "A & B") {
		t.Errorf("expected decoded entity, got:\n%s", result)
	}
}

func TestRenderTextTable(t *testing.T) {
	rows := [][]string{
		{"Name", "Score"},
		{"Alice", "95"},
		{"Bob", "87"},
	}
	result := renderTextTable(rows, 0)
	lines := strings.Split(strings.TrimRight(result, "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines, got %d:\n%s", len(lines), result)
	}
	if !strings.Contains(lines[1], "---") {
		t.Errorf("expected separator with dashes, got: %s", lines[1])
	}
	// All lines same length
	firstLen := len(lines[0])
	for i, line := range lines {
		if len(line) != firstLen {
			t.Errorf("line %d has length %d, expected %d", i, len(line), firstLen)
		}
	}
}

func TestRenderTextTableWrapsLongCells(t *testing.T) {
	rows := [][]string{
		{"Name", "Description"},
		{"Tool", "This is a very long description that should be wrapped across multiple lines within the cell"},
	}
	result := renderTextTable(rows, 50)
	lines := strings.Split(strings.TrimRight(result, "\n"), "\n")
	// The data row should span multiple display lines
	dataLines := 0
	for _, l := range lines {
		if strings.Contains(l, "| ") && !strings.Contains(l, "---") && !strings.Contains(l, "Name") {
			dataLines++
		}
	}
	if dataLines < 2 {
		t.Errorf("expected long cell to wrap across multiple lines, got %d data lines:\n%s", dataLines, result)
	}
	// All lines should have the same length (aligned columns)
	firstLen := len(lines[0])
	for i, line := range lines {
		if len(line) != firstLen {
			t.Errorf("line %d has length %d, expected %d (misaligned):\n%s", i, len(line), firstLen, line)
		}
	}
}

func TestStripHTMLHeadingsMarked(t *testing.T) {
	input := `<h1>Main Title</h1><p>Some text.</p><h2>Sub <em>heading</em></h2>`
	result := stripHTML(input, 0)

	if !strings.Contains(result, headingMarker+"Main Title") {
		t.Errorf("expected h1 marked, got:\n%q", result)
	}
	if !strings.Contains(result, headingMarker+"Sub heading") {
		t.Errorf("expected h2 marked (inner tags stripped), got:\n%q", result)
	}
	if !strings.Contains(result, "Some text.") {
		t.Errorf("expected paragraph text preserved, got:\n%q", result)
	}
}

func TestRenderPlainEmailConsumesHeadingMarker(t *testing.T) {
	input := headingMarker + "My Heading\nNormal line"
	result := renderPlainEmail(input)

	if strings.Contains(result, headingMarker) {
		t.Errorf("heading marker should be consumed, got:\n%q", result)
	}
	if !strings.Contains(result, "My Heading") {
		t.Errorf("heading text should be preserved, got:\n%q", result)
	}
	if !strings.Contains(result, "Normal line") {
		t.Errorf("normal text should be preserved, got:\n%q", result)
	}
}

func TestWrapTextSkipsTableLines(t *testing.T) {
	line := "| " + strings.Repeat("x", 100) + " | " + strings.Repeat("y", 100) + " |"
	result := wrapText(line+"\n", 80)
	lines := strings.Split(strings.TrimRight(result, "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("expected table line to not be wrapped, got %d lines:\n%s", len(lines), result)
	}
}

func TestWrapCell(t *testing.T) {
	text := "This is a sentence that should wrap"
	lines := wrapCell(text, 15)
	if len(lines) < 2 {
		t.Errorf("expected multiple lines, got %d: %v", len(lines), lines)
	}
	for i, l := range lines {
		if len(l) > 15 {
			t.Errorf("line %d exceeds max width: %q", i, l)
		}
	}
}
