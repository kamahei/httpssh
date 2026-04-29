# Implementation Plan

Status: this is now a historical implementation roadmap. The relay, embedded web client, and Flutter mobile app have working implementations, and GitHub Release automation has been added for the Windows `.exe` and Android `.apk`. For current operator and maintainer workflows, start with [docs/README.md](README.md), [docs/user-manual.md](user-manual.md), and [docs/release.md](release.md).

A phased plan that builds the relay first (so testing is possible end-to-end at each step), then the web client (because it lets us validate the OAuth and protocol paths in a browser), then the Flutter app (which has the longest iteration loop).

## Phase 0 — Repository Scaffold

Goal: An empty but well-structured repo with linting and a passing build for an empty server.

- Initialize `relay/go.mod` (`module github.com/<owner>/httpssh/relay`) targeting Go 1.22+.
- Create the package directory layout from `docs/architecture.md`.
- Add `relay/cmd/httpssh-relay/main.go` that starts an HTTP server on `:18822` and answers `/api/health`.
- Add a Makefile or `Taskfile.yml` with `build`, `test`, `lint` targets.
- Wire `golangci-lint` with sane defaults (errcheck, govet, staticcheck, ineffassign, unused).
- Verify `GOOS=windows GOARCH=amd64 go build ./...` produces a binary on the dev machine.
- Initial commit.

Validation: `go build ./...` succeeds; `curl http://localhost:18822/api/health` returns 200 with stub body.

## Phase 1 — ConPTY Wrapper + Single Session

Goal: Spawn a real PowerShell process through ConPTY and pipe its I/O to a single WebSocket client.

- Add `internal/conpty` wrapping `github.com/UserExistsError/conpty`. Tests on Windows verify echo and resize.
- Add `internal/session` with `Session` (no scrollback yet), `Manager.Create`, `Manager.Get`, `Manager.Kill`. No idle GC yet.
- Add `internal/server` with `POST /api/sessions`, `DELETE /api/sessions/{id}`, `GET /api/sessions/{id}/io` (WebSocket).
- Implement the wire protocol (`replay` is empty for now; `out`, `in`, `resize`, `ping`/`pong`).
- Add `nhooyr.io/websocket`.

Validation: `websocat ws://localhost:18822/api/sessions/<id>/io` after a `POST` produces a working PowerShell session.

## Phase 2 — Multi-Session, Scrollback, Replay, Multi-Subscriber

Goal: Server-side persistence across disconnects and concurrent subscribers.

- Add `RingBuffer` in `internal/session`. Default 4 MiB, configurable.
- Update the PTY pump to fan out to every current subscriber AND append to the ring.
- Send a `replay` frame on attach with the latest ring contents.
- Add `GET /api/sessions` and `PATCH /api/sessions/{id}`.
- Add `Manager.Reap` goroutine for idle GC (interval 60 s, default timeout 24 h).

Validation: open two `websocat` clients to the same session; both see live output. Disconnect one, reconnect, verify replay shows recent buffer.

## Phase 3 — Auth and Service Wrapping

Goal: Production auth and Windows service deployment.

- Add `internal/config` with YAML loader + validation.
- Add `internal/auth` middleware: LAN bearer required on every request (REST `Authorization: Bearer ...` or WebSocket `?token=...`). Cloudflare Access is treated as an outer edge layer only; the relay does not inspect any `Cf-*` header.
- Add `internal/svc` for Windows service registration (`golang.org/x/sys/windows/svc`).
- Add `scripts/install-service.ps1` and `uninstall-service.ps1`.
- Logging via `slog` with JSON handler; capture `auth_outcome` field on every request.

Validation: Start as a Windows service; verify the bearer-only auth decision table from `docs/api-contracts.md`.

## Phase 3.5 — Web Test Client

Goal: A browser client that exercises the full protocol and the OAuth flow.

- Scaffold `relay/web/` with TypeScript + esbuild + xterm.js.
- Implement: profiles in localStorage, sessions panel, multi-tab terminal, fit addon, reconnect with backoff.
- Add `build.mjs` that emits `relay/internal/server/webfs/{index.html,bundle.js,style.css}`.
- Embed via `go:embed all:webfs` and serve at `/web/` WITHOUT the auth gate so the SPA shell can load before the user pastes a bearer.
- Confirm `/api/*` is still bearer-gated and that the public Cloudflare path is still protected by Cloudflare Access at the edge.

Validation: `https://pwsh.<domain>/web/` opens, completes Google login, lists sessions, attaches a terminal.

## Phase 4 — Flutter App MVP (single tab)

Goal: An installable Flutter app with one profile and one session at a time.

- `flutter create mobile`, configure `pubspec.yaml` with `xterm`, `web_socket_channel`, `dio`, `flutter_secure_storage`, `flutter_localizations`, `intl`.
- Set up `mobile/lib/l10n/` with `app_en.arb`, `app_ja.arb`, and `l10n.yaml`.
- Implement Profiles screen and editor.
- Implement Sessions screen.
- Implement Terminal screen (single tab) with reconnect handling.

Validation: `flutter run` on Android emulator and iOS simulator; create profile, create session, type commands, kill session.

## Phase 5 — Multi-Tab Flutter

Goal: Multiple terminals per profile with independent state.

- Convert Terminal screen to a `TabController`-based view; each tab owns a `_SessionTabState` with its own `WebSocketChannel` and `Terminal`.
- Add tab strip, `+` button, soft-keyboard helper bar, fullscreen toggle.
- Add per-tab status dot.

Validation: Two tabs running `Get-ChildItem -Recurse C:\` simultaneously without interference; switching tabs shows correct backlog.

## Phase 6 — Cloudflare End-to-End

Goal: Verified production deployment with Service Token (mobile) and Google SSO (web/browser).

- Walk through `docs/cloudflare-setup.md` on the actual user account.
- Run smoke tests in the doc.
- Confirm the mobile app works through Cloudflare Tunnel with a Service Token profile.
- Confirm the web client works after Google login.
- Capture screenshots for the README.

Validation: Mobile data connection (Wi-Fi off) → app connects to `pwsh.<domain>` → terminal works.

## Phase 7 — Polish

Goal: Things that aren't critical but make the product feel finished.

- App icon and splash screen for mobile.
- Theme picker, terminal palette picker, font size in mobile settings.
- Per-profile last-used session restore on the Sessions screen.
- README screenshots and setup video link (optional).
- Crash safety: panic recovery in HTTP handlers; structured error frames on the WS.
- Unit-test coverage push: aim for ≥ 80 % on `internal/session` and `internal/auth`.

Validation: Run through the acceptance criteria in `docs/acceptance-criteria.md`.

## Delivery Strategy

- Each phase ends with a tagged commit (`phase-0`, `phase-1`, …) on `main`.
- Phase boundaries are also natural review checkpoints. The user can pause at any phase boundary without leaving the codebase in a half-done state.
- Skipping a phase is allowed only if its validation step would still pass (e.g., Phase 7 polish can be deferred indefinitely).
