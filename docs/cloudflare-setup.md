# Cloudflare Setup

This guide walks the operator through configuring Cloudflare Tunnel and Cloudflare Access for `httpssh`. It assumes the operator uses their own Cloudflare account and already manages their own domain on Cloudflare (Free plan is sufficient). Do not rely on a shared project-owned Cloudflare account or domain.

## Prerequisites

- A Cloudflare account with the target domain (e.g., `example.com`) on Cloudflare DNS.
- A Cloudflare Zero Trust team set up (`https://one.dash.cloudflare.com/`). Free tier is fine.
- The Windows host where the relay runs has internet access (outbound HTTPS only).
- An admin account on Google Workspace or a personal Google account if Google OAuth is desired.

## 1. Install `cloudflared`

1. Download the latest Windows AMD64 binary from `https://github.com/cloudflare/cloudflared/releases`.
2. Place `cloudflared.exe` in `C:\Program Files\Cloudflare\cloudflared\`.
3. From an elevated PowerShell:
   ```powershell
   cloudflared --version
   cloudflared service install <TUNNEL_TOKEN>   # token comes from step 2 below
   ```
   The relay does not depend on this; cloudflared is an independent Windows service.

## 2. Create the Tunnel

1. In the Zero Trust dashboard, go to **Networks → Tunnels → Create a tunnel**.
2. Select connector type "Cloudflared", name the tunnel `httpssh-<hostname>`, save.
3. Copy the tunnel token shown on the install page; this is the `<TUNNEL_TOKEN>` for `cloudflared service install`.
4. On the **Public Hostname** tab of the tunnel, add:
   - Subdomain: `pwsh`
   - Domain: `example.com`
   - Service: `HTTP` `localhost:18822`
   - **Additional application settings → TLS → No TLS Verify**: not needed (HTTP backend).
5. Save. Cloudflare DNS automatically creates the proxied CNAME `pwsh.example.com → <tunnel-id>.cfargotunnel.com`.

WebSocket support is enabled by default in Cloudflare Tunnel and does not require a special toggle.

## 3. Create the Cloudflare Access Application

1. In Zero Trust, go to **Access → Applications → Add an application → Self-hosted**.
2. Application name: `httpssh`. Session duration: `24 hours` (browser SSO sessions); shorter is fine.
3. Application domain: `pwsh.example.com` (root path; leave path blank to cover all routes including `/api/...` and `/web/...`).
4. Under **Identity providers**, enable:
   - **Google** (set up the Google IdP integration first if you haven't; see step 4).
   - **One-time PIN** (optional, useful as a fallback).
5. Save. Continue to the policy tab.

## 4. Configure the Google Identity Provider (one-time per team)

1. Zero Trust → **Settings → Authentication → Login methods → Add → Google**.
2. Follow Cloudflare's instructions to create a Google Cloud OAuth Client ID/Secret and paste them into Cloudflare.
3. Test the connection from the same screen.

## 5. Create the Access Policies

The same application gets two policies that are evaluated as a logical OR.

### Policy A — Service Token (mobile app)

- Action: **Allow**.
- Include: **Service Token** → pick the token you will create in step 6.
- Save.

### Policy B — Owner email (browser/Google)

- Action: **Allow**.
- Include: **Emails** → your allowed email address (and any additional trusted emails) in your own Cloudflare account.
- Require: **Login methods** → `Google` (forces the identity to come from Google rather than One-time PIN, which is harder to phish).
- Save.

Order does not matter; either policy granting **Allow** lets the request through.

## 6. Create the Service Token (for the mobile app)

1. Zero Trust → **Access → Service Auth → Service Tokens → Create Service Token**.
2. Name: `httpssh-mobile`.
3. Duration: `Forever` (or as desired).
4. Save. Cloudflare displays the **Client ID** and **Client Secret** ONCE. Copy both into the mobile app's profile or store them in a password manager. The Client Secret cannot be retrieved later.
5. The token automatically satisfies Policy A.

## 7. Wire the Relay to Cloudflare Access

The relay only needs the LAN bearer. Cloudflare Access does its own
identity check at the edge before any request reaches the relay; the
relay then validates the bearer.

```yaml
auth:
  lan_bearer: "<32+ random hex chars>"
```

There are no Cloudflare-specific switches on the relay side: clients
add the Cloudflare credentials they need to satisfy Access (Service
Token for the mobile app, Google SSO cookie for the browser), and they
also send the LAN bearer. Both the bearer and the Cloudflare layer are
required.

If you change `lan_bearer`, restart the service:

```powershell
Restart-Service httpssh-relay
```

## 8. Smoke Tests

### LAN smoke

```powershell
curl http://127.0.0.1:18822/api/health -H "Authorization: Bearer <lan_bearer>"
# {"status":"ok",...}
```

### External via Service Token (mobile app path)

The mobile app needs to satisfy two layers: the Cloudflare Access edge
(Service Token) and the relay (LAN bearer). Both go on the same
request:

```powershell
curl https://pwsh.example.com/api/health `
  -H "CF-Access-Client-Id: <id>" `
  -H "CF-Access-Client-Secret: <secret>" `
  -H "Authorization: Bearer <lan_bearer>"
# {"status":"ok","version":"...","uptimeSeconds":...}
```

Without the Service Token: Cloudflare returns a login page (302).
Without the bearer (but with a valid Service Token): the relay returns
`401 unauthorized` as JSON.

### External via browser (Google SSO path)

Open `https://pwsh.example.com/web/`. Cloudflare redirects to Google,
log in as the allow-listed email, redirects back. The relay then
serves the static web client. In the **Settings** dialog, paste the
LAN bearer once; the SPA stores it in localStorage and includes it on
subsequent REST calls and the WebSocket URL.

The Cloudflare cookie satisfies the edge; the bearer satisfies the
relay.

### Mobile app

In the Flutter app, **Add profile**:

- Name: `Home (Cloudflare)`
- Base URL: `https://pwsh.example.com`
- LAN bearer (required): paste the same value you set in the relay's
  `config.yaml`.
- Authentication: **Bearer + CF Service Token**
- `CF-Access-Client-Id`: the Client ID from §6
- `CF-Access-Client-Secret`: the Client Secret from §6

Or, for the Google-SSO path: pick **Bearer + CF browser SSO** instead
of pasting a Service Token. The first connection opens an in-app
browser for Google login; the cookie is captured and reused.

Tap the profile, then **+** to spawn a session.

## 9. Rotation and Revocation

- Rotate Service Token: Zero Trust → Service Tokens → `httpssh-mobile` → **Refresh**. Update the mobile app's profile with the new Client Secret. Old token immediately invalidated at the edge.
- Rotate LAN bearer: edit `config.yaml`, set a new value, restart the service. Existing WebSockets stay open until the next request.
- Revoke a user: Zero Trust → Access → Applications → `httpssh` → Policies → remove the email or the token. Existing browser sessions stay valid until their session duration expires.

## 10. Operational Notes

- Cloudflare Tunnel automatic load-balancing across multiple `cloudflared` instances is supported but not used here (single instance is sufficient).
- Cloudflare Access logs are visible in **Logs → Access** and useful when diagnosing 401s.
- The relay should bind to `127.0.0.1:18822` only (not `0.0.0.0`) when LAN access is not desired; if LAN access is desired bind to the LAN interface IP and rely on the bearer.
- Cloudflare Access sessions are bound to the user's browser; using "Incognito" mode forces a fresh login.
