package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
)

// recoverMiddleware catches panics that escape any downstream handler,
// logs the panic plus a stack trace, and (when the response has not yet
// started) sends a 500 to the client. A WebSocket upgrade that has
// already started cannot be salvaged; the panic is logged and the
// connection drops.
func recoverMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					if rec == http.ErrAbortHandler {
						// Re-panic so net/http's standard handling kicks in.
						panic(rec)
					}
					logger.Error("panic in handler",
						"event", "handler_panic",
						"panic", fmt.Sprintf("%v", rec),
						"path", r.URL.Path,
						"method", r.Method,
						"remote", r.RemoteAddr,
						"stack", string(debug.Stack()),
					)
					// Best effort: try to send a 500. If the headers were
					// already flushed, the WriteHeader call is a no-op and
					// the client will see a half-written response.
					rw, ok := w.(interface{ WriteHeader(int) })
					if ok {
						rw.WriteHeader(http.StatusInternalServerError)
					}
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
