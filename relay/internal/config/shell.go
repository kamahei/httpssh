// Package config provides runtime configuration helpers for the relay.
package config

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf16"
)

// ResolveShell maps a logical shell name ("pwsh", "powershell", "cmd",
// "auto") to an absolute executable path on the host plus the
// shell-specific arguments needed to install the OSC 9;9 prompt wrapper
// the relay uses to track each session's working directory. Unknown
// names are rejected to prevent the relay from being abused as a
// generic process launcher.
func ResolveShell(name string) (string, []string, error) {
	switch name {
	case "", "auto":
		// Prefer pwsh, fall back to powershell.
		if p, err := exec.LookPath("pwsh"); err == nil {
			return p, powerShellArgs(), nil
		}
		if p, err := exec.LookPath("powershell"); err == nil {
			return p, powerShellArgs(), nil
		}
		if runtime.GOOS == "windows" {
			candidate := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
			if _, err := os.Stat(candidate); err == nil {
				return candidate, powerShellArgs(), nil
			}
		}
		return "", nil, errors.New("no PowerShell binary found on PATH")

	case "pwsh":
		if p, err := exec.LookPath("pwsh"); err == nil {
			return p, powerShellArgs(), nil
		}
		return "", nil, errors.New("pwsh not found on PATH")

	case "powershell":
		if p, err := exec.LookPath("powershell"); err == nil {
			return p, powerShellArgs(), nil
		}
		if runtime.GOOS == "windows" {
			candidate := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
			if _, err := os.Stat(candidate); err == nil {
				return candidate, powerShellArgs(), nil
			}
		}
		return "", nil, errors.New("powershell not found")

	case "cmd":
		if runtime.GOOS != "windows" {
			return "", nil, errors.New("cmd requires Windows")
		}
		candidate := filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, cmdArgs(), nil
		}
		return "", nil, errors.New("cmd.exe not found at expected location")

	default:
		return "", nil, fmt.Errorf("unknown shell %q (allowed: pwsh, powershell, cmd, auto)", name)
	}
}

// powerShellArgs builds the pwsh/powershell command line that wraps the
// post-profile prompt with an OSC 9;9 emitter. The script is delivered
// via -EncodedCommand so we never have to fight the host's command-line
// quoting rules.
//
// PowerShell's startup order is: $PROFILE → -Command/-EncodedCommand →
// interactive REPL (because of -NoExit). That means our wrapper sees the
// user's customized prompt (if any) and composes on top of it, instead
// of clobbering it.
func powerShellArgs() []string {
	const script = `$global:_HttpsshPriorPrompt = (Get-Item function:prompt).ScriptBlock
function global:prompt {
  $base = & $global:_HttpsshPriorPrompt
  if ($PWD.Provider.Name -eq 'FileSystem') {
    "$([char]27)]9;9;$($PWD.ProviderPath)$([char]7)$base"
  } else {
    $base
  }
}
`
	return []string{"-NoLogo", "-NoExit", "-EncodedCommand", encodePwshScript(script)}
}

// cmdArgs builds a cmd.exe invocation that overrides PROMPT with an
// OSC 9;9 prefix. cmd's PROMPT macros: $E = ESC, $P = current path,
// $G = '>'. The sequence "$E\\" produces ESC + '\' which is the ST
// (string terminator) for OSC.
//
// /D suppresses the AutoRun registry entries so a hostile or unexpected
// HKCU\Software\Microsoft\Command Processor\AutoRun cannot replace our
// prompt before we install it.
func cmdArgs() []string {
	return []string{"/D", "/K", `prompt $E]9;9;$P$E\$P$G `}
}

func encodePwshScript(script string) string {
	// PowerShell's -EncodedCommand expects base64 of UTF-16-LE.
	script = strings.ReplaceAll(script, "\r\n", "\n")
	units := utf16.Encode([]rune(script))
	buf := make([]byte, len(units)*2)
	for i, u := range units {
		binary.LittleEndian.PutUint16(buf[i*2:], u)
	}
	return base64.StdEncoding.EncodeToString(buf)
}
