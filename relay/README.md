# httpssh-relay

The Windows x64 HTTP/WebSocket relay that bridges browser/mobile terminal
clients to a ConPTY-backed PowerShell session.

## Prerequisites

- Windows 10 1809 or later (ConPTY requires the pseudo-console API).
- Go 1.22 or later. Install with `winget install GoLang.Go`.
- PowerShell 7 (`pwsh`) on PATH is recommended; `powershell.exe` works as a
  fallback. `cmd.exe` is also accepted as a `--shell cmd` choice.
- (Optional) [Task](https://taskfile.dev): `winget install Task.Task`.

## First-time setup

From the `relay/` directory:

```powershell
go mod tidy
go build -o httpssh-relay.exe ./cmd/httpssh-relay
```

If you prefer Task:

```powershell
task build
```

`task build` writes `httpssh-relay.exe` in this directory. `task build-windows`
cross-builds Windows x64 to `dist/httpssh-relay.exe`.

## Running

Foreground, with a stable bearer token:

```powershell
./httpssh-relay.exe --listen 127.0.0.1:18822 --bearer "your-32-char-or-longer-token-here" --log-level debug
```

Foreground without `--bearer`: a random token is generated and printed to
stderr. Useful for a quick smoke test, but copy it into the web client
settings before the relay restarts.

```powershell
./httpssh-relay.exe --listen 127.0.0.1:18822 --log-level debug
```

Run-from-source equivalent:

```powershell
go run ./cmd/httpssh-relay --log-level debug
```

## Connecting

1. Open `http://127.0.0.1:18822/web/` in a browser. The page redirects from
   `/` to `/web/`.
2. Click **Settings**, paste the LAN bearer that the relay logged at
   startup (or the one you passed via `--bearer`), and **Save**.
3. Click **+ New** to spawn a `pwsh` session. The terminal panel attaches
   automatically.

For an HTTP-level smoke test:

```powershell
curl http://127.0.0.1:18822/api/health -H "Authorization: Bearer <your-bearer>"
```

## Flags

| Flag | Default | Purpose |
|---|---|---|
| `--config` | _(none)_ | path to `config.yaml`; flags override config values |
| `--listen` | `127.0.0.1:18822` | host:port to listen on |
| `--bearer` | _(random)_ | LAN bearer token |
| `--idle-timeout` | `24h` | kill an idle session after this long |
| `--scrollback-bytes` | `4194304` | per-session scrollback ring size |
| `--log-level` | `info` | `debug`, `info`, `warn`, `error` |
| `--shell` | `auto` | shell resolver default for lower-level callers; HTTP session-create currently defaults an omitted `shell` field to `pwsh` |

## Tests

```powershell
go test -race ./...
```

ConPTY tests exist on Windows; the package compiles to a stub that returns
`ErrUnsupported` on other platforms so most of the relay can still be
unit-tested cross-platform.

## Production: behind Cloudflare Tunnel

The relay always requires the LAN bearer. Cloudflare Access at the
edge handles identity (Service Token for the mobile app, Google SSO
for the browser); the relay does not look at any Cloudflare header.

Recommended `config.yaml`:

```yaml
listen: "127.0.0.1:18822"
shell: "auto"
auth:
  lan_bearer: "<32+ random hex chars; required>"
session:
  idle_timeout: "24h"
  scrollback_bytes: 4194304
  reap_interval: "60s"
log:
  level: "info"
```

Then install the service:

```powershell
pwsh -File scripts/install-service.ps1   # elevated
Start-Service httpssh-relay
```

For a local build that already has `dist/httpssh-relay.exe` and `config.yaml`,
an elevated Command Prompt or PowerShell can run:

```bat
scripts\install-service.bat
```

To install from a downloaded release executable instead, pass the executable
path as the first argument. The script still copies `config.yaml` from the
relay directory:

```bat
scripts\install-service.bat "%USERPROFILE%\Downloads\httpssh-relay-windows-amd64-v0.2.0.exe"
```

To uninstall the service and remove the copied files from
`C:\Program Files\httpssh`, run:

```bat
scripts\uninstall-service.bat
```

If the relay should inherit the signed-in user's environment instead of running
as `LocalSystem`, install it as a current-user logon task:

```bat
scripts\install-logon-task.bat
```

This copies files to `%LOCALAPPDATA%\httpssh`, registers a hidden scheduled task
named `httpssh-relay-logon`, and starts it immediately. Do not run this task and
the `httpssh-relay` Windows service on the same `listen` address at the same
time. Remove the task with:

```bat
scripts\uninstall-logon-task.bat
```

Pass `/remove-files` to the uninstall script if you also want to remove the
copied files and logs from `%LOCALAPPDATA%\httpssh`.

For the operational runbook (rotating tokens, revoking users, debugging
401s), see [`docs/cloudflare-operations.md`](../docs/cloudflare-operations.md).

## GitHub Release Build

Tagged GitHub Releases publish the relay as:

```text
httpssh-relay-windows-amd64-<tag>.exe
```

The release workflow embeds the tag version into `/api/health` with Go linker flags. Maintainer steps are documented in [`docs/release.md`](../docs/release.md).

## Layout

See [`docs/architecture.md`](../docs/architecture.md) for the canonical
description of package boundaries.
