package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"golang.org/x/term"

	"httpssh/relay/internal/config"
	"httpssh/relay/internal/session"
)

const (
	attachSubprotocol = "httpssh.v1"
	attachPingPeriod  = 20 * time.Second
)

// attachOpts captures everything `httpssh-relay attach` accepts on the
// command line, after merging config-file values and CLI flags.
type attachOpts struct {
	listenURL string // base URL to the relay (e.g. http://127.0.0.1:18822)
	bearer    string
	logLevel  string

	sessionID  string
	makeNew    bool
	listOnly   bool
	shellName  string
	titleHint  string
}

func runAttach(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("attach", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: httpssh-relay attach [flags]")
		fmt.Fprintln(os.Stderr, "Run inside a terminal you already have open. The current window")
		fmt.Fprintln(os.Stderr, "becomes the host-side viewer of a relay session that mobile clients")
		fmt.Fprintln(os.Stderr, "can also attach to.")
		fmt.Fprintln(os.Stderr)
		fs.PrintDefaults()
	}

	configPath := fs.String("config", "", "path to config.yaml (same file the server uses)")
	listenURL := fs.String("listen", "", "relay base URL or host:port (overrides config). Example: http://127.0.0.1:18822")
	bearer := fs.String("bearer", "", "LAN bearer token (overrides config)")
	logLevel := fs.String("log-level", "", "log level: debug, info, warn, error (overrides config)")
	sessionID := fs.String("session", "", "join an existing session by ID instead of creating a new one")
	makeNew := fs.Bool("new", false, "force-create a new session even if others are available")
	listOnly := fs.Bool("list", false, "print live sessions and exit (machine-friendly TSV)")
	shellName := fs.String("shell", "", "shell for the new session: auto, pwsh, powershell, cmd")
	titleHint := fs.String("title", "", "title for the new session")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}

	opts := attachOpts{
		listenURL: firstNonEmpty(*listenURL, cfg.Listen),
		bearer:    firstNonEmpty(*bearer, cfg.Auth.LANBearer),
		logLevel:  firstNonEmpty(*logLevel, cfg.Log.Level),
		sessionID: strings.TrimSpace(*sessionID),
		makeNew:   *makeNew,
		listOnly:  *listOnly,
		shellName: strings.TrimSpace(*shellName),
		titleHint: *titleHint,
	}
	if opts.bearer == "" {
		return errors.New("attach: bearer token is required (set auth.lan_bearer in config.yaml or pass --bearer)")
	}
	base, err := normalizeRelayURL(opts.listenURL)
	if err != nil {
		return err
	}
	opts.listenURL = base.String()

	if opts.listOnly {
		return printSessionList(ctx, opts)
	}

	// Refuse early when stdin is not a real terminal, so we don't
	// create a relay session that we'd immediately fail to attach to.
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return errors.New("attach: stdin/stdout must be a terminal (run this from PowerShell, Windows Terminal, or another interactive shell)")
	}

	id, err := resolveSessionID(ctx, opts)
	if err != nil {
		return err
	}

	wsURL, err := buildAttachWSURL(base, id, opts.bearer, true)
	if err != nil {
		return err
	}

	return runAttachIO(ctx, wsURL, id)
}

// normalizeRelayURL accepts "host:port", "http://host:port", or
// "https://host:port" and returns a URL with a scheme. Empty input is
// rejected.
func normalizeRelayURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("attach: relay listen URL is empty (set listen in config.yaml or pass --listen)")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("attach: parse listen URL %q: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("attach: listen URL must use http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("attach: listen URL has no host: %q", raw)
	}
	return u, nil
}

