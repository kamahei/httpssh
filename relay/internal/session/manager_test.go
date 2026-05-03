package session

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"httpssh/relay/internal/conpty"
)

// fakePTY is a conpty.PTY stub used by the manager tests. Read blocks until
// Close is called; Wait returns once Close is called.
type fakePTY struct {
	mu       sync.Mutex
	closed   bool
	doneCh   chan struct{}
	readCh   chan []byte
	writes   [][]byte
	cols, rs uint16
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
		n := copy(b, data)
		return n, nil
	case <-p.doneCh:
		return 0, io.EOF
	}
}

func (p *fakePTY) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return 0, io.ErrClosedPipe
	}
	p.writes = append(p.writes, append([]byte{}, b...))
	return len(b), nil
}

func (p *fakePTY) Resize(c, r uint16) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cols, p.rs = c, r
	return nil
}

func (p *fakePTY) Wait() (int, error) {
	<-p.doneCh
	return 0, nil
}

func (p *fakePTY) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	close(p.doneCh)
	p.mu.Unlock()
	return nil
}

func newTestManager(t *testing.T, idle time.Duration) (*Manager, *clock) {
	t.Helper()
	c := &clock{now: time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC)}
	m := NewManager(Options{
		ScrollbackBytes: 4096,
		IdleTimeout:     idle,
		ReapInterval:    time.Hour, // disable timer-driven reap; we drive it manually
		Shells:          func(name string) (string, []string, error) { return "fake-shell", nil, nil },
		Spawn: func(exe string, args []string, cols, rows uint16) (conpty.PTY, error) {
			return newFakePTY(), nil
		},
		Now: c.Now,
	})
	t.Cleanup(m.Shutdown)
	return m, c
}

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func TestManager_CreateAndList(t *testing.T) {
	m, _ := newTestManager(t, time.Hour)

	s, err := m.Create("pwsh", 80, 24, "", -1)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if s.ID == "" {
		t.Fatal("session id is empty")
	}
	if list := m.List(); len(list) != 1 || list[0].ID != s.ID {
		t.Fatalf("list = %+v, want [%s]", list, s.ID)
	}
}

func TestManager_KillRemovesFromList(t *testing.T) {
	m, _ := newTestManager(t, time.Hour)
	s, err := m.Create("pwsh", 80, 24, "", -1)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := m.Kill(s.ID); err != nil {
		t.Fatalf("kill: %v", err)
	}
	// The session goroutines might race; give Manager.waitExit a moment.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(m.List()) == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("session still listed after kill")
}

func TestManager_Reap_KillsIdleSessions(t *testing.T) {
	m, c := newTestManager(t, 5*time.Minute)
	s, err := m.Create("pwsh", 80, 24, "", -1)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// First pass: not yet idle.
	if got := m.reapOnce(); got != 0 {
		t.Fatalf("reapOnce after 0 idle = %d, want 0", got)
	}

	// Advance the clock past the idle threshold.
	c.Advance(10 * time.Minute)

	if got := m.reapOnce(); got != 1 {
		t.Fatalf("reapOnce after 10m idle = %d, want 1", got)
	}

	// Wait for waitExit to delete from byID.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, err := m.Get(s.ID); err == ErrNotFound {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("session still in manager after reap")
}

