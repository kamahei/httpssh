package session

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"time"

	"httpssh/relay/internal/conpty"
)

const (
	defaultScrollbackBytes = 4 << 20 // 4 MiB
	defaultSubscriberQueue = 256
	resizeRepaintWindow    = 750 * time.Millisecond

	// Bounds for client-requested terminal dimensions.
	MaxCols uint16 = 500
	MaxRows uint16 = 200
)

// Session is one running shell attached to a ConPTY plus its subscribers and
// scrollback. Sessions are owned by a Manager.
type Session struct {
	ID        string
	Title     string
	Shell     string
	CreatedAt time.Time

	mu          sync.Mutex
	cols, rows  uint16
	pty         conpty.PTY
	scrollback  *RingBuffer
	subs        map[*subscriber]struct{}
	lastIO      time.Time
	idleTimeout time.Duration // 0 = never reaped
	closed      bool
	exitCode    *int
	exitErr     error
	doneCh      chan struct{}
	cwd         string
	// resizeRepaintUntil suppresses ConPTY's resize-triggered screen repaint.
	// Those bytes redraw already-visible content, so sending or retaining them
	// makes reconnects display stale screen copies before the real scrollback.
	resizeRepaintUntil time.Time
	// cwdTracker is touched only by the pump goroutine; no mutex.
	cwdTracker *cwdTracker
}

// SessionInfo is a snapshot of public session metadata used for listings.
//
// CWD is the last working directory reported by the shell prompt via
// OSC 9;9. It is empty until the first prompt fires; it is also empty
// when the shell's current location is on a non-FileSystem provider
// (e.g. `cd HKLM:`), because the prompt wrapper only emits OSC 9;9 for
// FileSystem locations.
type SessionInfo struct {
	ID                 string    `json:"id"`
	Title              string    `json:"title"`
	Shell              string    `json:"shell"`
	Cols               uint16    `json:"cols"`
	Rows               uint16    `json:"rows"`
	CreatedAt          time.Time `json:"createdAt"`
	LastIO             time.Time `json:"lastIo"`
	Subscribers        int       `json:"subscribers"`
	HostAttached       bool      `json:"hostAttached"`
	CWD                string    `json:"cwd,omitempty"`
	IdleTimeoutSeconds int64     `json:"idleTimeoutSeconds"`
}

// Info returns a metadata snapshot.
func (s *Session) Info() SessionInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	host := false
	for sub := range s.subs {
		if sub.role == SubscriberRoleHost {
			host = true
			break
		}
	}
	return SessionInfo{
		ID:                 s.ID,
		Title:              s.Title,
		Shell:              s.Shell,
		Cols:               s.cols,
		Rows:               s.rows,
		CreatedAt:          s.CreatedAt,
		LastIO:             s.lastIO,
		Subscribers:        len(s.subs),
		HostAttached:       host,
		CWD:                s.cwd,
		IdleTimeoutSeconds: int64(s.idleTimeout / time.Second),
	}
}

// CWD returns the last filesystem CWD reported by the shell prompt, or
// the empty string if no OSC 9;9 has been observed yet.
func (s *Session) CWD() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cwd
}

// SetCWD records a new working directory on the session. Production
// callers do not invoke this directly: the pump goroutine calls it as
// it parses OSC 9;9 sequences emitted by the shell prompt. Exposed so
// tests outside this package can seed a CWD without writing fake OSC
// bytes through a private PTY.
func (s *Session) SetCWD(path string) { s.setCWD(path) }

func (s *Session) setCWD(path string) {
	s.mu.Lock()
	s.cwd = path
	s.mu.Unlock()
}

