// Package config provides runtime configuration helpers for the relay.
package config

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// ResolveShell maps a logical shell name ("pwsh", "powershell", "cmd",
// "auto") to an absolute executable path on the host. Unknown names are
// rejected to prevent the relay from being abused as a generic process
// launcher.
func ResolveShell(name string) (string, error) {
	switch name {
	case "", "auto":
		// Prefer pwsh, fall back to powershell.
		if p, err := exec.LookPath("pwsh"); err == nil {
			return p, nil
		}
		if p, err := exec.LookPath("powershell"); err == nil {
			return p, nil
		}
		if runtime.GOOS == "windows" {
			candidate := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
			if _, err := os.Stat(candidate); err == nil {
				return candidate, nil
			}
		}
		return "", errors.New("no PowerShell binary found on PATH")

	case "pwsh":
		if p, err := exec.LookPath("pwsh"); err == nil {
			return p, nil
		}
		return "", errors.New("pwsh not found on PATH")

	case "powershell":
		if p, err := exec.LookPath("powershell"); err == nil {
			return p, nil
		}
		if runtime.GOOS == "windows" {
			candidate := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
			if _, err := os.Stat(candidate); err == nil {
				return candidate, nil
			}
		}
		return "", errors.New("powershell not found")

	case "cmd":
		if runtime.GOOS != "windows" {
			return "", errors.New("cmd requires Windows")
		}
		candidate := filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
		return "", errors.New("cmd.exe not found at expected location")

	default:
		return "", fmt.Errorf("unknown shell %q (allowed: pwsh, powershell, cmd, auto)", name)
	}
}
