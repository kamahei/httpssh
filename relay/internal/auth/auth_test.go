package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMiddleware_BearerHeaderAllowed(t *testing.T) {
	mw := Middleware(Config{LANBearer: "secret-bearer-value-32bytes-or-more"})
	called := false
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))

	r := httptest.NewRequest("GET", "/api/health", nil)
	r.Header.Set("Authorization", "Bearer secret-bearer-value-32bytes-or-more")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != 200 {
		t.Fatalf("status=%d want 200", w.Code)
	}
	if !called {
		t.Fatalf("downstream handler not invoked")
	}
}

func TestMiddleware_BearerQueryAllowed(t *testing.T) {
	// Browsers cannot set Authorization on a WebSocket handshake; the
	// middleware must accept the bearer via ?token= as a fallback.
	mw := Middleware(Config{LANBearer: "secret-bearer-value-32bytes-or-more"})
	called := false
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))

	r := httptest.NewRequest("GET", "/api/sessions/abc/io?token=secret-bearer-value-32bytes-or-more", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != 200 {
		t.Fatalf("status=%d want 200", w.Code)
	}
	if !called {
		t.Fatalf("downstream not invoked for query-token request")
	}
}

func TestMiddleware_NoCredentialsRejected(t *testing.T) {
	mw := Middleware(Config{LANBearer: "secret"})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("downstream should not run")
	}))

	r := httptest.NewRequest("GET", "/api/health", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != 401 {
		t.Fatalf("status=%d want 401", w.Code)
	}
}

func TestMiddleware_WrongBearerRejected(t *testing.T) {
	mw := Middleware(Config{LANBearer: "right"})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("downstream should not run")
	}))

	r := httptest.NewRequest("GET", "/api/health", nil)
	r.Header.Set("Authorization", "Bearer wrong")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != 401 {
		t.Fatalf("status=%d want 401", w.Code)
	}
}

func TestMiddleware_EmptyBearerConfigRejectsAll(t *testing.T) {
	mw := Middleware(Config{LANBearer: ""})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("downstream should not run")
	}))

	r := httptest.NewRequest("GET", "/api/health", nil)
	r.Header.Set("Authorization", "Bearer anything")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != 401 {
		t.Fatalf("status=%d want 401", w.Code)
	}
}

func TestMiddleware_CloudflareHeadersIgnored(t *testing.T) {
	// The relay no longer inspects any Cf-* header. A request that
	// carries Cf-Access-Jwt-Assertion but no bearer must still 401.
	mw := Middleware(Config{LANBearer: "secret"})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("downstream should not run")
	}))

	r := httptest.NewRequest("GET", "/api/health", nil)
	r.Header.Set("Cf-Access-Jwt-Assertion", "eyJhbGciOiJSUzI1NiJ9.fake.fake")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != 401 {
		t.Fatalf("status=%d want 401", w.Code)
	}
}
