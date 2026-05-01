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
	"httpssh/relay/internal/fileapi"
	"httpssh/relay/internal/session"
)

// Server bundles dependencies and produces an http.Handler.
type Server struct {
	mgr       *session.Manager
	logger    *slog.Logger
	startedAt time.Time
	version   string
	mw        func(http.Handler) http.Handler
	files     *fileapi.Service
}

// Options configures Server.
type Options struct {
	Manager     *session.Manager
	Auth        auth.Config
	FileService *fileapi.Service
	Logger      *slog.Logger
	Version     string
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
		files:     opts.FileService,
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
	apiMux.HandleFunc("GET /api/sessions/{id}", s.handleGetSession)
	apiMux.HandleFunc("PATCH /api/sessions/{id}", s.handleRenameSession)
	apiMux.HandleFunc("DELETE /api/sessions/{id}", s.handleKillSession)
	apiMux.HandleFunc("GET /api/sessions/{id}/io", s.handleWebSocket)
	apiMux.HandleFunc("GET /api/files/roots", s.handleFileRoots)
	apiMux.HandleFunc("GET /api/files/list", s.handleFileList)
	apiMux.HandleFunc("GET /api/files/read", s.handleFileRead)
	apiMux.HandleFunc("GET /api/sessions/{id}/files/list", s.handleSessionFileList)
	apiMux.HandleFunc("GET /api/sessions/{id}/files/read", s.handleSessionFileRead)

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

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess, err := s.mgr.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, sess.Info())
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

type fileRootsResponse struct {
	Roots []fileapi.RootInfo `json:"roots"`
}

func (s *Server) handleFileRoots(w http.ResponseWriter, _ *http.Request) {
	roots := s.files.Roots()
	if roots == nil {
		roots = []fileapi.RootInfo{}
	}
	writeJSON(w, http.StatusOK, fileRootsResponse{Roots: roots})
}

func (s *Server) handleFileList(w http.ResponseWriter, r *http.Request) {
	if s.files == nil {
		s.writeFileError(w, fileapi.ErrDisabled)
		return
	}
	root := r.URL.Query().Get("root")
	if root == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "root is required")
		return
	}
	result, err := s.files.List(root, r.URL.Query().Get("path"))
	if err != nil {
		s.writeFileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleFileRead(w http.ResponseWriter, r *http.Request) {
	if s.files == nil {
		s.writeFileError(w, fileapi.ErrDisabled)
		return
	}
	root := r.URL.Query().Get("root")
	if root == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "root is required")
		return
	}
	result, err := s.files.Read(root, r.URL.Query().Get("path"))
	if err != nil {
		s.writeFileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// handleSessionFileList lists the directory rooted at the session's
// last-known working directory. Path is interpreted relative to the
// CWD; absolute paths are accepted only when they resolve under the
// CWD. Navigation above the CWD is rejected; to browse a different
// location, the operator types `cd` in the shell and re-opens the
// browser, which re-reads the (now updated) CWD.
func (s *Server) handleSessionFileList(w http.ResponseWriter, r *http.Request) {
	if s.files == nil {
		s.writeFileError(w, fileapi.ErrDisabled)
		return
	}
	id := r.PathValue("id")
	sess, err := s.mgr.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	cwd := sess.CWD()
	if cwd == "" {
		writeError(w, http.StatusConflict, "cwd_unknown", "shell has not yet reported a working directory")
		return
	}
	result, err := s.files.ListAt(cwd, r.URL.Query().Get("path"))
	if err != nil {
		s.writeFileError(w, err)
		return
	}
	result.Root = sessionRootID(id)
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleSessionFileRead(w http.ResponseWriter, r *http.Request) {
	if s.files == nil {
		s.writeFileError(w, fileapi.ErrDisabled)
		return
	}
	id := r.PathValue("id")
	sess, err := s.mgr.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	cwd := sess.CWD()
	if cwd == "" {
		writeError(w, http.StatusConflict, "cwd_unknown", "shell has not yet reported a working directory")
		return
	}
	result, err := s.files.ReadAt(cwd, r.URL.Query().Get("path"))
	if err != nil {
		s.writeFileError(w, err)
		return
	}
	result.Root = sessionRootID(id)
	writeJSON(w, http.StatusOK, result)
}

func sessionRootID(id string) string { return "session:" + id }

func (s *Server) writeFileError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, fileapi.ErrDisabled):
		writeError(w, http.StatusNotFound, "files_disabled", "file browsing is not configured")
	case errors.Is(err, fileapi.ErrRootNotFound):
		writeError(w, http.StatusNotFound, "root_not_found", err.Error())
	case errors.Is(err, fileapi.ErrInvalidBase):
		writeError(w, http.StatusConflict, "cwd_invalid", err.Error())
	case errors.Is(err, fileapi.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, fileapi.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden", err.Error())
	case errors.Is(err, fileapi.ErrNotDirectory):
		writeError(w, http.StatusBadRequest, "not_directory", err.Error())
	case errors.Is(err, fileapi.ErrNotText):
		writeError(w, http.StatusBadRequest, "not_text", err.Error())
	case errors.Is(err, fileapi.ErrTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "file_too_large", err.Error())
	default:
		s.logger.Error("file api failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "file operation failed")
	}
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
