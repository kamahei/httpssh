# API Contracts

All HTTP and WebSocket endpoints are exposed by the relay under the same listener (`127.0.0.1:18822` by default; reached publicly via `https://pwsh.<domain>/`). Every `/api/*` endpoint and WebSocket upgrade goes through the auth middleware described in `docs/architecture.md` and `docs/cloudflare-setup.md`.

## Conventions

- Content-Type for all JSON bodies: `application/json; charset=utf-8`.
- All timestamps are ISO 8601 UTC (`2026-04-29T12:34:56Z`).
- Error responses use the shape `{"error": {"code": "<machine_readable>", "message": "<human_readable>"}}`.
- Auth: every request MUST carry the LAN bearer, either as
  `Authorization: Bearer <lan_bearer>` (REST) or `?token=<lan_bearer>`
  (WebSocket; browsers cannot set custom headers on the upgrade).
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
      "id": "01HXY...",
      "title": "pwsh 2026-04-29 14:01",
      "shell": "C:\\Program Files\\PowerShell\\7\\pwsh.exe",
      "cols": 120,
      "rows": 40,
      "createdAt": "2026-04-29T14:01:02Z",
      "lastIo": "2026-04-29T14:05:11Z",
      "subscribers": 1
    }
  ]
}
```

### `POST /api/sessions`

Create a new session.

Request:
```json
{
  "shell": "pwsh",          // optional; one of "pwsh"|"powershell"|"cmd"; default = pwsh if available, else powershell
  "cols": 120,              // optional; default 80
  "rows": 40,               // optional; default 24
  "title": "logs"           // optional; defaults to "<shell> <timestamp>"
}
```

Response:
```http
HTTP/1.1 201 Created
Location: /api/sessions/01HXY...

{ "id": "01HXY...", "title": "logs", "shell": "...", "cols": 120, "rows": 40, "createdAt": "..." }
```

Errors: `400 invalid_dimensions`, `400 invalid_shell`, `503 spawn_failed`.

### `PATCH /api/sessions/{id}`

Rename a session.

Request: `{"title":"new name"}`. Response: `200 OK` with the updated session object. `404 not_found` if the session does not exist.

### `DELETE /api/sessions/{id}`

Kill the session. Returns `204 No Content` on success, `404 not_found` if it does not exist. Idempotent: a second delete returns `404`.

## WebSocket Endpoint

### `GET /api/sessions/{id}/io` (Upgrade: websocket)

Upgrades to a WebSocket connection that speaks the protocol in `docs/protocol.md`.

- Subprotocol: `httpssh.v1`.
- Auth: same headers as REST. Cloudflare validates Service Token / cookie on the upgrade.
- Close codes:
  - `1000` normal close (client requested or server shutting down).
  - `4404` session not found at upgrade time.
  - `4401` auth rejected.
  - `4413` inbound frame too large.
  - `4503` session subscriber buffer overflow (slow client).

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

- Protocol version is encoded in the WebSocket subprotocol name (`httpssh.v1`). The relay refuses upgrades that do not negotiate this subprotocol.
- REST endpoints are unversioned in v1; future breaking changes will introduce `/api/v2/...`. Clients SHOULD treat unknown JSON fields as forward-compatible no-ops.
