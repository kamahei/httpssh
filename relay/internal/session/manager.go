package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"httpssh/relay/internal/conpty"
)

// ShellResolver resolves a logical shell name ("pwsh", "powershell", "cmd")
// to an absolute executable path on the host plus any shell-specific
// bootstrap arguments the relay needs to spawn the shell with (for
// example, command-line flags that install the OSC 9;9 prompt wrapper
// used by the CWD tracker). It is injected so tests can avoid touching
// the filesystem.
type ShellResolver func(name string) (executable string, args []string, err error)

// PTYFactory builds a pseudo-console attached to a child process. The
// default uses conpty.Spawn; tests can swap in a stub that returns a fake
// PTY without touching the OS.
type PTYFactory func(executable string, args []string, cols, rows uint16) (conpty.PTY, error)

// Options tunes a Manager. Zero values pick reasonable defaults.
type Options struct {
	ScrollbackBytes int
	IdleTimeout     time.Duration
	ReapInterval    time.Duration // how often the GC goroutine looks for stale sessions; default 60s
	Shells          ShellResolver
	Spawn           PTYFactory // default conpty.Spawn
	Logger          *slog.Logger
	// Now is overridable for tests; defaults to time.Now.
	Now func() time.Time
}

// Manager owns all live sessions in the relay process.
type Manager struct {
	opts Options

	mu   sync.RWMutex
	byID map[string]*Session

	closed   bool
	reapDone chan struct{}
	stopReap context.CancelFunc
}

// NewManager constructs a Manager. A nil Logger uses slog.Default. The
// background idle-GC goroutine is started immediately; call Shutdown to
// stop it.
func NewManager(opts Options) *Manager {
	if opts.ScrollbackBytes <= 0 {
		opts.ScrollbackBytes = defaultScrollbackBytes
	}
	if opts.IdleTimeout <= 0 {
		opts.IdleTimeout = 24 * time.Hour
	}
	if opts.ReapInterval <= 0 {
		opts.ReapInterval = 60 * time.Second
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Spawn == nil {
		opts.Spawn = conpty.Spawn
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}

	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{
		opts:     opts,
		byID:     make(map[string]*Session),
		reapDone: make(chan struct{}),
		stopReap: cancel,
	}
	go m.runReaper(ctx)
	return m
}

// Create spawns a new session.
func (m *Manager) Create(shell string, cols, rows uint16, title string) (*Session, error) {
	if cols == 0 {
		cols = 80
	}
	if rows == 0 {
		rows = 24
	}
	if cols > MaxCols || rows > MaxRows {
		return nil, ErrInvalidDimensions
	}
	if m.opts.Shells == nil {
		return nil, fmt.Errorf("manager: no shell resolver configured")
	}
	exe, args, err := m.opts.Shells(shell)
	if err != nil {
		return nil, err
	}

	pty, err := m.opts.Spawn(exe, args, cols, rows)
	if err != nil {
		return nil, fmt.Errorf("manager: spawn: %w", err)
	}

	id := newID()
	now := m.opts.Now().UTC()
	if title == "" {
		title = fmt.Sprintf("%s %s", shellLabel(shell), now.Format("2006-01-02 15:04"))
	}

	s := &Session{
		ID:         id,
		Title:      title,
		Shell:      exe,
		CreatedAt:  now,
		cols:       cols,
		rows:       rows,
		pty:        pty,
		scrollback: NewRingBuffer(m.opts.ScrollbackBytes),
		subs:       make(map[*subscriber]struct{}),
		lastIO:     now,
		doneCh:     make(chan struct{}),
		cwdTracker: newCWDTracker(),
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		_ = pty.Close()
		return nil, fmt.Errorf("manager: shutting down")
	}
	m.byID[id] = s
	m.mu.Unlock()

	go m.pump(s)
	go m.waitExit(s)

	m.opts.Logger.Info("session created",
		"event", "session_create",
		"session_id", s.ID,
		"shell", s.Shell,
		"cols", cols, "rows", rows,
	)

	return s, nil
}

// Get returns the session with the given ID, or ErrNotFound.
func (m *Manager) Get(id string) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.byID[id]
	if !ok {
		return nil, ErrNotFound
	}
	return s, nil
}

// List returns metadata snapshots for every live session.
func (m *Manager) List() []SessionInfo {
	m.mu.RLock()
	out := make([]SessionInfo, 0, len(m.byID))
	for _, s := range m.byID {
		out = append(out, s.Info())
	}
	m.mu.RUnlock()
	return out
}

