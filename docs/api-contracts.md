# API Contracts

All HTTP and WebSocket endpoints are exposed by the relay under the same listener (`127.0.0.1:18822` by default; reached publicly via `https://pwsh.<domain>/`). Every `/api/*` endpoint and WebSocket upgrade goes through the auth middleware described in `docs/architecture.md` and `docs/cloudflare-setup.md`.

## Conventions

- Content-Type for all JSON bodies: `application/json; charset=utf-8`.
- All timestamps are ISO 8601 UTC (`2026-04-29T12:34:56Z`).
- Error responses use the shape `{"error": {"code": "<machine_readable>", "message": "<human_readable>"}}`.
- Auth: every request MUST carry the LAN bearer, either as
  `Authorization: Bearer <lan_bearer>` or `?token=<lan_bearer>`.
  REST clients should prefer the header. Browser WebSocket clients use
  the query parameter because they cannot set custom headers on the
  upgrade.
  Cloudflare Access is treated as an outer edge layer only and does
  NOT relax this requirement: the mobile app additionally sends
  `CF-Access-Client-Id`/`CF-Access-Client-Secret` (Service Token) or
  the `CF_Authorization` cookie (Google SSO) to satisfy Cloudflare,
  but the relay itself only checks the bearer.
- Idempotency: `GET` and `DELETE` are idempotent; `POST /api/sessions` is not idempotent (each call spawns a new pwsh).

## REST Endpoints

### `GET /api/health`

Returns `200 OK` with `{"status":"ok","version":"<semver>","uptimeSeconds":<int>}`. Used by clients to verify connectivity and auth before attempting a WS upgrade. Goes through auth middleware.

### `GET /api/sessions`

Returns the list of live sessions.

```http
HTTP/1.1 200 OK
Content-Type: application/json

{
  "sessions": [
    {
      "id": "4f3c2a1d9e8b7c6a554433221100ffee",
      "title": "pwsh 2026-04-29 14:01",
      "shell": "C:\\Program Files\\PowerShell\\7\\pwsh.exe",
      "cols": 120,
      "rows": 40,
      "createdAt": "2026-04-29T14:01:02Z",
      "lastIo": "2026-04-29T14:05:11Z",
      "subscribers": 1,
      "hostAttached": true,
      "cwd": "C:\\Users\\Owner\\projects\\httpssh",
      "idleTimeoutSeconds": 86400
    }
  ]
}
```

`cwd` is the last working directory observed via the OSC 9;9 prompt
hook (see `docs/architecture.md`). It is omitted (or empty) until the
shell emits its first prompt and when the shell is on a non-FileSystem
PowerShell provider (e.g. `cd HKLM:`).

`idleTimeoutSeconds` is the per-session idle reaper budget. The relay
kills a session that has had zero subscribers and no I/O for this many
seconds. `0` means "never expire" (the session lives until the shell
exits or it is explicitly deleted). Omitting the field at create time
falls back to the relay's `session.idle_timeout` default.

`hostAttached` is `true` when at least one currently-connected
WebSocket subscriber upgraded with `?role=host` (used by the
`httpssh-relay attach` command on the PC side). Mobile and web clients
use this to surface "host PC is attached" in their session pickers.

### `GET /api/sessions/{id}`

Returns the metadata snapshot for a single session.

Response: `200 OK` with the same shape as one entry in the `sessions`
array above. `404 not_found` if the id is unknown.

### `POST /api/sessions`

Create a new session.

Request:
```json
{
  "shell": "pwsh",            // optional; one of "auto"|"pwsh"|"powershell"|"cmd"; omitted means "pwsh"; send "auto" for pwsh-then-powershell fallback
  "cols": 120,                // optional; default 80
  "rows": 40,                 // optional; default 24
  "title": "logs",            // optional; defaults to "<shell> <timestamp>"
  "idleTimeoutSeconds": 86400 // optional; per-session idle reaper budget in seconds. 0 means "never expire". Omit to use the relay's session.idle_timeout default. Capped at 31622400 (366 days).
}
```

Response:
```http
HTTP/1.1 201 Created
Location: /api/sessions/4f3c2a1d9e8b7c6a554433221100ffee

{ "id": "4f3c2a1d9e8b7c6a554433221100ffee", "title": "logs", "shell": "...", "cols": 120, "rows": 40, "createdAt": "...", "idleTimeoutSeconds": 86400 }
```

Errors: `400 invalid_dimensions`, `400 invalid_shell`, `400 invalid_idle_timeout`, `503 spawn_failed`.

### `PATCH /api/sessions/{id}`

Rename a session.

Request: `{"title":"new name"}`. Response: `200 OK` with the updated session object. `404 not_found` if the session does not exist.

### `DELETE /api/sessions/{id}`

Kill the session. Returns `204 No Content` on success, `404 not_found` if it does not exist. Idempotent: a second delete returns `404`.

### `GET /api/files/roots`

