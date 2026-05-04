//go:build windows

package main

import (
	"context"
	"os"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/term"
)

// enterRawMode flips the local console into "TUI mode": stdin echoes are
// off, line buffering is off, control sequences are passed through to
// the relay, and stdout interprets the ANSI escapes that ConPTY emits.
//
// The returned restore func is idempotent.
func enterRawMode() (func(), error) {
	stdinFd := int(os.Stdin.Fd())

	oldState, err := term.MakeRaw(stdinFd)
	if err != nil {
		return nil, err
	}

	// Output side: enable VT processing so the bytes the relay sends
	// (CSI sequences, OSC 9;9 hooks, etc.) actually move the cursor and
	// paint colors instead of being printed literally.
	stdoutHandle := windows.Handle(os.Stdout.Fd())
	var oldOutMode uint32
	haveOutMode := false
	if err := windows.GetConsoleMode(stdoutHandle, &oldOutMode); err == nil {
		newMode := oldOutMode | windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING | windows.DISABLE_NEWLINE_AUTO_RETURN
		_ = windows.SetConsoleMode(stdoutHandle, newMode)
		haveOutMode = true
	}

	var once bool
	return func() {
		if once {
			return
		}
		once = true
		_ = term.Restore(stdinFd, oldState)
		if haveOutMode {
			_ = windows.SetConsoleMode(stdoutHandle, oldOutMode)
		}
	}, nil
}

// getStdoutSize returns the current console dimensions (cols, rows). On
// failure it returns (0, 0, err).
func getStdoutSize() (uint16, uint16, error) {
	w, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return 0, 0, err
	}
	return uint16(w), uint16(h), nil
}

// watchResize emits {cols,rows} on ch every time the local console
// dimensions change. Windows lacks SIGWINCH, so we poll
// GetConsoleScreenBufferInfo via term.GetSize. 250 ms is fast enough
// for human-perceivable resize and cheap enough to ignore.
func watchResize(ctx context.Context, ch chan<- struct{ c, r uint16 }) func() {
	t := time.NewTicker(250 * time.Millisecond)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		var lastCols, lastRows uint16
		if c, r, err := getStdoutSize(); err == nil {
			lastCols, lastRows = c, r
		}
		for {
			select {
			case <-ctx.Done():
				t.Stop()
				return
			case <-t.C:
				c, r, err := getStdoutSize()
				if err != nil || c == 0 || r == 0 {
					continue
				}
				if c == lastCols && r == lastRows {
					continue
				}
				lastCols, lastRows = c, r
				select {
				case ch <- struct{ c, r uint16 }{c, r}:
				case <-ctx.Done():
					t.Stop()
					return
				}
			}
		}
	}()
	return func() {
		<-stopped
	}
}
