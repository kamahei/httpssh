# Task Breakdown

Status: this file is retained as a historical task map for future maintenance context. It is no longer the public documentation entry point. Use [docs/README.md](README.md) for the current documentation map and [docs/development.md](development.md) for validation commands.

Each task is sized for an AI agent or a single working session. Tasks in the same phase can usually be done in order, but parallelism is called out where safe.

Conventions:
- `Scope` lists the files/packages the task may touch.
- `DoD` (Definition of Done) is the validation that proves it works.
- `Deps` is the prerequisite tasks; tasks with the same `Deps` can run in parallel.

## Phase 0 — Repository Scaffold

### T0.1 Initialize Go module and skeleton
- Scope: `relay/go.mod`, `relay/cmd/httpssh-relay/main.go`, `relay/Taskfile.yml` (or `Makefile`).
- Action: `go mod init`, add minimal `main()` that starts `net/http` on `:18822` with `/api/health` returning `{"status":"ok"}`.
- DoD: `go build ./...` from `relay/` succeeds; `curl localhost:18822/api/health` returns 200.
- Deps: none.

### T0.2 Add linter and pre-commit
- Scope: `.golangci.yml`, `relay/Taskfile.yml`.
- Action: enable errcheck, govet, staticcheck, ineffassign, unused; ensure `go vet ./...` and `golangci-lint run` are clean on T0.1's code.
- DoD: `task lint` (or `make lint`) returns exit 0.
- Deps: T0.1.

## Phase 1 — ConPTY + Single Session

### T1.1 ConPTY wrapper
- Scope: `relay/internal/conpty/`.
- Action: Wrap `github.com/UserExistsError/conpty`. Expose `New(cols, rows uint16) (*ConPty, error)`, `Read`, `Write`, `Resize`, `Close`, plus `Spawn(path string, args []string) (*exec.Cmd, error)` that attaches the spawned process to the ConPTY.
- DoD: `go test ./internal/conpty/...` on Windows passes a test that spawns `pwsh.exe -NoProfile -Command "Write-Output hello"` and asserts `hello` appears in `Read` output.
- Deps: T0.2.

### T1.2 Session and Manager (no scrollback yet)
- Scope: `relay/internal/session/`.
- Action: Define `Session` struct from `docs/data-model.md` (without `Scrollback`). Implement `Manager` with `Create`, `Get`, `Kill`. Use a goroutine to read from PTY and broadcast to subscribers (subscriber registration API).
- DoD: Unit test creates a session, registers a fake subscriber, writes `Get-Date\r`, asserts a non-empty output frame is delivered.
- Deps: T1.1.

### T1.3 HTTP server and WebSocket handler
- Scope: `relay/internal/server/`, `relay/cmd/httpssh-relay/main.go`.
- Action: Wire `chi` (or `net/http` + `gorilla/mux` if preferred) router. Implement `POST /api/sessions`, `DELETE /api/sessions/{id}`, `GET /api/sessions/{id}/io` (WebSocket via `nhooyr.io/websocket`).
- DoD: With `websocat`, POST to create a session, then attach to the WS, send `{"t":"in","d":"echo hi\r"}`, see `{"t":"out","d":"...hi..."}`.
- Deps: T1.2.

### T1.4 Resize and ping
- Scope: `relay/internal/server/`, `relay/internal/session/`.
- Action: Handle `{"t":"resize",...}` (validate clamp) and `{"t":"ping"}` → `{"t":"pong"}`.
- DoD: `websocat` sends resize, server logs new dimensions; `tput cols` inside pwsh reflects the change.
- Deps: T1.3.

## Phase 2 — Multi-Session, Scrollback, Replay

### T2.1 RingBuffer
- Scope: `relay/internal/session/ringbuffer.go`.
- Action: Implement bounded ring buffer with `Write([]byte)`, `Snapshot() []byte`, configurable capacity.
- DoD: Unit tests cover wraparound, exact-capacity, over-capacity. Race-detector clean.
- Deps: T1.2.

### T2.2 Wire scrollback into Session
- Scope: `relay/internal/session/`.
- Action: PTY pump writes to ring buffer AND fans out to subscribers. On new subscriber, send a `replay` frame containing the snapshot before transitioning to live frames.
- DoD: Two `websocat` clients, second connects later and sees the recent buffer in a `replay` frame.
- Deps: T2.1, T1.3.

### T2.3 List + rename
- Scope: `relay/internal/server/`.
- Action: `GET /api/sessions`, `PATCH /api/sessions/{id}`.
- DoD: Curl tests in CI assert the list shape and rename behavior.
- Deps: T1.3.

