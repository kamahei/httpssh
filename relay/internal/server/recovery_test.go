package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRecoverMiddleware_CatchesPanic(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	handler := recoverMiddleware(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("kaboom")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d want 500", rec.Code)
	}
}

func TestRecoverMiddleware_PassesThroughNonPanic(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	handler := recoverMiddleware(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTeapot {
		t.Fatalf("status=%d want 418", rec.Code)
	}
}

func TestRecoverMiddleware_RepanicsHTTPAbort(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	handler := recoverMiddleware(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected http.ErrAbortHandler to propagate")
		} else if r != http.ErrAbortHandler {
			t.Fatalf("re-panicked with unexpected value: %v", r)
		}
	}()
	handler.ServeHTTP(rec, req)
}
