//go:build windows

package conpty

import (
	"context"
	"fmt"

	wcp "github.com/UserExistsError/conpty"
)

type winPTY struct {
	cp *wcp.ConPty
}

func spawn(executable string, args []string, cols, rows uint16) (PTY, error) {
	cmdline := executable
	for _, a := range args {
		cmdline += " " + quote(a)
	}

	cp, err := wcp.Start(cmdline, wcp.ConPtyDimensions(int(cols), int(rows)))
	if err != nil {
		return nil, fmt.Errorf("conpty: start %q: %w", executable, err)
	}
	return &winPTY{cp: cp}, nil
}

func (p *winPTY) Read(b []byte) (int, error)  { return p.cp.Read(b) }
func (p *winPTY) Write(b []byte) (int, error) { return p.cp.Write(b) }

func (p *winPTY) Resize(cols, rows uint16) error {
	if err := p.cp.Resize(int(cols), int(rows)); err != nil {
		return fmt.Errorf("conpty: resize: %w", err)
	}
	return nil
}

func (p *winPTY) Wait() (int, error) {
	code, err := p.cp.Wait(context.Background())
	if err != nil {
		return -1, fmt.Errorf("conpty: wait: %w", err)
	}
	return int(code), nil
}

func (p *winPTY) Close() error {
	return p.cp.Close()
}

// quote applies cmd.exe-friendly quoting for arguments containing spaces or
// special characters. A full Windows command-line escaper would be overkill
// here; the relay only spawns shells with controlled argument lists.
func quote(s string) string {
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '"' {
			return `"` + s + `"`
		}
	}
	return s
}