// Resize asks the underlying ConPTY to switch dimensions.
//
// No-ops silently when the requested size matches what the PTY is already at.
// This matters because every fresh WebSocket — including the auto-reconnect
// after a brief network blip — sends an initial resize; if we forwarded that
// to ConPTY unconditionally the shell would receive a SIGWINCH-equivalent and
// PSReadLine (and similar prompt-redrawing shells) would emit a fresh prompt
// into the scrollback. Over repeated reconnects those accumulated prompt
// redraws are exactly what users see as "past logs duplicated".
func (s *Session) Resize(cols, rows uint16) error {
	if cols < 1 || cols > MaxCols || rows < 1 || rows > MaxRows {
		return ErrInvalidDimensions
	}
	s.mu.Lock()
	pty := s.pty
	closed := s.closed
	sameSize := s.cols == cols && s.rows == rows
	s.mu.Unlock()
	if closed {
		return ErrSessionClosed
	}
	if sameSize {
		return nil
	}
	if err := pty.Resize(cols, rows); err != nil {
		return err
	}
	s.mu.Lock()
	s.cols = cols
	s.rows = rows
	s.resizeRepaintUntil = time.Now().Add(resizeRepaintWindow)
	s.mu.Unlock()
	return nil
}

// WriteInput forwards bytes from a client into the PTY.
func (s *Session) WriteInput(p []byte) error {
	s.mu.Lock()
	pty := s.pty
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return ErrSessionClosed
	}
	if _, err := pty.Write(p); err != nil {
		return err
	}
	s.mu.Lock()
	s.lastIO = time.Now()
	s.mu.Unlock()
	return nil
}

// ScrollbackSnapshot returns a copy of the current scrollback bytes.
func (s *Session) ScrollbackSnapshot() []byte {
	return s.scrollback.Snapshot()
}

// publishOutput records one PTY read in scrollback and delivers the matching
// live frame to the subscribers that were attached before the write became
// visible. Holding s.mu across both operations gives Subscribe a clean
// ordering: a reconnecting client receives a chunk either as replay or as
// live output, never both.
func (s *Session) publishOutput(data []byte, cwdPaths []string, now time.Time) {
	frame := ServerFrame{T: FrameOut, D: string(data)}
	var cancelSubs []*subscriber

	s.mu.Lock()
	dropFrame := s.suppressResizeRepaintLocked(data, now)
	if !dropFrame {
		_, _ = s.scrollback.Write(data)
	}
	s.lastIO = now
	for _, path := range cwdPaths {
		s.cwd = path
	}
	if !dropFrame {
		for sub := range s.subs {
			select {
			case sub.out <- frame:
			case <-sub.ctx.Done():
				delete(s.subs, sub)
				cancelSubs = append(cancelSubs, sub)
			default:
				delete(s.subs, sub)
				cancelSubs = append(cancelSubs, sub)
			}
		}
	}
	s.mu.Unlock()

	for _, sub := range cancelSubs {
		sub.cancel()
	}
}

func (s *Session) suppressResizeRepaintLocked(data []byte, now time.Time) bool {
	if s.resizeRepaintUntil.IsZero() {
		return false
	}
	if now.After(s.resizeRepaintUntil) {
		s.resizeRepaintUntil = time.Time{}
		return false
	}
	return looksLikeConPTYResizeRepaint(data)
}

func looksLikeConPTYResizeRepaint(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	return bytes.Contains(data, []byte("\x1b[?25l")) &&
		bytes.Contains(data, []byte("\x1b[H")) &&
		bytes.Contains(data, []byte("\x1b[?25h")) &&
		bytes.Count(data, []byte("\x1b[K")) >= 2
}

// Done returns a channel that is closed after the underlying shell exits or
// the session is killed.
func (s *Session) Done() <-chan struct{} { return s.doneCh }

// ExitState reports the exit code (if any) and the error that ended the
// session. Both are zero/nil while the session is still live.
func (s *Session) ExitState() (code *int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exitCode, s.exitErr
}

// Kill terminates the underlying shell process. Idempotent.
func (s *Session) Kill() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	pty := s.pty
	s.mu.Unlock()
	return pty.Close()
}

// --- Subscribers ---

