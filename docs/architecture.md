# Architecture

## Summary

A single Go relay process on Windows owns all PowerShell ConPTY sessions and brokers them to clients over HTTP+WebSocket. Clients are either the Flutter mobile app (over LAN HTTP or Cloudflare HTTPS) or the embedded xterm.js web client served from the same relay. Cloudflare Tunnel handles edge termination and `cloudflared` runs alongside the relay on the Windows host. There is no database; session state is in-memory only.

## Current Repository Shape

```text
httpssh/
├── README.md
├── AGENTS.md
├── docs/                                # Documentation pack (English)
├── relay/                               # Go relay (Windows x64)
│   ├── go.mod
│   ├── go.sum
│   ├── cmd/
│   │   └── httpssh-relay/
│   │       └── main.go                  # Service entrypoint
│   ├── internal/
│   │   ├── conpty/                      # Thin ConPTY wrapper
│   │   ├── session/                     # Session, Manager, RingBuffer
│   │   ├── server/                      # HTTP routes, WebSocket handler, embedded web
│   │   │   └── webfs/                   # go:embed FS for the web client bundle
│   │   ├── auth/                        # LAN bearer auth middleware
│   │   ├── config/                      # YAML config loader
│   │   └── svc/                         # Windows service plumbing
│   ├── config.example.yaml
│   └── scripts/
│       ├── install-service.ps1
│       └── uninstall-service.ps1
└── mobile/                              # Flutter app
    ├── pubspec.yaml
    ├── l10n.yaml
    ├── android/
    └── lib/
        ├── main.dart
        ├── app.dart
        ├── api/                         # REST + WS client
        ├── auth/                        # Secure storage of credentials
        ├── models/                      # Profile, Session, Frame
        ├── state/                       # Riverpod providers
        ├── terminal/                    # xterm bridge + tab state
        ├── screens/
        │   ├── profiles_screen.dart
        │   ├── sessions_screen.dart
        │   └── terminal_workspace.dart
        └── l10n/
            ├── app_en.arb
            └── app_ja.arb
```

## Runtime Components

- **`httpssh-relay` (Windows service)**: HTTP server, WebSocket upgrader, session manager, ConPTY supervisor, read-only file browser API, embedded web file server, auth middleware. Single process; single binary.
- **`cloudflared` (Windows service)**: Cloudflare Tunnel daemon. Mapped: `pwsh.<domain>` → `http://127.0.0.1:18822`. Independent process.
- **Mobile app**: Flutter app on Android and iOS. Holds connection profiles in secure storage; opens one WebSocket per active session tab.
- **Web client**: Embedded browser SPA served at `/web/`. Used for testing, browser/Google OAuth login flow, and admin operations that the current UI exposes (list, create, attach, kill).

## Responsibilities And Boundaries

- ConPTY layer (`internal/conpty`): only knows about Win32 pseudo-console objects. Returns an `io.ReadWriteCloser` plus a `Resize(cols, rows)` method. Knows nothing about HTTP.
- Session layer (`internal/session`): owns the lifecycle of a `Session` (id, ConPTY, command, scrollback, subscribers, idle timer). Knows nothing about HTTP.
- Server layer (`internal/server`): owns HTTP routes, WebSocket framing, and the embedded web FS. Calls into `session.Manager`. Knows nothing about Win32.
- Auth layer (`internal/auth`): pure HTTP middleware. Decides allow/deny based on headers. Knows nothing about sessions.
- Config (`internal/config`): YAML loader and validator. No globals.
- File API (`internal/fileapi`): read-only filesystem listing and text decoding for configured roots. Knows nothing about HTTP auth or sessions.
- Service (`internal/svc`): Windows service entrypoint glue. Calls into the rest of the relay's plain `main`.

Cross-layer rule: lower layers must not import higher layers. `conpty` must not import `session`; `session` must not import `server`.

## Data And Control Flow

### Mobile session create

1. Mobile app issues `POST /api/sessions` with body `{shell, cols, rows}` and the chosen auth headers.
2. `auth` middleware verifies `Authorization: Bearer <lan_bearer>`. Cloudflare-side credentials (Service Token or SSO cookie) are consumed by Cloudflare at the edge and not seen by the relay.
3. `server` calls `session.Manager.Create(shell, cols, rows)`.
4. `session.Manager` calls `conpty.New(cols, rows)`, then `cmd.Start()` for `pwsh.exe`. A `goroutine` reads from the PTY and fans out into the session's ring buffer and any current subscribers.
5. Server returns `201 Created` with the session ID.

