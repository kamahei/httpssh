//go:build !windows

package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/term"
)

func enterRawMode() (func(), error) {
	stdinFd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(stdinFd)
	if err != nil {
		return nil, err
	}
	var once bool
	return func() {
		if once {
			return
		}
		once = true
		_ = term.Restore(stdinFd, oldState)
	}, nil
}

func getStdoutSize() (uint16, uint16, error) {
	w, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return 0, 0, err
	}
	return uint16(w), uint16(h), nil
}

// watchResize forwards SIGWINCH-driven resize events. Returns a stop
// function that is safe to call after ctx cancellation.
func watchResize(ctx context.Context, ch chan<- struct{ c, r uint16 }) func() {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGWINCH)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			select {
			case <-ctx.Done():
				signal.Stop(sig)
				return
			case <-sig:
				c, r, err := getStdoutSize()
				if err != nil || c == 0 || r == 0 {
					continue
				}
				select {
				case ch <- struct{ c, r uint16 }{c, r}:
				case <-ctx.Done():
					signal.Stop(sig)
					return
				}
			}
		}
	}()
	return func() {
		<-stopped
	}
}
