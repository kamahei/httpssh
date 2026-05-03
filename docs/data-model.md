# Data Model

The relay holds no persistent storage in v1. The "domain entities" below describe in-memory objects on the relay and locally persisted objects on each client.

## Server-Side Entities (in-memory only)

### `Session`

- Purpose: A live PowerShell process attached to a ConPTY, with subscribers and a scrollback buffer.
- Identifier: `ID` — a 32-character lowercase hex string generated from 16 random bytes at creation.
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
  - `subs map[*subscriber]struct{}` — current WebSocket subscribers.
  - `cwd string` — last working directory reported by the shell prompt via OSC 9;9. Empty until the first prompt fires; remains set across `cd` to non-FileSystem providers (the OSC wrapper only emits for FileSystem, so the previous value is retained).
  - `cwdTracker *cwdTracker` — stateful OSC 9;9 parser that scans every PTY read; touched only by the pump goroutine.
  - `mu sync.Mutex` — guards `subs`, `LastIO`, and `cwd`.
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
- Contents exclude ConPTY resize repaint bursts. Those bytes are not streamed or retained because they redraw already-visible screen contents instead of representing new shell output.
- Behavior on overflow: oldest bytes are overwritten silently; the replay frame on reconnect therefore reflects "the last `cap` bytes of output" rather than "everything since session start."

### `Manager`

- Purpose: Owns all sessions in the process.
- Fields:
  - `byID map[string]*Session`.
  - `mu sync.RWMutex`.
  - `idleTimeout time.Duration`.
  - `scrollbackBytes int`.
  - `shellResolver func(name string) (string, []string, error)` — resolves `auto`, `pwsh`, `powershell`, or `cmd` to an executable path plus the shell-specific bootstrap arguments (e.g. `-NoLogo -NoExit -EncodedCommand <base64>` for pwsh) that install the OSC 9;9 prompt wrapper.
- Operations: `Create`, `Get`, `List`, `Rename`, `Kill`, `Shutdown`, and background reaping on the configured `reap_interval`. Resizes are performed on the `Session` returned by `Get`.

### `FileRoot`

- Purpose: A configured read-only filesystem root exposed to authenticated clients.
- Identifier: `ID` from `files.roots[].id`.
- Fields:
  - `ID string` — stable root id used by `/api/files/*`.
  - `Name string` — display label for mobile UI.
  - `Path string` — absolute, symlink-resolved Windows directory path.
- Invariants:
  - Root ids are unique and contain no path separators.
  - Clients can only list/read paths that remain under the root after cleaning and symlink resolution.
  - The relay never writes, deletes, renames, or uploads files through this API.

### Session-scoped file root (synthetic)

- Purpose: An ad-hoc, per-request "root" that is the session's current `cwd`.
- Identifier: `session:<sessionID>` in API responses.
- Storage: not persisted; computed on each request from `Session.cwd`.
- Invariants:
  - The base path is re-read from the live session at each request, so a `cd` in the shell takes effect immediately on the next list/read call.
  - The relay enforces the same in-jail check as configured roots: paths must remain under the session's CWD after cleaning and symlink resolution.
  - The synthetic root does not require any `files.roots` configuration; it relies on the fact that the session owner already has shell access to that directory.

### `subscriber`

- Purpose: One WebSocket subscriber to a session.
- Fields:
  - `out chan ServerFrame` — buffered (default 256) channel for outbound frames.
  - `ctx context.Context`.
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

#### `FileBookmark`

- Purpose: Per-profile shortcut to a file browser location.
- Storage: Shared preferences, keyed by profile id.
- Fields:
  - `rootId String`
  - `path String` — root-relative path, empty for the root.
  - `label String`

### Web Client (browser localStorage)

- `httpssh.lanBearer` — stored in localStorage and sent as `Authorization: Bearer ...` on REST and `?token=...` on WebSocket connections for the current origin.
- `httpssh.cfClientId` / `httpssh.cfClientSecret` — for the manual Service Token testing mode (developer use only, off by default).
- Active session list comes from the server; not persisted client-side.

## Access Patterns

- "List my sessions" → `Manager.List()` returns a snapshot (RLock + copy of metadata, no buffers).
- "Attach to session X" → `Manager.Get(id)` then register a subscriber; one allocation of a `[]byte` snapshot of `Scrollback` for the replay frame.
- "Send keystroke" → directly `conpty.Write(payload)`; no intermediate queueing.
- "Reap idle sessions" → goroutine ranges `byID` under RLock, collects `Detached` sessions whose `LastIO` is older than `idleTimeout` minus current sub count == 0; upgrades to Lock to remove and `Kill`.
- "Multi-subscriber output" → PTY pump iterates `Session.subs` and non-blocking-sends to each subscriber `out` channel. If the channel is full, the slow subscriber is canceled; the session continues for the rest.
- "List files" → resolve configured root, clean requested path, resolve symlinks, reject paths outside the root, return sorted directory entries.
- "Read text file" → resolve path using the same rule, enforce `files.max_file_bytes`, reject binary/NUL content, decode UTF-8, UTF-16 BOM, or Shift_JIS.
- "List files at session CWD" → look up `Session.cwd`; if empty, reject with `cwd_unknown`; otherwise treat the CWD as a synthetic root and apply the same path-cleaning, symlink-resolution, and in-jail check as a configured root. Each request re-reads `Session.cwd`, so a shell-side `cd` takes effect on the next call.

## Validation Rules

- Session create: `cols ∈ [1,500]`, `rows ∈ [1,200]`, `shell ∈ {auto, pwsh, powershell, cmd}` resolved to absolute paths against an allow-list (the relay rejects arbitrary executables to avoid auth-bypass into RCE-unrelated binaries). If the API request omits `shell`, the server currently defaults it to `pwsh`.
- File roots: every configured root has non-empty `id`, `name`, and absolute `path`; ids are unique and cannot contain path separators. `files.max_file_bytes` must be > 0.
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
