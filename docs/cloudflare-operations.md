# Cloudflare Operations Runbook

This is the day-to-day playbook for running `httpssh` behind Cloudflare
Tunnel and Cloudflare Access. The one-time setup lives in
[`docs/cloudflare-setup.md`](cloudflare-setup.md).

The runbook assumes the deployment shape used in this project:

- a Cloudflare Tunnel created on the Windows host with `cloudflared` registered as a Windows service;
- one Cloudflare Access **self-hosted application** at `pwsh.<domain>` that covers `/api/...` and `/web/...`;
- two Access policies on that application — Service Token (mobile app) and email + Google IdP (browser);
- the relay running as the `httpssh-relay` Windows service with `auth.lan_bearer` set.

## Auth Model At A Glance

The LAN bearer is **required on every request**, regardless of how the
request reaches the relay. Cloudflare Access only adds an outer edge
layer (identity); it never replaces the bearer.

| Surface | Address | Edge layer (Cloudflare) | Relay layer (always) |
|---|---|---|---|
| LAN, direct | `http://<host>:18822/...` | — (LAN trust) | `Authorization: Bearer <lan_bearer>` (REST) / `?token=<lan_bearer>` (WS) |
| Cloudflare, mobile app, Service Token | `https://pwsh.<domain>/...` | `CF-Access-Client-Id` + `CF-Access-Client-Secret` | `Authorization: Bearer <lan_bearer>` |
| Cloudflare, mobile app, Google SSO | `https://pwsh.<domain>/...` | `CF_Authorization` cookie (captured by in-app webview) | `Authorization: Bearer <lan_bearer>` |
| Cloudflare, browser | `https://pwsh.<domain>/web/` | `CF_Authorization` cookie (set by Google SSO) | `Authorization: Bearer <lan_bearer>` (sent by SPA from localStorage) |

## Daily / Routine

### Health check

```powershell
# Loopback
curl http://127.0.0.1:18822/api/health -H "Authorization: Bearer <lan_bearer>"

# Public
curl https://pwsh.<domain>/api/health `
  -H "CF-Access-Client-Id: <id>" `
  -H "CF-Access-Client-Secret: <secret>" `
  -H "Authorization: Bearer <lan_bearer>"
```

Both should return `{"status":"ok",...}`. A 401 from the public path almost always means the headers are missing/wrong; a 302 redirect to a Cloudflare login page means the request fell through to a different policy.

### Service status

```powershell
Get-Service httpssh-relay
Get-Service cloudflared
```

Both should be `Running`. Restart with `Restart-Service <name>`.

### Tail relay logs

The relay logs JSON to stdout. The current Windows service wrapper does not
configure a dedicated Event Log sink, so use foreground/dev mode when you need
live structured logs:

```powershell
.\httpssh-relay.exe --config config.yaml |
  jq -c 'select(.event=="auth_denied" or .event=="session_create" or .event=="session_end")'
