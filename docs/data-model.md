# Data Model

The relay holds no persistent storage in v1. The "domain entities" below describe in-memory objects on the relay and locally persisted objects on each client.

## Server-Side Entities (in-memory only)

### `Session`

- Purpose: A live PowerShell process attached to a ConPTY, with subscribers and a scrollback buffer.
- Identifier: `ID` — a 26-character Crockford-encoded ULID generated at creation.
- Fields:
  - `ID string` — primary key inside the relay process.
  - `Title string` — display name; defaults to `"<shell> <yyyy-mm-dd HH:MM>"`; user can rename via `PATCH`.
  - `Shell string` — absolute path to the spawned executable (`pwsh.exe` or `powershell.exe` or `cmd.exe`).
  - `Cols, Rows uint16` — current terminal dimensions; clamped to `1..500` and `1..200`.
  - `CreatedAt time.Time` — UTC timestamp at creation.
  - `LastIO time.Time` — UTC timestamp of the most recent I/O event (PTY read or client write); used for idle GC.
  - `Pty *conpty.ConPty` — the pseudo-console handle.
  - `Cmd *exec.Cmd` — the spawned process.
  - `Scrollback *RingBuffer` — bounded byte buffer (default 4 MiB).
  - `subs map[*WSConn]struct{}` — current WebSocket subscribers.
  - `mu sync.Mutex` — guards `subs` and `LastIO`.
- Lifecycle:
  - `Created` (PTY+process running, 0 subs).
  - `Active` (>= 1 sub).
  - `Detached` (0 subs, idle timer running).
  - `Killed` (terminal: process exited or `Manager.Kill` invoked; entry removed from manager).
- Invariants:
  - Once `Killed`, the entry is removed from `Manager.byID`; the ID is never reused.
  - `Pty` and `Cmd` are non-nil for the entire `Created..Detached` lifetime.
  - `Cols >= 1`, `Rows >= 1`.
- Read paths: `Manager.List()`, `Manager.Get(id)`, the read-side of the PTY pump.
- Write paths: `Manager.Create`, `Manager.Resize`, `Manager.Kill`, the WebSocket input handler, the PTY pump (output → buffer + subs).

### `RingBuffer`

- Purpose: Bounded scrollback for replay on reconnect.
- Fields:
  - `cap int` — capacity in bytes (default 4 MiB).
  - `buf []byte` — backing storage of length `cap`.
  - `head, size int` — write cursor and current valid length.
  - `mu sync.Mutex`.
- Invariants:
  - `0 <= size <= cap`.
  - `Snapshot()` returns a copy (or two slices that the caller concatenates) of the most recent `size` bytes in chronological order.
- Behavior on overflow: oldest bytes are overwritten silently; the replay frame on reconnect therefore reflects "the last `cap` bytes of output" rather than "everything since session start."

### `Manager`

- Purpose: Owns all sessions in the process.
- Fields:
  - `byID map[string]*Session`.
  - `mu sync.RWMutex`.
  - `idleTimeout time.Duration`.
  - `scrollbackBytes int`.
  - `shellResolver func() string` — chooses `pwsh.exe` if available, else `powershell.exe`.
- Operations: `Create`, `Get`, `List`, `Rename`, `Resize`, `Kill`, `Reap` (background goroutine that kills idle sessions on a 60-second tick).

### `WSConn`

- Purpose: One WebSocket subscriber to a session.
- Fields:
  - `conn *websocket.Conn`.
  - `sessID string`.
  - `outCh chan []byte` — buffered (default 256) channel for outbound frames.
  - `cancel context.CancelFunc`.
- Lifecycle: created on `/api/sessions/{id}/io` upgrade, registered in `Session.subs`, removed on disconnect or write error.

## Client-Side Entities (persisted on the device)

### Mobile (Flutter, secure storage)

#### `Profile`

- Purpose: A connection profile the user can pick from.
- Identifier: `id` — UUID v4 generated on creation.
- Fields:
  - `id String`
  - `name String` — user-supplied label (e.g., "Home (LAN)").
  - `baseUrl String` — e.g., `http://192.168.1.20:18822` or `https://pwsh.example.com`.
  - `authMode enum {bearerOnly, bearerPlusServiceToken, bearerPlusBrowserSso}`. Describes the OUTER (Cloudflare-edge) layer only.
  - `lanBearer String` — required for every mode; the relay's only auth check.
  - `cfClientId String?` / `cfClientSecret String?` — present iff `authMode == bearerPlusServiceToken`.
  - `createdAt DateTime`.
- Invariants: secrets (`lanBearer`, `cfClientSecret`) live only in `flutter_secure_storage`; non-secret fields can mirror to shared preferences for fast list rendering.

#### `TabState` (in-memory only on the device)

- Purpose: Per-tab terminal state and reconnect bookkeeping.
- Fields:
  - `profileId String`, `sessionId String`.
  - `terminal Terminal` — xterm.dart instance.
  - `wsState enum {connecting, attached, replaying, live, reconnecting, closed}`.
  - `pendingInput Queue<String>` — typed-but-not-yet-sent characters during reconnect.
  - `lastError String?`.

### Web Client (browser localStorage)

- `lan_bearer` (LAN-only profile) — stored in localStorage; never sent to a Cloudflare-mode origin.
- `cf_service_token_id` / `cf_service_token_secret` — for the manual Service Token testing mode (developer use only, off by default).
- Active session list comes from the server; not persisted client-side.

## Access Patterns

- "List my sessions" → `Manager.List()` returns a snapshot (RLock + copy of metadata, no buffers).
- "Attach to session X" → `Manager.Get(id)` then register a `WSConn`; one allocation of a `[]byte` snapshot of `Scrollback` for the replay frame.
- "Send keystroke" → directly `conpty.Write(payload)`; no intermediate queueing.
- "Reap idle sessions" → goroutine ranges `byID` under RLock, collects `Detached` sessions whose `LastIO` is older than `idleTimeout` minus current sub count == 0; upgrades to Lock to remove and `Kill`.
- "Multi-subscriber output" → PTY pump iterates `Session.subs` and non-blocking-sends to each `outCh`. If a `outCh` is full, the slow subscriber's WS is closed (cannot keep up); the session continues for the rest.

## Validation Rules

- Session create: `cols ∈ [1,500]`, `rows ∈ [1,200]`, `shell ∈ {pwsh, powershell, cmd}` resolved to absolute paths against an allow-list (the relay rejects arbitrary executables to avoid auth-bypass into RCE-unrelated binaries).
- Profile add (mobile): `baseUrl` must start with `http://` or `https://` and parse as a URL; `lanBearer` is required for every mode and must be ≥ 16 chars; `cfClientId`/`cfClientSecret` must be non-empty when `authMode == bearerPlusServiceToken`.
- Frame size: WebSocket inbound frames ≥ 1 MiB are rejected to prevent buffer-bloat attacks.

## Lifecycle Summary

```
Profile (client, persistent)         Session (server, in-memory)
─────────────────────────────        ──────────────────────────
created                              POST /api/sessions  → Created
edited (any field)                   first WS attach     → Active
duplicated                           last WS detach      → Detached
deleted                              idle > timeout      → Killed
                                     DELETE /api/sessions/{id} → Killed
                                     pwsh exits          → Killed
```
