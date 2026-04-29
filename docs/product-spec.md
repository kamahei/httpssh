# Product Spec

## Scope Summary

v1 delivers three deliverables:

1. `httpssh-relay` — a Windows x64 Go binary that runs as a Windows service, spawns PowerShell sessions through ConPTY, and exposes them over HTTP+WebSocket. It also serves the bundled web test client.
2. `httpssh-web` — a browser test client embedded into the relay binary via `go:embed` and served at `/web/`.
3. `httpssh-mobile` — a Flutter app for Android and iOS with multi-tab terminal support and English/Japanese UI.

Deployment topology: the relay listens on `127.0.0.1:18822` and is fronted locally by `cloudflared` (Cloudflare Tunnel). Mobile clients reach the relay either directly over LAN (`http://<lan-ip>:18822`) using a shared bearer token, or via `https://pwsh.<domain>/` gated by Cloudflare Access.

## Primary User Journeys

### Journey 1: First-time external connection from the mobile app

- Trigger: Owner installs the app, opens it, taps "Add profile."
- Steps:
  1. Owner enters profile name "Home (Cloudflare)", base URL `https://pwsh.example.com`, picks "Cloudflare Service Token" as auth, and pastes the Client ID + Client Secret issued from Cloudflare Access.
  2. App calls `GET /api/health`. Cloudflare Access validates the headers, the relay returns 200.
  3. App lists sessions (none). Owner taps "New session", picks `pwsh.exe`, default 80x24.
  4. Relay spawns ConPTY, returns the session ID, app opens a WebSocket and shows the prompt.
- Outcome: Owner sees `PS C:\Users\Owner>` within seconds and can type commands.

### Journey 2: LAN connection with bearer

- Trigger: Owner is at home on Wi-Fi and wants the lower-latency LAN path.
- Steps:
  1. Owner adds a second profile "Home (LAN)" with base URL `http://192.168.1.20:18822` and pastes the LAN bearer from `config.yaml`.
  2. App attaches `Authorization: Bearer <token>` to every REST and WebSocket request.
  3. Relay's auth middleware verifies the bearer (constant-time compare) and lets the request through.
- Outcome: Owner gets the same multi-session UX directly over LAN with no Cloudflare round-trip.

### Journey 3: Reconnect after disconnect

- Trigger: Mobile network handoff drops the WebSocket while a long-running command is producing output.
- Steps:
  1. App detects WebSocket close, marks the tab "Reconnecting".
  2. Relay keeps the ConPTY process alive and continues writing output into the session's ring buffer.
  3. App reconnects within 30 seconds, opens a fresh WebSocket to `/api/sessions/{id}/io`.
  4. Relay sends a `replay` frame containing the buffered output since the last point the client acknowledged, then resumes live streaming.
- Outcome: Owner sees missed output without losing the running command.

### Journey 4: Browser/Google OAuth (web client)

- Trigger: Owner is on a borrowed laptop and needs PowerShell access without installing anything.
- Steps:
  1. Owner opens `https://pwsh.example.com/web/` in a browser.
  2. Cloudflare Access redirects to Google SSO, Owner approves with an email address allow-listed in their own Cloudflare Access policy.
  3. Cloudflare redirects back with a session cookie; the relay serves the embedded SPA.
  4. SPA calls REST/WS with the cookie attached automatically.
- Outcome: Owner gets the same session list and multi-tab terminal in the browser, no Service Token needed.

### Journey 5: Multiple tabs

- Trigger: Owner already has a long `Get-ChildItem -Recurse` running in tab 1 and wants a second prompt.
- Steps:
  1. Owner taps "New tab" in the app. App calls `POST /api/sessions`.
  2. Relay spawns a second ConPTY+pwsh, returns a new session ID.
  3. App opens a second WebSocket. Both tabs run independently.
  4. Owner switches to tab 1, sees output that arrived while tab 2 was active (rendered from the ring buffer on activation).
- Outcome: Two concurrent sessions, no I/O loss when switching.

## Functional Requirements

