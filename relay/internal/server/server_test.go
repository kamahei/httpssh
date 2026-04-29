package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"httpssh/relay/internal/auth"
	"httpssh/relay/internal/conpty"
	"httpssh/relay/internal/fileapi"
	"httpssh/relay/internal/session"
)

// fakePTY implements conpty.PTY without touching the OS.
type fakePTY struct {
	mu     sync.Mutex
	closed bool
	doneCh chan struct{}
	readCh chan []byte
}

func newFakePTY() *fakePTY {
	return &fakePTY{
		doneCh: make(chan struct{}),
		readCh: make(chan []byte, 8),
	}
}

func (p *fakePTY) Read(b []byte) (int, error) {
	select {
	case data, ok := <-p.readCh:
		if !ok {
			return 0, io.EOF
		}
		return copy(b, data), nil
	case <-p.doneCh:
		return 0, io.EOF
	}
}

func (p *fakePTY) Write(b []byte) (int, error)    { return len(b), nil }
func (p *fakePTY) Resize(cols, rows uint16) error { return nil }
func (p *fakePTY) Wait() (int, error)             { <-p.doneCh; return 0, nil }
func (p *fakePTY) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	close(p.doneCh)
	return nil
}

func newTestServer(t *testing.T) (*Server, *session.Manager) {
	t.Helper()
	return newTestServerWithFileService(t, nil)
}

func newTestServerWithFileService(t *testing.T, fileSvc *fileapi.Service) (*Server, *session.Manager) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mgr := session.NewManager(session.Options{
		ScrollbackBytes: 4096,
		IdleTimeout:     time.Hour,
		ReapInterval:    time.Hour,
		Shells:          func(string) (string, error) { return "fake", nil },
		Spawn: func(string, []string, uint16, uint16) (conpty.PTY, error) {
			return newFakePTY(), nil
		},
		Logger: logger,
	})
	t.Cleanup(mgr.Shutdown)

	srv := New(Options{
		Manager:     mgr,
		Auth:        auth.Config{LANBearer: "test-bearer-32-chars-or-longer-12345", Logger: logger},
		FileService: fileSvc,
		Logger:      logger,
		Version:     "0.0.0-test",
	})
	return srv, mgr
}

func bearerReq(method, path string, body []byte) *http.Request {
	var r *http.Request
	if body == nil {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, bytes.NewReader(body))
	}
	r.Header.Set("Authorization", "Bearer test-bearer-32-chars-or-longer-12345")
	r.Header.Set("Content-Type", "application/json")
	return r
}