// Rename updates a session's title.
func (m *Manager) Rename(id, title string) (SessionInfo, error) {
	s, err := m.Get(id)
	if err != nil {
		return SessionInfo{}, err
	}
	s.mu.Lock()
	s.Title = title
	s.mu.Unlock()
	return s.Info(), nil
}

// Kill terminates a session and removes it from the manager.
func (m *Manager) Kill(id string) error {
	s, err := m.Get(id)
	if err != nil {
		return err
	}
	if err := s.Kill(); err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.byID, id)
	m.mu.Unlock()
	return nil
}

// Shutdown kills every session, stops the GC goroutine, and prevents new
// sessions from being created. Idempotent.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	all := make([]*Session, 0, len(m.byID))
	for _, s := range m.byID {
		all = append(all, s)
	}
	m.byID = nil
	m.mu.Unlock()

	for _, s := range all {
		_ = s.Kill()
	}
	if m.stopReap != nil {
		m.stopReap()
		<-m.reapDone
	}
}

// runReaper periodically kills sessions that have had zero subscribers and
// no I/O for longer than IdleTimeout.
func (m *Manager) runReaper(ctx context.Context) {
	defer close(m.reapDone)
	t := time.NewTicker(m.opts.ReapInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.reapOnce()
		}
	}
}

// reapOnce performs a single GC pass. Exposed for tests.
func (m *Manager) reapOnce() int {
	now := m.opts.Now()
	cutoff := now.Add(-m.opts.IdleTimeout)

	m.mu.RLock()
	candidates := make([]*Session, 0)
	for _, s := range m.byID {
		s.mu.Lock()
		stale := !s.closed && len(s.subs) == 0 && s.lastIO.Before(cutoff)
		s.mu.Unlock()
		if stale {
			candidates = append(candidates, s)
		}
	}
	m.mu.RUnlock()

	for _, s := range candidates {
		m.opts.Logger.Info("session reaped",
			"event", "session_reaped",
			"session_id", s.ID,
			"idle_for", now.Sub(s.lastIO).String(),
		)
		_ = s.Kill()
		m.mu.Lock()
		delete(m.byID, s.ID)
		m.mu.Unlock()
	}
	return len(candidates)
}

// pump reads from the session's PTY and fans out to subscribers + scrollback.
func (m *Manager) pump(s *Session) {
	buf := make([]byte, 32*1024)
	for {
		n, err := s.pty.Read(buf)
		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])
			_, _ = s.scrollback.Write(data)
			s.mu.Lock()
			s.lastIO = time.Now()
			s.mu.Unlock()
			for _, p := range s.cwdTracker.feed(data) {
				s.setCWD(p)
			}
			s.fanout(ServerFrame{T: FrameOut, D: string(data)})
		}
		if err != nil {
			m.opts.Logger.Info("session pump exit",
				"event", "session_pump_exit",
				"session_id", s.ID,
				"err", err,
			)
			return
		}
	}
}

// waitExit waits for the underlying process to exit, signals subscribers,
// and removes the session from the manager.
func (m *Manager) waitExit(s *Session) {
	code, werr := s.pty.Wait()
	s.mu.Lock()
	c := code
	s.exitCode = &c
	s.exitErr = werr
	s.closed = true
	s.mu.Unlock()

	s.fanout(ServerFrame{T: FrameExit, Code: &c})

	// Cancel every subscriber so the websocket writer goroutines exit.
	s.mu.Lock()
	subs := make([]*subscriber, 0, len(s.subs))
	for sub := range s.subs {
		subs = append(subs, sub)
	}
	s.subs = nil
	s.mu.Unlock()
	for _, sub := range subs {
		sub.cancel()
	}

	close(s.doneCh)

	m.mu.Lock()
	delete(m.byID, s.ID)
	m.mu.Unlock()

	m.opts.Logger.Info("session ended",
		"event", "session_end",
		"session_id", s.ID,
		"exit_code", c,
		"err", werr,
	)
}

// shellLabel returns a friendly label for log/title use.
func shellLabel(name string) string {
	switch name {
	case "powershell":
		return "powershell"
	case "cmd":
		return "cmd"
	default:
		return "pwsh"
	}
}

// newID returns a 16-byte random hex string. Cryptographic strength is not
// strictly required (sessions live in-memory), but it removes any chance of
// collision and is forgery-resistant.
func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Should never happen; panic surfaces a misconfigured RNG loudly.
		panic(fmt.Errorf("rand.Read: %w", err))
	}
	return hex.EncodeToString(b[:])
}