### T2.4 Idle session GC
- Scope: `relay/internal/session/`.
- Action: Background goroutine ticks every 60 s; kills sessions with 0 subscribers and `LastIO` older than `idleTimeout`.
- DoD: Test with a 5-second timeout: create session, no subs, wait, assert kill.
- Deps: T2.2.

## Phase 3 — Auth + Service

### T3.1 Config loader
- Scope: `relay/internal/config/`, `relay/config.example.yaml`.
- Action: YAML loader; validate `listen`, `auth.lan_bearer` (length ≥ 16 when set), `session.idle_timeout`, `session.scrollback_bytes`, `session.reap_interval`, `log.level`.
- DoD: Unit tests cover good/bad configs.
- Deps: T0.2.

### T3.2 Auth middleware (LAN bearer always required)
- Scope: `relay/internal/auth/`.
- Action: Middleware that requires `Authorization: Bearer <lan_bearer>` (REST) or `?token=<lan_bearer>` (WebSocket) on every request. Constant-time compare. Cloudflare-side headers are not inspected; Cloudflare Access is treated as an outer edge layer that handles identity at the edge before the request reaches the relay.
- DoD: Tests for present/absent/wrong bearer; query-token fallback; Cf-* headers alone are insufficient.
- Deps: T3.1, T1.3.

### T3.3 (removed) Cloudflare JWT verification
- Status: dropped. Earlier drafts had the relay re-validate the
  Cloudflare Access JWT against JWKS. The current model treats
  Cloudflare Access as edge-only; the relay does not inspect any Cf-*
  header and relies on the LAN bearer alone for relay-level auth. The
  `internal/auth/cfaccess.go` file and its test were removed in commit
  3841f4f.

### T3.4 Windows service wrapper
- Scope: `relay/internal/svc/`, `relay/scripts/install-service.ps1`, `relay/scripts/uninstall-service.ps1`.
- Action: `golang.org/x/sys/windows/svc` boilerplate. Service name `httpssh-relay`. Install script uses `sc.exe`.
- DoD: `install-service.ps1` registers the service; `Start-Service httpssh-relay` runs the relay; `Stop-Service` stops cleanly.
- Deps: T3.1.

## Phase 3.5 — Web Test Client

### T3.5.1 Scaffold web client
- Scope: `relay/web/`.
- Action: `package.json`, `tsconfig.json`, `index.html`, `style.css`, `src/{main,api,terminal}.ts`, `build.mjs` using esbuild. Add `xterm` and `@xterm/addon-fit` deps.
- DoD: `node build.mjs` produces `relay/internal/server/webfs/{index.html,bundle.js}`.
- Deps: T0.1.

### T3.5.2 Embed and serve
- Scope: `relay/internal/server/static.go` and route registration in `relay/internal/server/server.go`.
- Action: `//go:embed all:webfs` directive; serve `/web/*` and the `/`->`/web/` redirect WITHOUT the auth middleware so the SPA shell can load before the user pastes a bearer. Only `/api/*` is gated. Cloudflare Access still enforces identity at the edge for the public hostname.
- DoD: `curl http://localhost:18822/web/index.html` returns 200 without credentials; `curl http://localhost:18822/api/health` still returns 401 without credentials. Loading in a browser displays the SPA shell.
- Deps: T3.5.1, T3.2.

### T3.5.3 Web client features
- Scope: `relay/web/src/`.
- Action: Profiles in localStorage, sessions panel polling, multi-tab terminals with `addon-fit`, reconnect with backoff, settings modal (LAN bearer + Service Token override mode).
- DoD: Manual smoke against a running relay: create two sessions, switch tabs, kill one, reload the page, profile auto-restored.
- Deps: T3.5.2, T2.3.

## Phase 4 — Flutter MVP

### T4.1 Scaffold mobile app
- Scope: `mobile/`.
- Action: `flutter create mobile --org app.httpssh --project-name httpssh_mobile`. Add deps: `xterm`, `web_socket_channel`, `dio`, `flutter_secure_storage`, `flutter_localizations`, `intl`, `riverpod`, `flutter_web_auth_2`.
- DoD: `flutter run` shows the default counter on Android and iOS.
- Deps: T0.1.

### T4.2 Localization wiring
- Scope: `mobile/lib/l10n/{app_en.arb,app_ja.arb}`, `mobile/l10n.yaml`, `mobile/lib/main.dart`.
- Action: Set up `flutter gen-l10n`. App locale follows `Localizations.localeOf` and falls back to English. Add a `LocaleNotifier` Riverpod provider for manual override.
- DoD: A test widget displays the right string for both locales.
- Deps: T4.1.

