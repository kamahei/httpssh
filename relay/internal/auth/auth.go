// Package auth gates access to relay endpoints with a single, mandatory
// LAN bearer token.
//
// Auth model:
//
//   - The relay always requires `Authorization: Bearer <lan_bearer>` (or
//     the equivalent `?token=` query parameter on the WebSocket
//     handshake, where browsers cannot set custom headers).
//   - Cloudflare Access is treated as an outer edge-only layer. When the
//     relay is exposed through `cloudflared`, Cloudflare Access enforces
//     identity (Service Token for the mobile app, Google SSO for the
//     browser) BEFORE the request reaches the relay. The relay itself
//     does not look at any Cloudflare headers; it simply checks the
//     bearer.
//
// This is "what you have" (the bearer) plus "who you are" (Cloudflare
// Access at the edge). A leaked bearer alone cannot reach the relay
// from outside the LAN, and a compromised Cloudflare account cannot
// drive the relay without the bearer.
package auth

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
)

const (
	HeaderAuthz  = "Authorization"
	BearerPrefix = "Bearer "
)

// Config is the runtime configuration for the auth middleware.
type Config struct {
	// LANBearer is the shared bearer token required on every request.
	// Empty disables the relay entirely (every request returns 401).
	LANBearer string

	// Logger receives structured allow/deny events.
	Logger *slog.Logger
}

// Middleware returns an http.Handler middleware that enforces the policy.
func Middleware(cfg Config) func(http.Handler) http.Handler {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ok, code := evaluate(r, cfg)
			if !ok {
				cfg.Logger.Warn("auth denied",
					"event", "auth_denied",
					"path", r.URL.Path,
					"remote", r.RemoteAddr,
					"reason", code,
				)
				writeError(w, http.StatusUnauthorized, code, "missing or invalid credentials")
				return
			}
			cfg.Logger.Debug("auth allowed",
				"event", "auth_allowed",
				"path", r.URL.Path,
			)
			next.ServeHTTP(w, r)
		})
	}
}

func evaluate(r *http.Request, cfg Config) (ok bool, code string) {
	if cfg.LANBearer == "" {
		return false, "unauthorized"
	}
	got := bearerFromRequest(r)
	if got == "" {
		return false, "unauthorized"
	}
	if subtle.ConstantTimeCompare([]byte(got), []byte(cfg.LANBearer)) != 1 {
		return false, "unauthorized"
	}
	return true, ""
}

// bearerFromRequest extracts the LAN bearer from either the Authorization
// header (preferred) or the `token` query parameter. The query fallback
// exists because browsers cannot set arbitrary headers on a WebSocket
// handshake; same-origin requests can still authenticate via the URL.
func bearerFromRequest(r *http.Request) string {
	hdr := strings.TrimSpace(r.Header.Get(HeaderAuthz))
	if strings.HasPrefix(hdr, BearerPrefix) {
		return strings.TrimSpace(strings.TrimPrefix(hdr, BearerPrefix))
	}
	if t := r.URL.Query().Get("token"); t != "" {
		return t
	}
	return ""
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