func decodeJSON(t *testing.T, body io.Reader, dst any) {
	t.Helper()
	if err := json.NewDecoder(body).Decode(dst); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

// --- handler tests ---

func TestServer_Health(t *testing.T) {
	srv, _ := newTestServer(t)
	r := bearerReq("GET", "/api/health", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want 200", w.Code)
	}
	var got map[string]any
	decodeJSON(t, w.Body, &got)
	if got["status"] != "ok" {
		t.Fatalf("body=%v want status=ok", got)
	}
	if got["version"] != "0.0.0-test" {
		t.Fatalf("version=%v want 0.0.0-test", got["version"])
	}
}

func TestServer_RootRedirectsToWeb(t *testing.T) {
	srv, _ := newTestServer(t)
	r := bearerReq("GET", "/", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusFound {
		t.Fatalf("status=%d want 302", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/web/" {
		t.Fatalf("Location=%q want /web/", loc)
	}
}

func TestServer_CreateAndListSession(t *testing.T) {
	srv, _ := newTestServer(t)
	h := srv.Handler()

	body := []byte(`{"shell":"pwsh","cols":120,"rows":40}`)
	r := bearerReq("POST", "/api/sessions", body)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", w.Code, w.Body.String())
	}
	var created map[string]any
	decodeJSON(t, w.Body, &created)
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatal("no id returned")
	}

	r = bearerReq("GET", "/api/sessions", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("list status=%d", w.Code)
	}
	var list struct {
		Sessions []map[string]any `json:"sessions"`
	}
	decodeJSON(t, w.Body, &list)
	if len(list.Sessions) != 1 || list.Sessions[0]["id"] != id {
		t.Fatalf("list = %+v, want one session %s", list.Sessions, id)
	}
}

func TestServer_RenameAndDeleteSession(t *testing.T) {
	srv, _ := newTestServer(t)
	h := srv.Handler()

	r := bearerReq("POST", "/api/sessions", []byte(`{"shell":"pwsh"}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var created map[string]any
	decodeJSON(t, w.Body, &created)
	id := created["id"].(string)

	// PATCH
	r = bearerReq("PATCH", "/api/sessions/"+id, []byte(`{"title":"renamed"}`))
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("rename status=%d", w.Code)
	}
	var renamed map[string]any
	decodeJSON(t, w.Body, &renamed)
	if renamed["title"] != "renamed" {
		t.Fatalf("title=%v want renamed", renamed["title"])
	}

	// DELETE
	r = bearerReq("DELETE", "/api/sessions/"+id, nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d", w.Code)
	}

	// Second DELETE -> 404
	r = bearerReq("DELETE", "/api/sessions/"+id, nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("second delete status=%d want 404", w.Code)
	}
}

func TestServer_CreateSessionInvalidDimensions(t *testing.T) {
	srv, _ := newTestServer(t)
	r := bearerReq("POST", "/api/sessions", []byte(`{"cols":9999,"rows":40}`))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want 400", w.Code)
	}
}

func TestServer_AuthRequired(t *testing.T) {
	srv, _ := newTestServer(t)
	r := httptest.NewRequest("GET", "/api/health", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want 401", w.Code)
	}
}

func TestServer_StaticServedWithoutAuth(t *testing.T) {
	// /web/* must be reachable WITHOUT credentials so the SPA shell can
	// load and present its Settings dialog where the user pastes the
	// bearer. /api/* remains gated.
	srv, _ := newTestServer(t)
	h := srv.Handler()

	for _, path := range []string{"/web/", "/web/index.html", "/web/app.js", "/web/style.css"} {
		r := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code == http.StatusUnauthorized {
			t.Fatalf("%s: status=401, expected 200/404 without auth", path)
		}
	}
}

func TestServer_RootRedirectWithoutAuth(t *testing.T) {
	srv, _ := newTestServer(t)
	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusFound {
		t.Fatalf("status=%d want 302 (redirect should not require auth)", w.Code)
	}
}

func TestServer_RenameMissingSession(t *testing.T) {
	srv, _ := newTestServer(t)
	r := bearerReq("PATCH", "/api/sessions/does-not-exist", []byte(`{"title":"x"}`))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d want 404", w.Code)
	}
}

func TestServer_FileRootsRequireAuth(t *testing.T) {
	srv, _ := newTestServer(t)
	r := httptest.NewRequest("GET", "/api/files/roots", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want 401", w.Code)
	}
}

func TestServer_FileRootsAndRead(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	fileSvc, err := fileapi.NewService(fileapi.Config{
		Roots: []fileapi.RootConfig{{ID: "main", Name: "Main", Path: root}},
	})
	if err != nil {
		t.Fatal(err)
	}
	srv, _ := newTestServerWithFileService(t, fileSvc)
	h := srv.Handler()

	r := bearerReq("GET", "/api/files/roots", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("roots status=%d body=%s", w.Code, w.Body.String())
	}
	var roots struct {
		Roots []map[string]any `json:"roots"`
	}
	decodeJSON(t, w.Body, &roots)
	if len(roots.Roots) != 1 || roots.Roots[0]["id"] != "main" {
		t.Fatalf("roots=%+v", roots.Roots)
	}

	r = bearerReq("GET", "/api/files/list?root=main", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", w.Code, w.Body.String())
	}

	r = bearerReq("GET", "/api/files/read?root=main&path=note.txt", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("read status=%d body=%s", w.Code, w.Body.String())
	}
	var doc map[string]any
	decodeJSON(t, w.Body, &doc)
	if doc["content"] != "hello" {
		t.Fatalf("doc=%+v", doc)
	}
}

func TestServer_FileReadRejectsEscape(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	fileSvc, err := fileapi.NewService(fileapi.Config{
		Roots: []fileapi.RootConfig{{ID: "main", Name: "Main", Path: root}},
	})
	if err != nil {
		t.Fatal(err)
	}
	srv, _ := newTestServerWithFileService(t, fileSvc)

	r := bearerReq("GET", "/api/files/read?root=main&path="+url.QueryEscape(outside), nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s want 403", w.Code, w.Body.String())
	}
}

func TestServer_FileDisabled(t *testing.T) {
	srv, _ := newTestServer(t)
	r := bearerReq("GET", "/api/files/list?root=main", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s want 404", w.Code, w.Body.String())
	}
}

// --- WebSocket smoke ---

func TestServer_WebSocketRefusesWithoutSubprotocol(t *testing.T) {
	srv, mgr := newTestServer(t)
	s, err := mgr.Create("pwsh", 80, 24, "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	url := ts.URL + "/api/sessions/" + s.ID + "/io"
	req, _ := http.NewRequestWithContext(context.Background(), "GET", url, nil)
	req.Header.Set("Authorization", "Bearer test-bearer-32-chars-or-longer-12345")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	// Intentionally no Sec-WebSocket-Protocol -> server must not echo
	// our subprotocol back, even if it switches protocols.

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusSwitchingProtocols && resp.Header.Get("Sec-WebSocket-Protocol") == "httpssh.v1" {
		t.Fatalf("server negotiated httpssh.v1 even though client did not request it")
	}
}
