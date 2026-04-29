package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"httpssh/relay/internal/auth"
	"httpssh/relay/internal/config"
	"httpssh/relay/internal/fileapi"
	"httpssh/relay/internal/server"
	"httpssh/relay/internal/session"
	relaysvc "httpssh/relay/internal/svc"
)

var version = "0.0.0-dev"

func main() {
	configPath := flag.String("config", "", "path to config.yaml (overrides default values; flags override config)")
	addr := flag.String("listen", "", "host:port to listen on (overrides config)")
	bearer := flag.String("bearer", "", "LAN bearer token (overrides config). When empty and config has no bearer, a random token is generated and printed")
	idleTimeout := flag.Duration("idle-timeout", 0, "kill an idle session after this long (overrides config)")
	scrollbackBytes := flag.Int("scrollback-bytes", 0, "per-session scrollback ring size (overrides config)")
	logLevel := flag.String("log-level", "", "log level: debug, info, warn, error (overrides config)")
	shell := flag.String("shell", "", "default shell: auto, pwsh, powershell, cmd (overrides config)")
	flag.Parse()

	cfg, err := buildConfig(*configPath, addr, bearer, idleTimeout, scrollbackBytes, logLevel, shell)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(2)
	}

	logger := newLogger(cfg.Log.Level)
	slog.SetDefault(logger)

	// Detect whether we were started by the Windows Service Control
	// Manager. If so, run inside the service handler; otherwise act as a
	// normal foreground process driven by SIGINT/SIGTERM.
	inService, err := relaysvc.IsWindowsService()
	if err != nil {
		logger.Error("could not determine service mode", "err", err)
		os.Exit(1)
	}

	run := func(ctx context.Context) error { return runRelay(ctx, cfg, logger) }

	if inService {
		if err := relaysvc.Run(logger, run); err != nil {
			logger.Error("service run failed", "err", err)
			os.Exit(1)
		}
		return
	}

	// Foreground / interactive mode.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("relay exited with error", "err", err)
		os.Exit(1)
	}
}

// buildConfig loads config.yaml then overlays explicit CLI flag values.
// Flags that the user did not pass are detected via flag.Visit and left
// alone, so config-file values win for those.
func buildConfig(configPath string, addr, bearer *string, idleTimeout *time.Duration, scrollbackBytes *int, logLevel, shell *string) (*config.Config, error) {
	setFlags := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { setFlags[f.Name] = true })

	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, fmt.Errorf("config error: %w", err)
	}

	if setFlags["listen"] {
		cfg.Listen = *addr
	}
	if setFlags["bearer"] {
		cfg.Auth.LANBearer = *bearer
	}
	if setFlags["idle-timeout"] {
		cfg.Session.IdleTimeout = *idleTimeout
	}
	if setFlags["scrollback-bytes"] {
		cfg.Session.ScrollbackBytes = *scrollbackBytes
	}
	if setFlags["log-level"] {
		cfg.Log.Level = *logLevel
	}
	if setFlags["shell"] {
		cfg.Shell = *shell
	}

	if cfg.Auth.LANBearer == "" {
		var b [32]byte
		if _, err := rand.Read(b[:]); err != nil {
			return nil, fmt.Errorf("rand.Read: %w", err)
		}
		cfg.Auth.LANBearer = hex.EncodeToString(b[:])
		// Only print to stderr in interactive (no --config) mode; under a
		// service, stderr is unattended and printing a generated bearer is
		// useless. (Operators running as a service must set lan_bearer in
		// config.yaml.)
		if configPath == "" && !setFlags["bearer"] {
			fmt.Fprintf(os.Stderr, "\n>>> Generated LAN bearer: %s\n>>> (use this in the web client's Settings dialog)\n\n", cfg.Auth.LANBearer)
		}
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// runRelay is the actual run loop, used by both interactive mode and the
// Windows service handler. It blocks until ctx is canceled, then performs
// graceful shutdown and returns.
func runRelay(ctx context.Context, cfg *config.Config, logger *slog.Logger) error {
	fileSvc, err := newFileService(cfg)
	if err != nil {
		return err
	}

	mgr := session.NewManager(session.Options{
		ScrollbackBytes: cfg.Session.ScrollbackBytes,
		IdleTimeout:     cfg.Session.IdleTimeout,
		ReapInterval:    cfg.Session.ReapInterval,
		Shells:          shellResolverFor(cfg.Shell),
		Logger:          logger,
	})

	srv := server.New(server.Options{
		Manager: mgr,
		Auth: auth.Config{
			LANBearer: cfg.Auth.LANBearer,
			Logger:    logger,
		},
		FileService: fileSvc,
		Logger:      logger,
		Version:     version,
	})

	httpSrv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	listenErr := make(chan error, 1)
	go func() {
		logger.Info("relay listening", "event", "listen", "addr", cfg.Listen, "version", version)
		err := httpSrv.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			listenErr <- err
			return
		}
		listenErr <- nil
	}()

	select {
	case <-ctx.Done():
	case err := <-listenErr:
		// Listen failed to start (or crashed) before ctx was canceled.
		mgr.Shutdown()
		if err != nil {
			return fmt.Errorf("listen failed: %w", err)
		}
		return nil
	}

	logger.Info("shutdown requested", "event", "shutdown_request")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	mgr.Shutdown()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "err", err)
		return err
	}
	logger.Info("shutdown complete", "event", "shutdown_done")
	return nil
}

func newFileService(cfg *config.Config) (*fileapi.Service, error) {
	roots := make([]fileapi.RootConfig, 0, len(cfg.Files.Roots))
	for _, root := range cfg.Files.Roots {
		roots = append(roots, fileapi.RootConfig{
			ID:   root.ID,
			Name: root.Name,
			Path: root.Path,
		})
	}
	svc, err := fileapi.NewService(fileapi.Config{
		Roots:        roots,
		MaxFileBytes: cfg.Files.MaxFileBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("file service: %w", err)
	}
	return svc, nil
}

// shellResolverFor returns a session.ShellResolver that always passes the
// configured default through unless the caller explicitly asked for a
// different shell. Empty maps to "auto".
func shellResolverFor(defaultShell string) session.ShellResolver {
	if defaultShell == "" {
		defaultShell = "auto"
	}
	return func(name string) (string, error) {
		if name == "" {
			name = defaultShell
		}
		return config.ResolveShell(name)
	}
}

func newLogger(level string) *slog.Logger {
	var lv slog.Level
	switch level {
	case "debug":
		lv = slog.LevelDebug
	case "info", "":
		lv = slog.LevelInfo
	case "warn":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		lv = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lv}))
}
