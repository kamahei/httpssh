# Integrations

`httpssh` deliberately keeps third-party surface small. The integrations below are the only external systems the project depends on.

## Cloudflare Tunnel + Cloudflare Access

- Role: Public ingress and authentication.
- Why: Avoids opening inbound ports; offloads identity to a managed IdP.
- Touch points:
  - `cloudflared` Windows service runs alongside the relay and originates an outbound TLS connection to Cloudflare.
  - The relay sees decrypted HTTP requests on `127.0.0.1:18822`. Cloudflare-injected headers (`Cf-Access-Jwt-Assertion`, `Cf-Access-Authenticated-User-Email`, etc.) are visible to the relay but **ignored**: the relay's only auth check is the LAN bearer the client must also send.
  - Service Token mode: the relay never sees `CF-Access-Client-Id`/`CF-Access-Client-Secret` directly; Cloudflare consumes them at the edge as the identity factor.
- Failure modes and handling:
  - `cloudflared` connection lost → external clients see a Cloudflare 530/1033 page; LAN access is unaffected.
  - Cloudflare lets a request through but the client forgot the bearer → relay returns `401 unauthorized`.
- Setup is documented in `docs/cloudflare-setup.md`.

## Google Identity Provider

- Role: Browser SSO for the Cloudflare Access application (Policy B).
- Why: User already has a Google account; no separate password needed.
- Touch points: configured once in the Cloudflare Zero Trust dashboard. Neither the relay nor the clients call Google directly.
- Failure modes: Google outage blocks Policy B logins; Policy A (Service Token) is unaffected, so the mobile app continues to work.

## Windows ConPTY API

- Role: Pseudo-console for spawning `pwsh.exe` / `powershell.exe` / `cmd.exe`.
- Why: The only sanctioned way to drive a Windows console process with arbitrary terminal dimensions and ANSI handling.
- Touch points: `relay/internal/conpty` calls `kernel32.dll` via `golang.org/x/sys/windows`. The `github.com/UserExistsError/conpty` package wraps the boilerplate.
- Failure modes:
  - Windows < 10 1809 → `CreatePseudoConsole` not available; relay logs a fatal error on startup.
  - Process spawn fails (executable missing, ACL denial) → returns `503 spawn_failed` to the client and emits a structured log.

## xterm.js (web client) and xterm.dart (Flutter client)

- Role: Terminal emulator widgets in the two GUI clients.
- Why: Mature, ANSI-compliant, well-tested implementations exist for both targets; reimplementing a terminal emulator is out of scope.
- Touch points:
  - Web client: `xterm` + `@xterm/addon-fit` loaded by the embedded browser test client.
  - Flutter app: `xterm` package on pub.dev.
- Failure modes: Widget bugs at very wide widths or unusual locales are isolated by the resize-clamp validation rule (`docs/data-model.md`).

## Flutter Secure Storage

- Role: Storing LAN bearers and Cloudflare Service Token secrets on the mobile device.
- Why: OS-level keystore (Keychain on iOS, Keystore on Android) is the right place for these secrets.
- Failure modes: On Android emulators without a user-set lock screen, secure storage degrades to encrypted shared preferences; acceptable for a developer environment.

## Observability (Built-In, Not Third-Party)

- Logs: `slog` JSON to stdout. The current Windows service wrapper does not configure a dedicated Event Log sink.
- Metrics: not in v1. If needed later, expose `/api/metrics` (Prometheus text format) under the same auth.

## Build And Release Automation

- GitHub Actions runs CI on pull requests and `main` pushes.
- GitHub Releases are created from version tags and publish the Windows relay `.exe`, Android `.apk`, and SHA-256 checksum files.
- Release packaging is documented in `docs/release.md`.

## Excluded On Purpose

- No Sentry / Crashlytics: the user does not want crash reports leaving the device.
- No analytics SDK in the mobile app.
- No external feature-flag service.
- No cloud KV / database service (sessions are in-memory).
