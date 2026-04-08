package markdown

import (
	"fmt"
	"net"
	"net/http"
	"sync"
)

type PreviewServer struct {
	mu   sync.RWMutex
	html string
	ln   net.Listener
	srv  *http.Server
}

func NewPreviewServer(html string) (*PreviewServer, error) {
	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return nil, fmt.Errorf("failed to start preview server: %w", err)
	}

	ps := &PreviewServer{
		html: html,
		ln:   ln,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", ps.handler)

	ps.srv = &http.Server{Handler: mux}
	go ps.srv.Serve(ln)

	return ps, nil
}

func (ps *PreviewServer) handler(w http.ResponseWriter, r *http.Request) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(ps.html))
}

func (ps *PreviewServer) URL() string {
	return fmt.Sprintf("http://localhost:%d", ps.ln.Addr().(*net.TCPAddr).Port)
}

func (ps *PreviewServer) Update(html string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.html = html
}

func (ps *PreviewServer) Close() error {
	return ps.srv.Close()
}
