//go:build windows

package svc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	wsvc "golang.org/x/sys/windows/svc"
)

// IsWindowsService returns true when the process was started by the
// Service Control Manager.
func IsWindowsService() (bool, error) {
	return wsvc.IsWindowsService()
}

// Run is invoked by main when the process is started as a service. It
// blocks until the SCM tells the service to stop, at which point the
// supplied ctx is canceled and the supplied runRelay returns. runRelay
// must respect ctx cancellation and return promptly.
func Run(logger *slog.Logger, runRelay func(ctx context.Context) error) error {
	h := &handler{logger: logger, run: runRelay}
	return wsvc.Run(ServiceName, h)
}

type handler struct {
	logger *slog.Logger
	run    func(ctx context.Context) error
}

func (h *handler) Execute(args []string, r <-chan wsvc.ChangeRequest, status chan<- wsvc.Status) (svcSpecificEC bool, exitCode uint32) {
	status <- wsvc.Status{State: wsvc.StartPending}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- h.run(ctx) }()

	status <- wsvc.Status{State: wsvc.Running, Accepts: wsvc.AcceptStop | wsvc.AcceptShutdown}

loop:
	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case wsvc.Interrogate:
				status <- c.CurrentStatus
			case wsvc.Stop, wsvc.Shutdown:
				h.logger.Info("service stop requested", "event", "service_stop", "cmd", uint32(c.Cmd))
				status <- wsvc.Status{State: wsvc.StopPending}
				cancel()
				break loop
			default:
				h.logger.Warn("unexpected service control", "event", "service_unexpected_ctrl", "cmd", uint32(c.Cmd))
			}
		case err := <-errCh:
			if err != nil && !errors.Is(err, context.Canceled) {
				h.logger.Error("relay exited with error", "event", "relay_exit_error", "err", err)
				status <- wsvc.Status{State: wsvc.Stopped}
				return true, 1
			}
			break loop
		}
	}

	// Wait for runRelay to actually exit.
	if err := <-errCh; err != nil && !errors.Is(err, context.Canceled) {
		h.logger.Error("relay shutdown error", "err", fmt.Sprintf("%v", err))
	}
	status <- wsvc.Status{State: wsvc.Stopped}
	return false, 0
}