// SubscriberRole tags a subscriber so that session metadata can surface
// "the host PC is currently attached" to mobile clients (used by the
// `httpssh-relay attach` command). An empty role is the default and is
// treated as a regular viewer.
type SubscriberRole string

const (
	// SubscriberRoleHost marks the PC-side local attach client. At most
	// one such subscriber per session is meaningful, but the session
	// model does not enforce uniqueness — extra hosts are accepted as
	// regular viewers from the relay's perspective.
	SubscriberRoleHost SubscriberRole = "host"
)

// subscriber is the internal record of a live attached client. The out
// channel is never closed by the session: it becomes garbage once the
// websocket writer goroutine notices ctx.Done() and returns. This avoids
// the classic "send on closed channel" race when fanout and unsubscribe
// run concurrently.
type subscriber struct {
	out    chan ServerFrame
	ctx    context.Context
	cancel context.CancelFunc
	role   SubscriberRole
}

// Done returns a channel that closes when the subscription should end.
func (sub *subscriber) Done() <-chan struct{} { return sub.ctx.Done() }

// Subscribe registers a subscriber. The returned channel receives an initial
// `replay` frame followed by live `out`/`exit`/`error` frames. The
// `done` channel closes when the subscription ends (either because the
// caller cancels via the returned func, the parent ctx cancels, or the
// session ends). Callers MUST select on `done` and stop reading from the
// frame channel once it fires; reads from the channel after `done` may
// block forever.
//
// role tags the subscriber for SessionInfo.HostAttached. Pass
// SubscriberRoleHost for the PC-side `httpssh-relay attach` client; pass
// the empty string for ordinary viewers (e.g. the mobile app).
func (s *Session) Subscribe(ctx context.Context, role SubscriberRole) (frames <-chan ServerFrame, done <-chan struct{}, unsubscribe func()) {
	ch := make(chan ServerFrame, defaultSubscriberQueue)
	subCtx, cancel := context.WithCancel(ctx)
	sub := &subscriber{out: ch, ctx: subCtx, cancel: cancel, role: role}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		cancel()
		return ch, subCtx.Done(), func() {}
	}
	// Snapshot, register, and enqueue the replay frame all under the same
	// lock publishOutput takes. Without this, a PTY chunk could become
	// visible in scrollback just before this snapshot and then also be
	// delivered as live `out` to this new subscriber. The subscriber
	// channel has 256 slots and is freshly created, so a non-blocking send
	// always succeeds here.
	replayData := s.scrollback.Snapshot()
	s.subs[sub] = struct{}{}
	select {
	case ch <- ServerFrame{T: FrameReplay, D: string(replayData)}:
	default:
		// Should never happen on a fresh channel; defensive only.
	}
	s.mu.Unlock()

	return ch, subCtx.Done(), func() { s.removeSubscriber(sub) }
}

func (s *Session) removeSubscriber(sub *subscriber) {
	s.mu.Lock()
	if _, ok := s.subs[sub]; ok {
		delete(s.subs, sub)
	}
	s.mu.Unlock()
	sub.cancel()
}

// fanout delivers a frame to every current subscriber. A subscriber whose
// queue is full is canceled and removed; its websocket writer will then
// notice ctx.Done() and tear down the connection.
func (s *Session) fanout(frame ServerFrame) {
	s.mu.Lock()
	subs := make([]*subscriber, 0, len(s.subs))
	for sub := range s.subs {
		subs = append(subs, sub)
	}
	s.mu.Unlock()

	for _, sub := range subs {
		select {
		case sub.out <- frame:
		case <-sub.ctx.Done():
			// already going away; nothing to do
		default:
			s.removeSubscriber(sub)
		}
	}
}

// --- Errors ---

var (
	ErrInvalidDimensions = errors.New("session: cols/rows out of range")
	ErrSessionClosed     = errors.New("session: closed")
	ErrNotFound          = errors.New("session: not found")
	ErrInvalidShell      = errors.New("session: invalid shell")
)