// buildAttachWSURL returns the WS URL for /api/sessions/{id}/io with the
// bearer token and (optionally) role=host appended as query parameters.
func buildAttachWSURL(base *url.URL, sessionID, bearer string, hostRole bool) (string, error) {
	if sessionID == "" {
		return "", errors.New("attach: empty session id")
	}
	u := *base
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/api/sessions/" + sessionID + "/io"
	q := u.Query()
	q.Set("token", bearer)
	if hostRole {
		q.Set("role", string(session.SubscriberRoleHost))
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// resolveSessionID picks which session the attach client should join.
//
// Order:
//   1. --session ID  → join that exact session (error if missing).
//   2. --new         → POST /api/sessions and return the new id.
//   3. (default)     → list sessions; if there is at least one without
//                      a host attached, prompt the user to pick one or
//                      create new. If the list is empty, create new.
func resolveSessionID(ctx context.Context, opts attachOpts) (string, error) {
	if opts.sessionID != "" {
		// Validate existence.
		if _, err := fetchSession(ctx, opts, opts.sessionID); err != nil {
			return "", err
		}
		return opts.sessionID, nil
	}
	if opts.makeNew {
		return createSession(ctx, opts)
	}

	infos, err := listSessions(ctx, opts)
	if err != nil {
		return "", err
	}
	candidates := make([]session.SessionInfo, 0, len(infos))
	for _, s := range infos {
		if !s.HostAttached {
			candidates = append(candidates, s)
		}
	}
	if len(candidates) == 0 {
		return createSession(ctx, opts)
	}

	fmt.Fprintln(os.Stderr, "Existing sessions without a host attached:")
	for i, s := range candidates {
		fmt.Fprintf(os.Stderr, "  [%d] %s  (%s, %dx%d, %d subscriber(s))\n",
			i+1, abbrev(s.ID, 12), s.Title, s.Cols, s.Rows, s.Subscribers)
	}
	fmt.Fprintf(os.Stderr, "  [N] new session\n")
	fmt.Fprint(os.Stderr, "Pick a number (or N) and press Enter: ")

	choice, err := readChoiceLine(os.Stdin)
	if err != nil {
		return "", err
	}
	switch choice {
	case "", "n", "N":
		return createSession(ctx, opts)
	}
	idx, err := strconv.Atoi(choice)
	if err != nil || idx < 1 || idx > len(candidates) {
		return "", fmt.Errorf("attach: invalid choice %q", choice)
	}
	return candidates[idx-1].ID, nil
}

func readChoiceLine(r io.Reader) (string, error) {
	br := bufio.NewReader(r)
	line, err := br.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func abbrev(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// --- REST helpers ---

func httpRequest(ctx context.Context, method string, opts attachOpts, path string, body io.Reader) (*http.Request, error) {
	u, err := url.Parse(opts.listenURL)
	if err != nil {
		return nil, err
	}
	u.Path = strings.TrimRight(u.Path, "/") + path
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+opts.bearer)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func listSessions(ctx context.Context, opts attachOpts) ([]session.SessionInfo, error) {
	req, err := httpRequest(ctx, "GET", opts, "/api/sessions", nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("attach: list sessions: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("attach: list sessions: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var payload struct {
		Sessions []session.SessionInfo `json:"sessions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("attach: decode sessions: %w", err)
	}
	return payload.Sessions, nil
}

func fetchSession(ctx context.Context, opts attachOpts, id string) (session.SessionInfo, error) {
	req, err := httpRequest(ctx, "GET", opts, "/api/sessions/"+id, nil)
	if err != nil {
		return session.SessionInfo{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return session.SessionInfo{}, fmt.Errorf("attach: fetch session: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return session.SessionInfo{}, fmt.Errorf("attach: session %q not found on the relay", id)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return session.SessionInfo{}, fmt.Errorf("attach: fetch session: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var info session.SessionInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return session.SessionInfo{}, fmt.Errorf("attach: decode session: %w", err)
	}
	return info, nil
}

type createSessionRequest struct {
	Shell string `json:"shell,omitempty"`
	Cols  uint16 `json:"cols,omitempty"`
	Rows  uint16 `json:"rows,omitempty"`
	Title string `json:"title,omitempty"`
}

func createSession(ctx context.Context, opts attachOpts) (string, error) {
	cols, rows, _ := getStdoutSize()
	if cols == 0 {
		cols = 120
	}
	if rows == 0 {
		rows = 40
	}
	body, _ := json.Marshal(createSessionRequest{
		Shell: opts.shellName,
		Cols:  cols,
		Rows:  rows,
		Title: opts.titleHint,
	})
	req, err := httpRequest(ctx, "POST", opts, "/api/sessions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("attach: create session: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("attach: create session: %s: %s", resp.Status, strings.TrimSpace(string(bodyBytes)))
	}
	var info session.SessionInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", fmt.Errorf("attach: decode create response: %w", err)
	}
	return info.ID, nil
}

func printSessionList(ctx context.Context, opts attachOpts) error {
	infos, err := listSessions(ctx, opts)
	if err != nil {
		return err
	}
	if len(infos) == 0 {
		fmt.Println("(no live sessions)")
		return nil
	}
	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()
	fmt.Fprintln(w, "id\ttitle\tshell\tdims\thostAttached\tsubscribers\tlastIo")
	for _, s := range infos {
		fmt.Fprintf(w, "%s\t%s\t%s\t%dx%d\t%t\t%d\t%s\n",
			s.ID, s.Title, s.Shell, s.Cols, s.Rows, s.HostAttached, s.Subscribers, s.LastIO.Format(time.RFC3339))
	}
	return nil
}

// --- I/O pump ---

// runAttachIO opens the WebSocket and shuttles bytes between the local
// terminal and the relay. It returns when ctx is canceled, the shell
// exits, or the WebSocket closes.
func runAttachIO(ctx context.Context, wsURL, sessionID string) error {
	dialCtx, cancelDial := context.WithTimeout(ctx, 10*time.Second)
	defer cancelDial()
	conn, _, err := websocket.Dial(dialCtx, wsURL, &websocket.DialOptions{
		Subprotocols: []string{attachSubprotocol},
	})
	if err != nil {
		return fmt.Errorf("attach: dial %s: %w", wsURL, err)
	}
	defer conn.CloseNow()
	if conn.Subprotocol() != attachSubprotocol {
		return fmt.Errorf("attach: server did not negotiate %q", attachSubprotocol)
	}
	conn.SetReadLimit(1 << 20)

	restore, err := enterRawMode()
	if err != nil {
		return fmt.Errorf("attach: enter raw mode: %w", err)
	}
	defer restore()

	ioCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	cols, rows, _ := getStdoutSize()
	if cols > 0 && rows > 0 {
		_ = wsjson.Write(ioCtx, conn, session.ClientFrame{
			T: session.FrameResize,
			C: cols,
			R: rows,
		})
	}

	var wg sync.WaitGroup
	wg.Add(3)

	exitCode := -1
	exitMu := &sync.Mutex{}

	// stdin -> ws
	go func() {
		defer wg.Done()
		buf := make([]byte, 1024)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				if werr := wsjson.Write(ioCtx, conn, session.ClientFrame{
					T: session.FrameIn,
					D: string(buf[:n]),
				}); werr != nil {
					cancel()
					return
				}
			}
			if err != nil {
				cancel()
				return
			}
			if ioCtx.Err() != nil {
				return
			}
		}
	}()

	// ws -> stdout
	go func() {
		defer wg.Done()
		for {
			var f session.ServerFrame
			if err := wsjson.Read(ioCtx, conn, &f); err != nil {
				cancel()
				return
			}
			switch f.T {
			case session.FrameReplay, session.FrameOut:
				if len(f.D) > 0 {
					_, _ = os.Stdout.Write([]byte(f.D))
				}
			case session.FrameExit:
				exitMu.Lock()
				if f.Code != nil {
					exitCode = *f.Code
				}
				exitMu.Unlock()
				cancel()
				return
			case session.FrameError:
				fmt.Fprintf(os.Stderr, "\r\n[relay error] %s\r\n", f.Message)
			case session.FramePong:
				// noop
			}
		}
	}()

	// ping + resize watcher merged in this goroutine
	go func() {
		defer wg.Done()
		ping := time.NewTicker(attachPingPeriod)
		defer ping.Stop()

		resizeCh := make(chan struct{ c, r uint16 }, 4)
		stopWatch := watchResize(ioCtx, resizeCh)
		defer stopWatch()

		for {
			select {
			case <-ioCtx.Done():
				return
			case <-ping.C:
				if err := wsjson.Write(ioCtx, conn, session.ClientFrame{T: session.FramePing}); err != nil {
					cancel()
					return
				}
			case sz := <-resizeCh:
				if err := wsjson.Write(ioCtx, conn, session.ClientFrame{
					T: session.FrameResize,
					C: sz.c,
					R: sz.r,
				}); err != nil {
					cancel()
					return
				}
			}
		}
	}()

	<-ioCtx.Done()
	_ = conn.Close(websocket.StatusNormalClosure, "")
	wg.Wait()
	restore()

	exitMu.Lock()
	code := exitCode
	exitMu.Unlock()

	fmt.Fprintf(os.Stderr, "\r\nsession-id: %s\r\n", sessionID)
	if code >= 0 {
		fmt.Fprintf(os.Stderr, "shell exited with code %d\r\n", code)
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