func TestManager_Reap_PerSessionTimeout(t *testing.T) {
	// Manager default 1h, but the per-session override on `short`
	// is 5m so the reaper should pick it up before `long`.
	m, c := newTestManager(t, time.Hour)
	short, err := m.Create("pwsh", 80, 24, "short", 5*time.Minute)
	if err != nil {
		t.Fatalf("create short: %v", err)
	}
	long, err := m.Create("pwsh", 80, 24, "long", time.Hour)
	if err != nil {
		t.Fatalf("create long: %v", err)
	}

	c.Advance(10 * time.Minute)
	if got := m.reapOnce(); got != 1 {
		t.Fatalf("reapOnce after 10m = %d, want 1", got)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, err := m.Get(short.ID); err == ErrNotFound {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := m.Get(short.ID); err != ErrNotFound {
		t.Fatal("short session still listed after 10m idle")
	}
	if _, err := m.Get(long.ID); err != nil {
		t.Fatalf("long session unexpectedly removed: %v", err)
	}
}

func TestManager_Reap_UnlimitedSession(t *testing.T) {
	// A session created with idleTimeout == 0 is exempt from the
	// reaper regardless of how long it has been idle.
	m, c := newTestManager(t, time.Hour)
	s, err := m.Create("pwsh", 80, 24, "forever", 0)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	c.Advance(100 * time.Hour)
	if got := m.reapOnce(); got != 0 {
		t.Fatalf("reapOnce on unlimited session = %d, want 0", got)
	}
	if _, err := m.Get(s.ID); err != nil {
		t.Fatalf("unlimited session removed: %v", err)
	}
}

func TestManager_Reap_SkipsActiveSessions(t *testing.T) {
	m, c := newTestManager(t, 5*time.Minute)
	s, err := m.Create("pwsh", 80, 24, "", -1)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	frames, _, _ := s.Subscribe(context.Background())
	go func() {
		for range frames {
		}
	}()
	// give Subscribe a beat to register
	time.Sleep(20 * time.Millisecond)

	c.Advance(10 * time.Minute)
	if got := m.reapOnce(); got != 0 {
		t.Fatalf("reapOnce with one subscriber = %d, want 0", got)
	}
}

func TestManager_RejectsInvalidDimensions(t *testing.T) {
	m, _ := newTestManager(t, time.Hour)
	if _, err := m.Create("pwsh", MaxCols+1, 24, "", -1); err == nil {
		t.Fatal("expected ErrInvalidDimensions, got nil")
	}
	if _, err := m.Create("pwsh", 80, MaxRows+1, "", -1); err == nil {
		t.Fatal("expected ErrInvalidDimensions, got nil")
	}
}

func TestSession_WriteInputForwardsToPTY(t *testing.T) {
	m, _ := newTestManager(t, time.Hour)
	s, err := m.Create("pwsh", 80, 24, "", -1)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	pty := s.pty.(*fakePTY)

	if err := s.WriteInput([]byte("ls\r")); err != nil {
		t.Fatalf("write: %v", err)
	}
	pty.mu.Lock()
	got := pty.writes
	pty.mu.Unlock()
	if len(got) != 1 || string(got[0]) != "ls\r" {
		t.Fatalf("pty writes=%v want [\"ls\\r\"]", got)
	}
}

func TestSession_ResizeUpdatesPTYAndDimensions(t *testing.T) {
	m, _ := newTestManager(t, time.Hour)
	s, err := m.Create("pwsh", 80, 24, "", -1)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	pty := s.pty.(*fakePTY)

	if err := s.Resize(120, 40); err != nil {
		t.Fatalf("resize: %v", err)
	}
	if got := s.Info(); got.Cols != 120 || got.Rows != 40 {
		t.Fatalf("info dims=%dx%d want 120x40", got.Cols, got.Rows)
	}
	pty.mu.Lock()
	c, r := pty.cols, pty.rs
	pty.mu.Unlock()
	if c != 120 || r != 40 {
		t.Fatalf("pty dims=%dx%d want 120x40", c, r)
	}
}

func TestSession_ResizeRejectsBounds(t *testing.T) {
	m, _ := newTestManager(t, time.Hour)
	s, err := m.Create("pwsh", 80, 24, "", -1)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.Resize(0, 24); err != ErrInvalidDimensions {
		t.Fatalf("cols=0: err=%v want ErrInvalidDimensions", err)
	}
	if err := s.Resize(120, 0); err != ErrInvalidDimensions {
		t.Fatalf("rows=0: err=%v want ErrInvalidDimensions", err)
	}
	if err := s.Resize(MaxCols+1, 24); err != ErrInvalidDimensions {
		t.Fatalf("cols too big: err=%v want ErrInvalidDimensions", err)
	}
	if err := s.Resize(80, MaxRows+1); err != ErrInvalidDimensions {
		t.Fatalf("rows too big: err=%v want ErrInvalidDimensions", err)
	}
}

func TestSession_SubscribeReceivesReplayThenLive(t *testing.T) {
	m, _ := newTestManager(t, time.Hour)
	s, err := m.Create("pwsh", 80, 24, "", -1)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	pty := s.pty.(*fakePTY)

	// Pre-populate the scrollback by writing through the PTY pump.
	pty.readCh <- []byte("first-output")
	// Wait for pump to deliver into scrollback.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if s.scrollback.Size() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	frames, _, unsub := s.Subscribe(context.Background())
	defer unsub()

	select {
	case f := <-frames:
		if f.T != FrameReplay {
			t.Fatalf("first frame t=%q want %q", f.T, FrameReplay)
		}
		if f.D != "first-output" {
			t.Fatalf("replay d=%q want first-output", f.D)
		}
	case <-time.After(time.Second):
		t.Fatal("did not receive replay frame")
	}

	pty.readCh <- []byte("live-output")
	select {
	case f := <-frames:
		if f.T != FrameOut {
			t.Fatalf("second frame t=%q want %q", f.T, FrameOut)
		}
		if f.D != "live-output" {
			t.Fatalf("out d=%q want live-output", f.D)
		}
	case <-time.After(time.Second):
		t.Fatal("did not receive live out frame")
	}
}

func TestSession_FanoutToMultipleSubscribers(t *testing.T) {
	m, _ := newTestManager(t, time.Hour)
	s, err := m.Create("pwsh", 80, 24, "", -1)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	pty := s.pty.(*fakePTY)

	a, _, unsubA := s.Subscribe(context.Background())
	defer unsubA()
	b, _, unsubB := s.Subscribe(context.Background())
	defer unsubB()

	// Drain the initial replay frames.
	<-a
	<-b

	pty.readCh <- []byte("hello")

	got := func(ch <-chan ServerFrame) ServerFrame {
		select {
		case f := <-ch:
			return f
		case <-time.After(time.Second):
			t.Fatal("frame not received")
			return ServerFrame{}
		}
	}
	fa := got(a)
	fb := got(b)
	if fa.T != FrameOut || fb.T != FrameOut {
		t.Fatalf("expected FrameOut on both, got a=%q b=%q", fa.T, fb.T)
	}
	if fa.D != "hello" || fb.D != "hello" {
		t.Fatalf("payload mismatch: a=%q b=%q", fa.D, fb.D)
	}
}

func TestSession_UnsubscribeStopsDelivery(t *testing.T) {
	m, _ := newTestManager(t, time.Hour)
	s, err := m.Create("pwsh", 80, 24, "", -1)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	frames, done, unsub := s.Subscribe(context.Background())
	<-frames // replay
	unsub()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("done channel not closed after unsubscribe")
	}
}

func TestSession_PumpUpdatesCWDFromOSC(t *testing.T) {
	m, _ := newTestManager(t, time.Hour)
	s, err := m.Create("pwsh", 80, 24, "", -1)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	pty := s.pty.(*fakePTY)
	pty.readCh <- []byte("\x1b]9;9;C:\\Users\\foo\x07PS> ")

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if s.CWD() == `C:\Users\foo` {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("cwd never updated; got %q", s.CWD())
}

func TestSession_ExitClosesSubscribers(t *testing.T) {
	m, _ := newTestManager(t, time.Hour)
	s, err := m.Create("pwsh", 80, 24, "", -1)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	frames, done, unsub := s.Subscribe(context.Background())
	defer unsub()
	<-frames // replay

	if err := m.Kill(s.ID); err != nil {
		t.Fatalf("kill: %v", err)
	}

	deadline := time.After(2 * time.Second)
	gotExit := false
	for !gotExit {
		select {
		case f, ok := <-frames:
			if !ok {
				t.Fatal("frames closed before exit frame")
			}
			if f.T == FrameExit {
				gotExit = true
			}
		case <-done:
			// done can fire before we drain the exit frame; that's fine.
			gotExit = true
		case <-deadline:
			t.Fatal("never observed FrameExit / done")
		}
	}
}
