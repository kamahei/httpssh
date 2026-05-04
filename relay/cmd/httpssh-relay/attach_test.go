package main

import (
	"net/url"
	"strings"
	"testing"
)

func TestNormalizeRelayURL(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"127.0.0.1:18822", "http://127.0.0.1:18822", false},
		{"http://127.0.0.1:18822", "http://127.0.0.1:18822", false},
		{"https://relay.example.com", "https://relay.example.com", false},
		{"https://relay.example.com:443/", "https://relay.example.com:443/", false},
		{"", "", true},
		{"   ", "", true},
		{"ftp://nope", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := normalizeRelayURL(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got nil (url=%v)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.String() != tc.want {
				t.Fatalf("got=%q want=%q", got.String(), tc.want)
			}
		})
	}
}

func TestBuildAttachWSURL(t *testing.T) {
	base, err := url.Parse("https://relay.example.com:443")
	if err != nil {
		t.Fatalf("base parse: %v", err)
	}
	got, err := buildAttachWSURL(base, "abc123", "secret-bearer", true)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.HasPrefix(got, "wss://relay.example.com:443/api/sessions/abc123/io?") {
		t.Fatalf("unexpected URL prefix: %s", got)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	q := u.Query()
	if q.Get("token") != "secret-bearer" {
		t.Fatalf("token query: got %q want secret-bearer", q.Get("token"))
	}
	if q.Get("role") != "host" {
		t.Fatalf("role query: got %q want host", q.Get("role"))
	}

	// hostRole=false should omit the role param.
	got2, err := buildAttachWSURL(base, "abc123", "secret-bearer", false)
	if err != nil {
		t.Fatalf("build2: %v", err)
	}
	u2, _ := url.Parse(got2)
	if u2.Query().Get("role") != "" {
		t.Fatalf("role should be empty when hostRole=false, got %q", u2.Query().Get("role"))
	}

	// http base → ws scheme
	httpBase, _ := url.Parse("http://127.0.0.1:18822")
	got3, err := buildAttachWSURL(httpBase, "x", "y", true)
	if err != nil {
		t.Fatalf("build3: %v", err)
	}
	if !strings.HasPrefix(got3, "ws://127.0.0.1:18822/api/sessions/x/io?") {
		t.Fatalf("ws scheme not applied: %s", got3)
	}

	// Empty session ID is rejected.
	if _, err := buildAttachWSURL(base, "", "y", true); err == nil {
		t.Fatalf("expected error for empty session id")
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", "a", "b"); got != "a" {
		t.Fatalf("got %q want a", got)
	}
	if got := firstNonEmpty("first", "second"); got != "first" {
		t.Fatalf("got %q want first", got)
	}
	if got := firstNonEmpty(""); got != "" {
		t.Fatalf("got %q want empty", got)
	}
}

func TestAbbrev(t *testing.T) {
	if got := abbrev("short", 12); got != "short" {
		t.Fatalf("got %q want short", got)
	}
	got := abbrev("0123456789abcdef", 8)
	if !strings.HasPrefix(got, "01234567") {
		t.Fatalf("prefix wrong: %q", got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("suffix wrong: %q", got)
	}
}
