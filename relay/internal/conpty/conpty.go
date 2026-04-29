// Package conpty exposes a small interface around the Windows ConPTY API.
//
// The interface is OS-agnostic so that callers can be tested on any platform;
// the real implementation in conpty_windows.go is only compiled on Windows,
// and the stub in conpty_other.go fails fast with ErrUnsupported elsewhere.
package conpty

import (
	"errors"
	"io"
)

var ErrUnsupported = errors.New("conpty: pseudo-console requires Windows 10 1809 or later")

// PTY represents a live pseudo-console attached to a child process.
type PTY interface {
	io.ReadWriteCloser
	Resize(cols, rows uint16) error
	Wait() (exitCode int, err error)
}

// Spawn creates a pseudo-console of the given size, starts the executable,
// and returns a PTY bound to that process. The caller must Close the PTY
// when finished, which terminates the child process.
func Spawn(executable string, args []string, cols, rows uint16) (PTY, error) {
	return spawn(executable, args, cols, rows)
}