- FR-1: Relay spawns `pwsh.exe` if available, else `powershell.exe`. Shell selection is per session via the `shell` parameter.
- FR-2: Relay implements `GET /api/sessions`, `POST /api/sessions`, `DELETE /api/sessions/{id}`, `PATCH /api/sessions/{id}` (rename), `GET /api/health`.
- FR-3: Relay implements `GET /api/sessions/{id}/io` as a WebSocket endpoint speaking the protocol defined in `docs/protocol.md`.
- FR-4: On WebSocket attach, relay sends a single `replay` frame containing the most recent N bytes of scrollback (default 4 MiB), then transitions to live streaming.
- FR-5: Relay supports a `resize` frame from the client and reflects new dimensions to the ConPTY.
- FR-6: Relay supports concurrent multi-subscriber on a single session (multi-device or multi-tab attach to the same shell allowed).
- FR-7: Auth middleware: every `/api/*` request and every WebSocket handshake requires `Authorization: Bearer <lan_bearer>` or `?token=<lan_bearer>` for browser WebSocket handshakes. Cloudflare Access is treated as an outer edge layer only and does not relax this requirement; the relay does not inspect any `Cf-*` header.
- FR-8: Relay serves embedded web client static assets at `/web/*` (HTML, JS bundle, CSS, favicon) via `go:embed`. Static assets are loadable before the bearer is entered; all meaningful operations from the web client still call bearer-gated `/api/*` endpoints.
- FR-9: Idle session timeout: sessions with zero subscribers and no I/O for `idle_timeout` (default 24h) are killed gracefully.
- FR-10: Mobile app stores profiles and credentials in `flutter_secure_storage`. App supports adding, editing, deleting, duplicating profiles.
- FR-11: Mobile app maintains independent tab state per session, including local rendering buffer that is flushed to the visible xterm when the tab becomes active.
- FR-12: Mobile app supports automatic reconnect with exponential backoff (1s, 2s, 5s, 10s, 30s cap) on WebSocket close.
- FR-13: Mobile app UI is localized to English and Japanese using ARB files.
- FR-14: Web client supports the same session list, create, attach, resize, multi-tab UX as the mobile app for testing.
- FR-15: Relay logs every authentication outcome (allowed/denied) at INFO level with the source path (LAN bearer vs Cloudflare).

## Non-Functional Requirements

- Performance: First byte from `GET /api/health` over LAN under 50 ms. Round-trip keystroke→echo over Cloudflare Tunnel under 250 ms over typical home internet.
- Reliability: Session must survive a client TCP RST and a 60-second client outage without dropping the shell.
- Scale: Up to 10 concurrent sessions on a single relay; up to 4 simultaneous WebSocket subscribers per session. No clustering.
- Security: External access requires a Cloudflare Access policy match (Service Token or email/Google). LAN access requires the bearer token. Bearer token is at least 32 bytes of crypto-random data, hex-encoded.
- Operability: Relay logs to stdout (captured by the Windows service wrapper) at INFO by default; supports `--log-level=debug` and a config option.
- Portability: Relay builds with `GOOS=windows GOARCH=amd64`. Flutter app builds for `android` and `ios` from the same code base.
- Distribution: GitHub Releases publish the Windows x64 relay `.exe` and a signed Android `.apk` with SHA-256 checksum files.
- Privacy: No telemetry; no third-party analytics.

## Out Of Scope

- Hosting the relay on Linux or macOS.
- File transfer (`scp`, `sftp`).
- Built-in code editor in the mobile app.
- Multi-user account system within the relay.
- Cluster mode / horizontal scaling.
- E2E session encryption at the application layer (TLS via Cloudflare and LAN trust assumed).
- Session recording / audit logs beyond standard logs.
- Plugins, scripting hooks, or extensibility mechanisms.

## Risks And Unknowns

- ConPTY behavior with very wide terminals (over 500 columns) is occasionally buggy on older Windows builds; we restrict client-requested resizes to 1..500 cols and 1..200 rows.
- Cloudflare Access for WebSocket: Service Token validation occurs on the WS upgrade request; if the token is rotated mid-session the existing WS stays open until disconnect. Documented as expected behavior, not a defect.
- iOS background WebSocket: iOS suspends sockets aggressively. The reconnect+replay mechanism handles this, but the user may see "Reconnecting" after returning to the foreground.
- Bearer leak on LAN: a leaked LAN bearer compromises the host. We mitigate by recommending firewall rules that bind the relay to the LAN interface only, and by allowing the bearer to be rotated via config edit + service restart.