List configured read-only file roots. Returns an empty list when file browsing is not configured.

```json
{
  "roots": [
    { "id": "home", "name": "Home" }
  ]
}
```

### `GET /api/files/list?root=<id>&path=<relative-or-absolute>`

List a directory under a configured file root. `path` is optional and defaults to the root. Relative paths are interpreted under the selected root. Absolute paths are accepted only when they resolve under the selected root.

Response:

```json
{
  "root": "home",
  "path": "Documents",
  "entries": [
    {
      "name": "notes.txt",
      "path": "Documents/notes.txt",
      "type": "file",
      "size": 1280,
      "modifiedAt": "2026-04-29T12:34:56Z"
    }
  ]
}
```

Errors: `400 invalid_request`, `400 not_directory`, `403 forbidden`, `404 root_not_found`, `404 not_found`.

### `GET /api/files/read?root=<id>&path=<relative-or-absolute>`

Read a text file under a configured file root. The relay decodes UTF-8, UTF-8 BOM, UTF-16 BOM, and Shift_JIS text. Files larger than `files.max_file_bytes` are rejected.

Response:

```json
{
  "root": "home",
  "path": "Documents/notes.txt",
  "name": "notes.txt",
  "size": 1280,
  "modifiedAt": "2026-04-29T12:34:56Z",
  "encoding": "utf-8",
  "content": "..."
}
```

Errors: `400 invalid_request`, `400 not_text`, `403 forbidden`, `404 root_not_found`, `404 not_found`, `413 file_too_large`.

### `GET /api/sessions/{id}/files/list?path=<relative-or-absolute>`

List a directory rooted at the session's last-known working directory.
The CWD is tracked from the OSC 9;9 prompt hook installed when the
session is spawned (see `docs/architecture.md`). `path` is optional and
defaults to the CWD itself; relative paths are interpreted under the
CWD. Absolute paths are accepted only when they resolve under the CWD.
Navigation above the CWD is rejected with `403 forbidden` — to browse a
different location, the operator types `cd` in the shell and re-issues
the request, which re-reads the (now updated) CWD.

The response shape mirrors `/api/files/list`, with `root` set to
`session:<id>` so clients can distinguish session-scoped responses from
configured-root responses.

Errors: `404 not_found` (session id unknown), `409 cwd_unknown` (the
shell has not yet emitted a prompt), `409 cwd_invalid` (the CWD reported
by the shell is not absolute or no longer exists), `400 not_directory`,
`403 forbidden`.

### `GET /api/sessions/{id}/files/read?path=<relative-or-absolute>`

Read a text file under the session's last-known working directory.
Decoding rules and size limits are identical to `/api/files/read`. The
response shape mirrors `/api/files/read` with `root` set to
`session:<id>`.

Errors: same set as `/api/sessions/{id}/files/list`, plus the standard
read errors (`400 not_text`, `413 file_too_large`).

## WebSocket Endpoint

### `GET /api/sessions/{id}/io` (Upgrade: websocket)

Upgrades to a WebSocket connection that speaks the protocol in `docs/protocol.md`.

- Subprotocol: `httpssh.v1`.
- Auth: same bearer rule as REST. Browser clients normally put the bearer in `?token=...`; Dart/desktop clients may also attach Cloudflare Service Token headers or an SSO cookie for the edge layer.
- Rejection and close behavior:
  - Auth failures and missing sessions are rejected before upgrade as HTTP `401` and `404`.
  - `1000` normal close (client requested or server shutting down).
  - `1008` policy violation when `httpssh.v1` is not negotiated.
  - Inbound frames over the 1 MiB read limit are rejected by the WebSocket library; the relay does not define a custom `44xx` close-code namespace.

## Static Endpoints

### `GET /web/...`

Serves the embedded web client (HTML, JS bundle, CSS, favicon). Static web assets do not require the relay bearer so the SPA shell can load before the operator enters the bearer. The SPA cannot perform session operations until its `/api/*` calls include the bearer. `text/html` for `index.html`, `application/javascript` for the bundle, etc.

## Auth Middleware Decision Table

| Bearer (header or query) | Decision |
|---|---|
| Matches `auth.lan_bearer` exactly (constant-time compare) | Allow |
| Missing | `401 unauthorized` |
| Present but wrong | `401 unauthorized` |

Cloudflare-side headers (`Cf-Access-Jwt-Assertion`,
`Cf-Access-Authenticated-User-Email`, etc.) are ignored by the relay.
Identity at the Cloudflare edge is enforced by the Cloudflare Access
application policy (Service Token for the mobile app, Google SSO for
the browser); the relay's bearer check is an additional independent
layer.

## Versioning

- Protocol version is encoded in the WebSocket subprotocol name (`httpssh.v1`). The relay closes connections that do not negotiate this subprotocol.
- REST endpoints are unversioned in v1; future breaking changes will introduce `/api/v2/...`. Clients SHOULD treat unknown JSON fields as forward-compatible no-ops.