```

### Cloudflare-side logs

- **Access events** (allow/deny) — Zero Trust → **Logs → Access**. Filter by application = `httpssh`.
- **Tunnel events** (connect/disconnect) — Zero Trust → **Networks → Tunnels** → click the tunnel → **Connectors** tab.
- **Live tail** — on the Windows host: `cloudflared tunnel info <name>` and `cloudflared tunnel run <name> --loglevel debug` (stop the service first).

## Periodic / Maintenance

### Update `cloudflared`

```powershell
cloudflared update
Restart-Service cloudflared
```

The tunnel stays registered across upgrades; only the connector binary changes.

### Update the relay

```powershell
Stop-Service httpssh-relay
Copy-Item .\relay\dist\httpssh-relay.exe "C:\Program Files\httpssh\httpssh-relay.exe" -Force
Start-Service httpssh-relay
```

Open WebSockets break across a restart. Sessions are in-memory only and are lost; mobile clients see a `Reconnecting...` banner and then a fresh session list.

### Rotate the LAN bearer

1. Generate a new value: `[guid]::NewGuid().ToString("N") + [guid]::NewGuid().ToString("N")` (or use `openssl rand -hex 32`).
2. Edit `config.yaml`, replace `auth.lan_bearer`.
3. `Restart-Service httpssh-relay`.
4. Update every LAN profile in the mobile app and the LAN bearer in the web client's Settings dialog.

The LAN bearer never leaves the host filesystem and the device or browser origin that uses it. The mobile app stores it in OS-level secure storage; the web client stores it in that origin's localStorage.

### Rotate the Cloudflare Service Token

1. Zero Trust → **Access → Service Auth → Service Tokens** → token row → **Refresh**.
2. Cloudflare displays the new Client Secret **once** — copy it.
3. Update the mobile app's CF Token profile (paste new Client ID + Client Secret).

The old token is invalidated immediately at the edge. There is no need to restart the relay.

### Add a trusted user (browser SSO)

1. Zero Trust → **Access → Applications → `httpssh`** → policy "Owner email" → **Edit**.
2. Add the Google account email under **Include → Emails**.
3. Save.

The change applies to new browser logins; existing sessions stay valid until their **Session duration** expires.

### Revoke a user immediately

1. Zero Trust → **Access → Applications → `httpssh`** → policy "Owner email" → remove the email and **Save**.
2. Zero Trust → **My Team → Users** → the user → **Revoke session**.
3. If the user is on a service token, refresh that token (above).

## Troubleshooting Matrix

| Symptom | Most likely cause | Where to look |
|---|---|---|
| Web client shows `Create failed: Failed to fetch` (or `Kill failed: Failed to fetch`) on the public hostname only, and reloading the page makes it work for a while | Cloudflare Access session cookie expired mid-request, or `cloudflared` did a brief reconnect. The web client now auto-retries once and surfaces "Cloudflare Access session expired. Reload the page to sign in again." instead of the raw browser message — if you still see "Failed to fetch", the second attempt also failed and the cause is genuinely network-level (DNS / TLS / cloudflared down). | DevTools → Network: confirm whether the request reached Cloudflare at all. If it did and you got an HTML response from `/cdn-cgi/access/login/...`, the cookie expired — reload the page and sign in again. |
| `530`/`1033` page on `pwsh.<domain>` | `cloudflared` service down or tunnel not connected | `Get-Service cloudflared`; Zero Trust → Tunnels → Connectors |
| Browser hits `https://pwsh.<domain>/web/` and gets a Cloudflare login forever | Email not in any Allow policy, or Google IdP misconfigured | Zero Trust → Logs → Access; reconfigure Google IdP if needed |
| `401 unauthorized` JSON from the relay (browser, app, or curl) | LAN bearer missing or wrong | Verify the bearer used by the client matches `config.yaml` `auth.lan_bearer` |
| Cloudflare login page returned (302) when curl-ing | Service Token rotated or wrong / not sent at all | Refresh the Service Token; verify `CF-Access-Client-Id` and `CF-Access-Client-Secret` are both attached |
| Browser SPA loads but `/api/*` 401s | Bearer not in the Settings dialog yet, or wrong | Open Settings, paste the LAN bearer, Save; refresh the page |
| WebSocket immediately closes with code 1008 | Client did not negotiate the `httpssh.v1` subprotocol | The first-party clients always do; if hitting from a custom client, add `Sec-WebSocket-Protocol: httpssh.v1` |
| Sessions disappear after a Windows reboot | Expected — sessions are in-memory only | Persistent sessions are out of scope for v1; see [Data model](data-model.md). |
| Relay starts but logs `listen failed bind: address already in use` | Some other process owns 18822 | `Get-NetTCPConnection -LocalPort 18822`; either stop that process or change `listen` |

## Decision: When To Bypass Cloudflare?

Use the LAN path (`http://<lan-ip>:18822` + bearer) when:

- both endpoints are on the same trusted Wi-Fi/wired LAN segment;
- minimum latency matters (no Cloudflare edge round-trip);
- you do not want to depend on the Cloudflare edge for connectivity (e.g. during Cloudflare incidents).

Use the Cloudflare path otherwise — it is the only path that works off-LAN and the only one with edge-level identity.

## Production Mode Checklist

- [ ] `config.yaml` has a long, persistent `auth.lan_bearer` (32+ chars).
- [ ] `httpssh-relay` and `cloudflared` are both `Running` with `StartType = Automatic`.
- [ ] Two Access policies exist on `pwsh.<domain>`: Service Token (Allow) and email + Google IdP (Allow).
- [ ] Smoke tests in `docs/cloudflare-setup.md` §8 pass.
- [ ] At least one mobile app profile is configured with **Bearer + CF Service Token** (or Bearer + CF browser SSO) against `https://pwsh.<domain>`.
- [ ] `https://pwsh.<domain>/web/` loads in an incognito browser after Google login, and the bearer pasted into Settings drives a working terminal.
