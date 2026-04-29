# Acceptance Criteria

These criteria define "v1 done." They are the bar for declaring the project shippable to its single operator. Acceptance is binary per item.

## Project-Level

- AC-P1 — All artifacts in this repo are written in English (no Japanese in code, comments, docs, or commit messages).
- AC-P2 — The Flutter app's UI strings are all routed through ARB files, with both `app_en.arb` and `app_ja.arb` populated for every key.
- AC-P3 — `go test ./...` from `relay/` passes, and `go build -o dist/httpssh-relay.exe ./cmd/httpssh-relay` produces a single Windows relay binary.
- AC-P4 — `flutter build apk --release` succeeds from `mobile/`; GitHub Release APK builds use the configured Android release signing secrets. `flutter build ios --release --no-codesign` succeeds on macOS.
- AC-P5 — The relay binary embeds `relay/internal/server/webfs/` and serves the web client at `/web/`.

## Relay (Functional)

- AC-R1 — `POST /api/sessions` with `{"shell":"pwsh","cols":120,"rows":40}` returns 201 and a session ID; the underlying `pwsh.exe` is running.
- AC-R2 — `GET /api/sessions` lists the session created by AC-R1.
- AC-R3 — Opening a WebSocket to `/api/sessions/{id}/io` returns a `replay` frame as the very first frame, then begins streaming `out` frames live.
- AC-R4 — Sending `{"t":"in","d":"Get-Date\r"}` produces `out` frames containing the current date string within 200 ms on LAN.
- AC-R5 — Sending `{"t":"resize","c":160,"r":50}` causes `tput cols` inside `pwsh` to print `160`.
- AC-R6 — Sending `{"t":"ping"}` results in a `{"t":"pong"}` reply within 100 ms on LAN.
- AC-R7 — Disconnecting the WebSocket and reconnecting within 30 s results in a fresh `replay` frame containing output produced during the gap.
- AC-R8 — Two simultaneous WebSocket subscribers on the same session both receive identical `out` streams.
- AC-R9 — `DELETE /api/sessions/{id}` returns 204 and the session is no longer in `GET /api/sessions`.
- AC-R10 — A session with zero subscribers and `idle_timeout=5s` is killed within 65 s of last I/O.
- AC-R16 — With `files.roots` configured, `GET /api/files/roots`, `GET /api/files/list`, and `GET /api/files/read` require the LAN bearer and return root, directory, and text-file data.
- AC-R17 — `/api/files/*` rejects root escape attempts, binary files, and files larger than `files.max_file_bytes`.

## Relay (Non-Functional)

- AC-R11 — Round-trip time for `ping`→`pong` over LAN is < 50 ms (median over 100 samples).
- AC-R12 — `GET /api/health` over LAN returns < 50 ms (median).
- AC-R13 — Round-trip keystroke→echo through Cloudflare Tunnel is < 250 ms over typical home internet.
- AC-R14 — `go test -race ./...` on Windows passes.
- AC-R15 — Memory growth across 1 hour of streaming output (≥ 1 MB total) plateaus near `scrollback_bytes` per session, with no monotonic increase.

## Auth

- AC-A1 — A request to `/api/health` with no headers returns 401.
- AC-A2 — A request to `/api/health` with a wrong bearer returns 401.
- AC-A3 — A request with the correct `Authorization: Bearer <lan_bearer>` returns 200.
- AC-A4 — A WebSocket upgrade with `?token=<lan_bearer>` and the `httpssh.v1` subprotocol completes; the same upgrade without the token is rejected.
- AC-A5 — A request that carries `Cf-Access-Jwt-Assertion` but no bearer still returns 401 (the relay does not trust Cf-* headers in isolation).
- AC-A6 — Service Token rotation: after refreshing the Service Token in Cloudflare, requests with the old credentials are rejected at the edge within 60 s (Cloudflare-side; the relay's bearer requirement is unaffected).
- AC-A7 — `/web/` and `/web/index.html` return 200 without any credentials; `/api/health` continues to return 401 without credentials.

## Cloudflare End-to-End

- AC-C1 — `https://pwsh.<domain>/api/health` with valid Service Token headers and the relay LAN bearer returns 200.
- AC-C2 — `https://pwsh.<domain>/web/` opens in a fresh browser, completes Google login as the allow-listed email, and renders the SPA.
- AC-C3 — A WebSocket through Cloudflare with the relay LAN bearer plus any required Cloudflare edge credential establishes successfully and survives a 60 s idle period (kept alive by the client's 20 s ping).

## Mobile (Functional)

- AC-M1 — App opens to a Profiles screen; "Add profile" creates a profile with a chosen auth mode.
- AC-M2 — Tapping a profile loads the Sessions screen with the live list from `GET /api/sessions`.
- AC-M3 — Creating a session opens a Terminal screen and a working tab within 3 s.
- AC-M4 — Adding a second tab opens a second WebSocket; both tabs accept input independently.
- AC-M5 — Backgrounding the app for 60 s and returning auto-reconnects all tabs.
- AC-M6 — Toggling airplane mode for 10 s mid-command produces a "Reconnecting" banner and recovers within 30 s without losing earlier output (visible in scrollback after replay).
- AC-M7 — Rotating the device sends a resize and the prompt re-flows correctly.
- AC-M8 — Killing a session from the Sessions screen removes it from the list and closes any open tab.
- AC-M12 — The mobile file browser lists configured roots, navigates directories, opens text files, syntax-highlights supported code/config files, searches within a file, copies content, and persists bookmarks per profile.

## Mobile (Localization)

- AC-M9 — Switching the app's language to Japanese changes every visible string except the product name `httpssh`.
- AC-M10 — Switching back to English flips them all back.
- AC-M11 — `flutter gen-l10n` finds zero missing keys between `app_en.arb` and `app_ja.arb`.

## Web Client

- AC-W1 — `/web/index.html` loads without relay bearer auth, then lists sessions and opens an existing or new session in a tab after the LAN bearer is saved in Settings.
- AC-W2 — Resizing the browser window resizes the terminal via the fit addon and sends a `resize` frame.
- AC-W3 — Reloading the page preserves the LAN bearer and optional developer Service Token values from localStorage.
- AC-W4 — Closing the browser and reopening it lists server-side sessions after auth; clicking a session re-attaches successfully.