### T4.3 Profiles screen + editor + secure storage
- Scope: `mobile/lib/screens/profiles_screen.dart`, `mobile/lib/auth/`, `mobile/lib/models/profile.dart`.
- Action: CRUD profiles. Secrets in `flutter_secure_storage`; non-secret fields in `shared_preferences` (or all in secure storage if simpler).
- DoD: Add a profile, kill the app, reopen, profile is still there. Secrets are not visible in `shared_preferences` JSON dumps.
- Deps: T4.2.

### T4.4 REST client + Sessions screen
- Scope: `mobile/lib/api/`, `mobile/lib/screens/sessions_screen.dart`.
- Action: `dio` client with auth-header injection per profile. Sessions list, create, kill, rename.
- DoD: List populates; create spawns a session; kill removes it.
- Deps: T4.3, T2.3.

### T4.5 Single-tab Terminal screen
- Scope: `mobile/lib/screens/terminal_screen.dart`, `mobile/lib/terminal/`.
- Action: One xterm.dart `Terminal` + `web_socket_channel` connection. Implement protocol (`replay`, `out`, `in`, `resize`, `ping`/`pong`). Soft-keyboard helper bar.
- DoD: Phone or emulator types `Get-Date`, output renders correctly. Rotate device → resize fires.
- Deps: T4.4, T1.4, T2.2.

### T4.6 Reconnect with backoff
- Scope: `mobile/lib/state/`, `mobile/lib/screens/terminal_screen.dart`.
- Action: Detect WS close; show "Reconnecting..." banner; retry 1s/2s/5s/10s/30s. On reconnect, accept the `replay` and resume.
- DoD: Toggle airplane mode briefly mid-command; verify reconnect + replay.
- Deps: T4.5.

## Phase 5 — Multi-Tab Flutter

### T5.1 TabController + multi-session state
- Scope: `mobile/lib/screens/terminal_screen.dart`, `mobile/lib/state/`.
- Action: Replace single-session screen with `TabController`. Each tab owns its own controller, terminal, and WS. Tab strip, `+` button, status dot.
- DoD: Two tabs running concurrent commands without crosstalk.
- Deps: T4.6.

### T5.2 Tab management UX
- Scope: `mobile/lib/screens/terminal_screen.dart`.
- Action: Close tab, rename tab, reorder by drag, fullscreen toggle.
- DoD: Manual UX walkthrough; widget tests for the tab strip.
- Deps: T5.1.

## Phase 6 — Cloudflare E2E

### T6.1 Document and execute Cloudflare setup
- Scope: `docs/cloudflare-setup.md` (already drafted; verify accuracy).
- Action: Operator follows the doc on a real account; report any drift. Update doc accordingly.
- DoD: `https://pwsh.<domain>/api/health` returns 200 with valid Service Token headers.
- Deps: T3.3.

### T6.2 Mobile profile against Cloudflare
- Scope: app config only.
- Action: Add a `CF Token` profile pointing at `https://pwsh.<domain>`. Verify health, sessions, multi-tab.
- DoD: Wi-Fi off → connection works over LTE.
- Deps: T6.1, T5.1.

### T6.3 Web client against Cloudflare
- Scope: none (the existing web client should just work).
- Action: Open `https://pwsh.<domain>/web/` in Chrome → Google login → terminal usable.
- DoD: Same as the smoke test in `docs/cloudflare-setup.md`.
- Deps: T6.1, T3.5.3.

## Phase 7 — Polish

### T7.1 App icon, splash, theme
- Scope: `mobile/android/`, `mobile/ios/`, `mobile/lib/theme/`.
- Action: App icon, splash, theme picker, font size, terminal palette picker.
- DoD: Builds for both platforms produce branded artifacts.
- Deps: T5.2.

### T7.2 Coverage push
- Scope: `relay/internal/{session,auth,config}`.
- Action: Add tests until coverage on those packages is ≥ 80 %.
- DoD: `go test -cover ./...` reports the threshold.
- Deps: T3.3.

### T7.3 Crash and error handling
- Scope: `relay/internal/server/`, `mobile/lib/`.
- Action: panic recovery middleware in the relay; user-friendly error states in the app (no raw stack traces).
- DoD: Manual fault-injection passes (e.g., kill ConPTY out from under the session).
- Deps: T6.2.
