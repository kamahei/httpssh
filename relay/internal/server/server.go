// Package server wires HTTP routes and WebSocket handlers onto a session
// Manager. The server itself is unaware of ConPTY; that responsibility
// belongs to the session package.
package server

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"httpssh/relay/internal/auth"
	"httpssh/relay/internal/session"
)

// Server bundles dependencies and produces an http.Handler.
type Server struct {
	mgr       *session.Manager
	logger    *slog.Logger
	startedAt time.Time
	version   string
	mw        func(http.Handler) http.Handler
}

// Options configures Server.
type Options struct {
	Manager *session.Manager
	Auth    auth.Config
	Logger  *slog.Logger
	Version string
}

// New constructs a Server.
func New(opts Options) *Server {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Version == "" {
		opts.Version = "0.0.0-dev"
	}
	return &Server{
		mgr:       opts.Manager,
		logger:    opts.Logger,
		startedAt: time.Now(),
		version:   opts.Version,
		mw:        auth.Middleware(opts.Auth),
	}
}

// Handler returns the configured http.Handler.
//
// Routing has two halves:
//
//   - /api/* is gated by the auth middleware. Every API call must carry
//     the LAN bearer; Cloudflare Access is edge-only and not inspected by
//     the relay.
//   - /web/* and the root redirect to /web/ are served WITHOUT the auth
//     middleware. The static SPA shell needs to load before the user can
//     enter a bearer; gating the static files would create a chicken-and-
//     egg problem on the LAN path. The shell discloses no secrets, and
//     every meaningful operation it performs hits /api/* which is gated.
//
// Cloudflare-tunnelled requests are still authenticated at the edge by
// Cloudflare Access on whatever paths the operator configured.
func (s *Server) Handler() http.Handler {
	apiMux := http.NewServeMux()

	apiMux.HandleFunc("GET /api/health", s.handleHealth)
	apiMux.HandleFunc("GET /api/sessions", s.handleListSessions)
	apiMux.HandleFunc("POST /api/sessions", s.handleCreateSession)
	apiMux.HandleFunc("PATCH /api/sessions/{id}", s.handleRenameSession)
	apiMux.HandleFunc("DELETE /api/sessions/{id}", s.handleKillSession)
	apiMux.HandleFunc("GET /api/sessions/{id}/io", s.handleWebSocket)

	root := http.NewServeMux()
	root.Handle("/api/", s.mw(apiMux))
	root.Handle("/web/", staticHandler())
	root.HandleFunc("/", s.handleRoot)

	// Outermost: panic recovery wraps the access log so that a panic
	// inside any handler still produces both a 500 and a log line.
	return recoverMiddleware(s.logger)(accessLogMiddleware(s.logger)(root))
}

// --- Handlers ---

type healthResponse struct {
	Status        string `json:"status"`
	Version       string `json:"version"`
	UptimeSeconds int64  `json:"uptimeSeconds"`
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{
		Status:        "ok",
		Version:       s.version,
		UptimeSeconds: int64(time.Since(s.startedAt).Seconds()),
	})
}

type listResponse struct {
	Sessions []session.SessionInfo `json:"sessions"`
}

func (s *Server) handleListSessions(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, listResponse{Sessions: s.mgr.List()})
}

type createRequest struct {
	Shell string `json:"shell"`
	Cols  uint16 `json:"cols"`
	Rows  uint16 `json:"rows"`
	Title string `json:"title"`
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if req.Cols == 0 {
		req.Cols = 80
	}
	if req.Rows == 0 {
		req.Rows = 24
	}
	if req.Shell == "" {
		req.Shell = "pwsh"
	}

	sess, err := s.mgr.Create(req.Shell, req.Cols, req.Rows, req.Title)
	if err != nil {
		switch {
		case errors.Is(err, session.ErrInvalidDimensions):
			writeError(w, http.StatusBadRequest, "invalid_dimensions", err.Error())
		case errors.Is(err, session.ErrInvalidShell):
			writeError(w, http.StatusBadRequest, "invalid_shell", err.Error())
		default:
			writeError(w, http.StatusServiceUnavailable, "spawn_failed", err.Error())
		}
		return
	}

	w.Header().Set("Location", "/api/sessions/"+sess.ID)
	writeJSON(w, http.StatusCreated, sess.Info())
}

type renameRequest struct {
	Title string `json:"title"`
}

func (s *Server) handleRenameSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req renameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	info, err := s.mgr.Rename(id, req.Title)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleKillSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.mgr.Kill(id); err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path == "/" {
		http.Redirect(w, r, "/web/", http.StatusFound)
		return
	}
	http.NotFound(w, r)
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Error("encode response", "err", err)
	}
}

type errorBody struct {
	Error errorPayload `json:"error"`
}

type errorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorBody{Error: errorPayload{Code: code, Message: message}})
}
