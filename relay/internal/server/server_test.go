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

	"github.com/coder/websocket"

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
		Shells:          func(string) (string, []string, error) { return "fake", nil, nil },
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

func TestServer_CreateSessionIdleTimeout(t *testing.T) {
	srv, _ := newTestServer(t)
	h := srv.Handler()

	cases := []struct {
		name    string
		body    string
		want    int64
		wantErr bool
	}{
		{"omitted_uses_default", `{"shell":"pwsh"}`, -1, false},
		{"zero_means_unlimited", `{"shell":"pwsh","idleTimeoutSeconds":0}`, 0, false},
		{"positive_value", `{"shell":"pwsh","idleTimeoutSeconds":3600}`, 3600, false},
		{"negative_rejected", `{"shell":"pwsh","idleTimeoutSeconds":-1}`, 0, true},
		{"too_large_rejected", `{"shell":"pwsh","idleTimeoutSeconds":99999999999}`, 0, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := bearerReq("POST", "/api/sessions", []byte(tc.body))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if tc.wantErr {
				if w.Code != http.StatusBadRequest {
					t.Fatalf("status=%d want 400, body=%s", w.Code, w.Body.String())
				}
				return
			}
			if w.Code != http.StatusCreated {
				t.Fatalf("status=%d want 201, body=%s", w.Code, w.Body.String())
			}
			var got map[string]any
			decodeJSON(t, w.Body, &got)
			gotTimeout, ok := got["idleTimeoutSeconds"].(float64)
			if !ok {
				t.Fatalf("idleTimeoutSeconds missing or wrong type: %v", got["idleTimeoutSeconds"])
			}
			if tc.want >= 0 && int64(gotTimeout) != tc.want {
				t.Fatalf("idleTimeoutSeconds=%d want %d", int64(gotTimeout), tc.want)
			}
			// For the "omitted" case we can't predict the value (it
			// comes from the test manager's default), just assert it
			// is non-negative.
			if tc.want < 0 && int64(gotTimeout) < 0 {
				t.Fatalf("idleTimeoutSeconds=%d expected >= 0", int64(gotTimeout))
			}
		})
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

func TestServer_SessionFiles_RequiresKnownCWD(t *testing.T) {
	fileSvc, err := fileapi.NewService(fileapi.Config{})
	if err != nil {
		t.Fatal(err)
	}
	srv, mgr := newTestServerWithFileService(t, fileSvc)
	sess, err := mgr.Create("pwsh", 80, 24, "", -1)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	r := bearerReq("GET", "/api/sessions/"+sess.ID+"/files/list", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s want 409 cwd_unknown", w.Code, w.Body.String())
	}
}

func TestServer_SessionFiles_ListAndRead(t *testing.T) {
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "note.txt"), []byte("hi from cwd"), 0o600); err != nil {
		t.Fatal(err)
	}
	fileSvc, err := fileapi.NewService(fileapi.Config{})
	if err != nil {
		t.Fatal(err)
	}
	srv, mgr := newTestServerWithFileService(t, fileSvc)
	sess, err := mgr.Create("pwsh", 80, 24, "", -1)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	sess.SetCWD(cwd)

	r := bearerReq("GET", "/api/sessions/"+sess.ID+"/files/list", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", w.Code, w.Body.String())
	}
	var list map[string]any
	decodeJSON(t, w.Body, &list)
	if list["root"] != "session:"+sess.ID {
		t.Fatalf("root=%v want session:%s", list["root"], sess.ID)
	}
	entries, _ := list["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("entries=%+v want 1", entries)
	}

	r = bearerReq("GET", "/api/sessions/"+sess.ID+"/files/read?path=note.txt", nil)
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("read status=%d body=%s", w.Code, w.Body.String())
	}
	var doc map[string]any
	decodeJSON(t, w.Body, &doc)
	if doc["content"] != "hi from cwd" {
		t.Fatalf("doc=%+v", doc)
	}
}

func TestServer_SessionFiles_RejectsEscapeAboveCWD(t *testing.T) {
	cwd := t.TempDir()
	fileSvc, err := fileapi.NewService(fileapi.Config{})
	if err != nil {
		t.Fatal(err)
	}
	srv, mgr := newTestServerWithFileService(t, fileSvc)
	sess, err := mgr.Create("pwsh", 80, 24, "", -1)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	sess.SetCWD(cwd)

	r := bearerReq("GET", "/api/sessions/"+sess.ID+"/files/list?path=..", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s want 403", w.Code, w.Body.String())
	}
}

// --- WebSocket smoke ---

func TestServer_WebSocketRefusesWithoutSubprotocol(t *testing.T) {
	srv, mgr := newTestServer(t)
	s, err := mgr.Create("pwsh", 80, 24, "", -1)
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

func TestServer_WebSocketRoleHostFlipsHostAttached(t *testing.T) {
	srv, mgr := newTestServer(t)
	s, err := mgr.Create("pwsh", 80, 24, "", -1)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	wsURL := "ws" + ts.URL[len("http"):] + "/api/sessions/" + s.ID + "/io?token=test-bearer-32-chars-or-longer-12345&role=host"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{Subprotocols: []string{"httpssh.v1"}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.CloseNow()

	// Wait until the subscriber registers (Subscribe takes s.mu briefly).
	deadline := time.Now().Add(2 * time.Second)
	var info session.SessionInfo
	for time.Now().Before(deadline) {
		info = s.Info()
		if info.HostAttached {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !info.HostAttached {
		t.Fatalf("HostAttached=false after dialing with role=host")
	}

	// A second WS without role=host must not flip it back, and must not
	// turn it true on its own.
	wsURLNoRole := "ws" + ts.URL[len("http"):] + "/api/sessions/" + s.ID + "/io?token=test-bearer-32-chars-or-longer-12345"
	c2, _, err := websocket.Dial(ctx, wsURLNoRole, &websocket.DialOptions{Subprotocols: []string{"httpssh.v1"}})
	if err != nil {
		t.Fatalf("dial2: %v", err)
	}
	defer c2.CloseNow()
	if !s.Info().HostAttached {
		t.Fatalf("HostAttached flipped to false after a viewer attached")
	}

	// Closing the host connection drops HostAttached back to false.
	_ = c.Close(websocket.StatusNormalClosure, "")
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !s.Info().HostAttached {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if s.Info().HostAttached {
		t.Fatalf("HostAttached still true after host websocket closed")
	}
}
