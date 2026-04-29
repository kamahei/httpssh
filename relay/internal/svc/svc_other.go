//go:build !windows

package svc

import (
	"context"
	"errors"
	"log/slog"
)

// IsWindowsService is always false on non-Windows platforms.
func IsWindowsService() (bool, error) { return false, nil }

// Run is unsupported off Windows; the relay does not target other OSes
// for service deployment.
func Run(logger *slog.Logger, runRelay func(ctx context.Context) error) error {
	_ = logger
	_ = runRelay
	return errors.New("svc: Windows-only")
}
