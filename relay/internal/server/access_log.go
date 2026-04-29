package server

import (
	"bufio"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// accessLogMiddleware emits one INFO log line per completed request,
// with method, path, status, bytes, and duration. Useful for
// correlating client-side errors (browser DevTools, mobile network
// failures) with relay behavior, especially when Cloudflare sits in
// front of the relay and might intercept or alter requests.
func accessLogMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			logger.Info("http request",
				"event", "http_request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"bytes", rec.bytes,
				"duration_ms", time.Since(start).Milliseconds(),
				"remote", r.RemoteAddr,
				"ua", r.Header.Get("User-Agent"),
				"cf_ray", r.Header.Get("Cf-Ray"),
			)
		})
	}
}

// statusRecorder wraps http.ResponseWriter to capture the status code
// and the number of body bytes written. It also forwards Hijack() so
// the WebSocket upgrade in handleWebSocket continues to work.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	bytes       int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if r.wroteHeader {
		return
	}
	r.status = code
	r.wroteHeader = true
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(p []byte) (int, error) {
	if !r.wroteHeader {
		r.wroteHeader = true
	}
	n, err := r.ResponseWriter.Write(p)
	r.bytes += n
	return n, err
}

// Hijack lets WebSocket upgrades through. The coder/websocket library
// uses http.Hijacker on Go versions where it is still required.
func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return hj.Hijack()
}
