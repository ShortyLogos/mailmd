package markdown

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestPreviewServerServesHTML(t *testing.T) {
	html := "<html><body><p>Hello</p></body></html>"
	srv, err := NewPreviewServer(html)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	resp, err := http.Get(srv.URL())
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Hello") {
		t.Errorf("expected HTML content, got: %s", body)
	}
	if resp.Header.Get("Content-Type") != "text/html; charset=utf-8" {
		t.Errorf("expected text/html content type, got: %s", resp.Header.Get("Content-Type"))
	}
}

func TestPreviewServerURL(t *testing.T) {
	srv, err := NewPreviewServer("<html></html>")
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	url := srv.URL()
	if !strings.HasPrefix(url, "http://localhost:") {
		t.Errorf("expected localhost URL, got: %s", url)
	}
}

func TestPreviewServerUpdate(t *testing.T) {
	srv, err := NewPreviewServer("<p>First</p>")
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	srv.Update("<p>Second</p>")

	resp, err := http.Get(srv.URL())
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Second") {
		t.Errorf("expected updated content, got: %s", body)
	}
}