### Mobile attach + replay + live

1. Mobile app opens `WebSocket /api/sessions/{id}/io`.
2. `auth` middleware enforces the same rules on the WS upgrade.
3. `server` registers the WebSocket as a subscriber of the session.
4. Server immediately sends one `{"t":"replay","d":"<scrollback>"}` frame.
5. Server pumps subsequent PTY output as `{"t":"out","d":"..."}` frames.
6. Server reads incoming frames: `{"t":"in",...}` writes to the PTY; `{"t":"resize",...}` calls `session.Resize`.

### Disconnect

1. Client TCP closes. WebSocket handler removes the subscriber; subscriber count goes to 0.
2. The session's idle timer starts. ConPTY and `pwsh` continue to run; output continues into the ring buffer.
3. On reconnect, the new WebSocket gets the latest ring buffer in a `replay` frame, then live frames.

### Web client OAuth

1. Browser hits `/web/index.html`.
2. Cloudflare Access intercepts, redirects to Google IdP, returns to the same path with a `CF_Authorization` cookie.
3. Cloudflare validates the cookie at the edge and forwards the request to the relay (the relay does not inspect any Cf-* header).
4. The static `/web/*` files are served unauthenticated; the SPA loads.
5. The user pastes the LAN bearer into the SPA's Settings dialog (stored in localStorage). Subsequent REST and WebSocket calls carry the bearer; Cloudflare's cookie continues to satisfy the edge automatically.

### Mobile file browser

1. Operator configures `files.roots` in `config.yaml` and restarts the relay.
2. Mobile app calls `GET /api/files/roots` with the same bearer and optional Cloudflare edge credentials as other REST calls.
3. App lists a configured root with `GET /api/files/list?root=<id>&path=<relative>`.
4. App opens text files with `GET /api/files/read?root=<id>&path=<relative>`. The relay decodes UTF-8, UTF-16 BOM, or Shift_JIS text up to `files.max_file_bytes`.
5. The relay rejects root escape attempts after path cleaning and symlink resolution.

## External Interfaces

- HTTP REST: `/api/health`, `/api/sessions`, `/api/sessions/{id}` (PATCH/DELETE).
- File REST: `/api/files/roots`, `/api/files/list`, `/api/files/read` (read-only, bearer-gated).
- WebSocket: `/api/sessions/{id}/io`.
- Static: `/web/*`.
- Cloudflare Tunnel: `pwsh.<domain>` → `http://127.0.0.1:18822`.
- Filesystem: `config.yaml` (read) plus read-only access to configured `files.roots`. Logs are written as JSON to stdout; the current service wrapper does not install a dedicated Windows Event Log sink.

## Key Tradeoffs

- **In-memory sessions vs persistence**: Persistence would survive restarts but adds a database, file rotation, and recovery semantics. v1 chooses ephemeral; the user explicitly accepted this.
- **One bearer for LAN vs per-device tokens**: Per-device LAN tokens would mirror Cloudflare Service Tokens, but for a single-operator deployment one shared bearer is simpler. Documented; revisit if multi-user becomes a goal.
- **Embedded web client vs separate static host**: Embedding via `go:embed` simplifies distribution and avoids a second hostname; Cloudflare Access policies are easier on a single origin. Cost: relay binary grows by ~300 KiB.
- **Flutter vs native**: Flutter halves the implementation cost at the price of slightly less native feel. The user picked Flutter explicitly.
- **JSON text frames vs binary**: JSON costs ~30% bandwidth overhead vs raw binary frames, but is trivial to debug with `websocat` and inspect in browser DevTools. Acceptable for single-user shell volumes.
- **Single binary on Windows vs Docker container**: Docker on Windows would complicate ConPTY access. A single signed `.exe` is simpler.
- **Read-only file API vs SFTP**: A narrow HTTP file API matches the existing bearer/Cloudflare model and avoids adding SSH/SFTP credentials or a second protocol stack. It intentionally does not support writes.
