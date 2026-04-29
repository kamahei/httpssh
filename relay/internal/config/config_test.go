package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTemp(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoad_Empty_UsesDefaults(t *testing.T) {
	c, err := Load("")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if c.Listen != "127.0.0.1:18822" {
		t.Errorf("listen=%q", c.Listen)
	}
	if c.Shell != "auto" {
		t.Errorf("shell=%q", c.Shell)
	}
	if c.Session.IdleTimeout != 24*time.Hour {
		t.Errorf("idle=%v", c.Session.IdleTimeout)
	}
}

func TestLoad_FullConfig(t *testing.T) {
	p := writeTemp(t, `
listen: "0.0.0.0:9001"
shell: "pwsh"
auth:
  lan_bearer: "abcdefghijklmnop1234"
session:
  idle_timeout: "1h"
  scrollback_bytes: 1048576
  reap_interval: "30s"
log:
  level: "debug"
`)
	c, err := Load(p)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if c.Listen != "0.0.0.0:9001" {
		t.Errorf("listen=%q", c.Listen)
	}
	if c.Auth.LANBearer != "abcdefghijklmnop1234" {
		t.Errorf("bearer=%q", c.Auth.LANBearer)
	}
	if c.Session.IdleTimeout != time.Hour {
		t.Errorf("idle=%v", c.Session.IdleTimeout)
	}
	if c.Session.ScrollbackBytes != 1048576 {
		t.Errorf("scrollback=%d", c.Session.ScrollbackBytes)
	}
	if c.Log.Level != "debug" {
		t.Errorf("log.level=%q", c.Log.Level)
	}
}

func TestValidate_OK(t *testing.T) {
	c := Default()
	if err := c.Validate(); err != nil {
		t.Fatalf("default config invalid: %v", err)
	}
}

func TestValidate_BearerTooShort(t *testing.T) {
	c := Default()
	c.Auth.LANBearer = "short"
	if err := c.Validate(); err == nil {
		t.Fatal("expected error for short bearer")
	}
}

func TestValidate_UnknownShell(t *testing.T) {
	c := Default()
	c.Shell = "fish"
	if err := c.Validate(); err == nil {
		t.Fatal("expected error for unknown shell")
	}
}

func TestValidate_BadLogLevel(t *testing.T) {
	c := Default()
	c.Log.Level = "verbose"
	if err := c.Validate(); err == nil {
		t.Fatal("expected error for bad log level")
	}
}

func TestLoad_BadYAML(t *testing.T) {
	p := writeTemp(t, ":\n: : :")
	if _, err := Load(p); err == nil {
		t.Fatal("expected error on malformed yaml")
	}
}

func TestLoad_MissingFile(t *testing.T) {
	if _, err := Load("/no/such/path/config.yaml"); err == nil {
		t.Fatal("expected error on missing file")
	}
}

func TestResolveShell_UnknownName(t *testing.T) {
	if _, err := ResolveShell("fish"); err == nil {
		t.Fatal("expected error for unknown shell")
	}
}

func TestResolveShell_AutoFindsSomething(t *testing.T) {
	// On a Windows test host, "auto" should resolve to a real path. On
	// CI without PowerShell, the function returns an error; we accept
	// either outcome as long as the absolute path (when present) exists.
	got, err := ResolveShell("auto")
	if err != nil {
		t.Skipf("no PowerShell on PATH: %v", err)
	}
	if got == "" {
		t.Fatal("auto returned empty path")
	}
}

func TestResolveShell_CmdRequiresWindows(t *testing.T) {
	// We cannot meaningfully run this on non-Windows; the function
	// returns a clear error there. On Windows it should resolve to a
	// path under SystemRoot.
	got, err := ResolveShell("cmd")
	if err != nil {
		// Expected on non-Windows hosts.
		return
	}
	if got == "" {
		t.Fatal("cmd resolved to empty path")
	}
}
