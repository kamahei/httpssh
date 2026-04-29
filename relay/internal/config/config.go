package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the parsed runtime configuration. The on-disk form is YAML,
// loaded via Load. Defaults are filled in by ApplyDefaults.
type Config struct {
	Listen  string        `yaml:"listen"`
	Shell   string        `yaml:"shell"` // auto | pwsh | powershell | cmd
	Auth    AuthConfig    `yaml:"auth"`
	Session SessionConfig `yaml:"session"`
	Files   FileConfig    `yaml:"files"`
	Log     LogConfig     `yaml:"log"`
}

type AuthConfig struct {
	// LANBearer is the shared bearer required on every request. Empty
	// disables the relay (every request returns 401). Cloudflare Access
	// is treated as an outer edge layer only and does not relax this
	// requirement.
	LANBearer string `yaml:"lan_bearer"`
}

type SessionConfig struct {
	IdleTimeout     time.Duration `yaml:"idle_timeout"`
	ScrollbackBytes int           `yaml:"scrollback_bytes"`
	ReapInterval    time.Duration `yaml:"reap_interval"`
}

type FileConfig struct {
	Roots        []FileRootConfig `yaml:"roots"`
	MaxFileBytes int              `yaml:"max_file_bytes"`
}

type FileRootConfig struct {
	ID   string `yaml:"id"`
	Name string `yaml:"name"`
	Path string `yaml:"path"`
}

type LogConfig struct {
	Level string `yaml:"level"` // debug | info | warn | error
}

// Default returns a Config populated with reasonable defaults.
func Default() *Config {
	return &Config{
		Listen: "127.0.0.1:18822",
		Shell:  "auto",
		Auth:   AuthConfig{},
		Session: SessionConfig{
			IdleTimeout:     24 * time.Hour,
			ScrollbackBytes: 4 << 20, // 4 MiB
			ReapInterval:    60 * time.Second,
		},
		Files: FileConfig{
			MaxFileBytes: 1 << 20, // 1 MiB
		},
		Log: LogConfig{Level: "info"},
	}
}

// Load reads YAML from path, merging on top of the defaults. If path is
// empty, the defaults are returned unchanged.
func Load(path string) (*Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %q: %w", path, err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("config: parse %q: %w", path, err)
	}
	return cfg, nil
}

// Validate enforces invariants. Should be called after merging in flag
// overrides.
func (c *Config) Validate() error {
	if c.Listen == "" {
		return errors.New("config: listen must be set")
	}
	switch c.Shell {
	case "", "auto", "pwsh", "powershell", "cmd":
	default:
		return fmt.Errorf("config: unknown shell %q", c.Shell)
	}
	if c.Auth.LANBearer != "" && len(c.Auth.LANBearer) < 16 {
		return errors.New("config: auth.lan_bearer must be at least 16 chars when set")
	}
	if c.Session.IdleTimeout <= 0 {
		return errors.New("config: session.idle_timeout must be > 0")
	}
	if c.Session.ScrollbackBytes <= 0 {
		return errors.New("config: session.scrollback_bytes must be > 0")
	}
	if c.Session.ReapInterval <= 0 {
		return errors.New("config: session.reap_interval must be > 0")
	}
	if c.Files.MaxFileBytes <= 0 {
		return errors.New("config: files.max_file_bytes must be > 0")
	}
	seenRoots := map[string]struct{}{}
	for _, root := range c.Files.Roots {
		id := strings.TrimSpace(root.ID)
		if id == "" {
			return errors.New("config: files.roots[].id must be set")
		}
		if strings.ContainsAny(id, `/\?#`) {
			return fmt.Errorf("config: files root id %q contains invalid characters", root.ID)
		}
		if _, ok := seenRoots[id]; ok {
			return fmt.Errorf("config: duplicate files root id %q", id)
		}
		seenRoots[id] = struct{}{}
		if strings.TrimSpace(root.Name) == "" {
			return fmt.Errorf("config: files root %q name must be set", id)
		}
		if strings.TrimSpace(root.Path) == "" {
			return fmt.Errorf("config: files root %q path must be set", id)
		}
		if !filepath.IsAbs(root.Path) {
			return fmt.Errorf("config: files root %q path must be absolute", id)
		}
	}
	switch c.Log.Level {
	case "", "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("config: unknown log.level %q", c.Log.Level)
	}
	return nil
}
