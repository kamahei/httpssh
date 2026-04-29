# httpssh

`httpssh` exposes a Windows PowerShell console over HTTP and WebSocket so it can be driven from Android, iOS, or a browser without opening an inbound SSH port.

The relay is a Go binary for Windows x64. It runs PowerShell through Windows ConPTY, keeps sessions alive across client disconnects with an in-memory scrollback buffer, and serves a built-in browser test client. The mobile app is a Flutter client with English and Japanese UI.

## What You Get

- Windows relay: `httpssh-relay.exe`, intended to run as a Windows service.
- Android client: `httpssh-mobile-android-<tag>.apk`, published from GitHub Releases.
- Browser test client: served from the relay at `/web/`.
- Cloudflare-ready remote access: Cloudflare Tunnel + Cloudflare Access at the edge, plus the relay's own LAN bearer on every API and WebSocket request.
- LAN mode: direct `http://<host>:18822` access with the same bearer token.

## Security Model

Every API and WebSocket request must carry the LAN bearer token. Cloudflare Access is an outer identity layer only; the relay ignores `Cf-*` headers and still checks the bearer.

The static web client at `/web/` is intentionally loadable without the bearer so the operator can paste the bearer into its Settings dialog. The web client cannot create, attach, list, rename, or kill sessions until API calls include the bearer.

## Quick Start

1. Download the latest Release assets:
   - `httpssh-relay-windows-amd64-<tag>.exe`
   - `httpssh-mobile-android-<tag>.apk`
2. Copy the relay exe to the Windows host as `httpssh-relay.exe`.
3. Copy `relay/config.example.yaml` to `config.yaml` on the Windows host and set a long `auth.lan_bearer`.
4. Start the relay:

   ```powershell
   .\httpssh-relay.exe --config .\config.yaml
   ```

5. Open the browser test client:

   ```text
   http://127.0.0.1:18822/web/
   ```

6. Paste the LAN bearer in Settings, create a session, and run a smoke command such as `Get-Date`.

For service installation, mobile setup, and Cloudflare setup, see the [user manual](docs/user-manual.md).

## Release Assets

Tagged releases are built by GitHub Actions from tags like `v0.2.0`.

| Asset | Purpose |
|---|---|
| `httpssh-relay-windows-amd64-v0.2.0.exe` | Windows x64 relay binary |
| `httpssh-mobile-android-v0.2.0.apk` | Signed Android APK |
| `*.sha256` | SHA-256 checksum for each binary |

Maintainer release instructions are in [docs/release.md](docs/release.md).

## Documentation

Start with [docs/README.md](docs/README.md).

High-value documents:

- [User manual](docs/user-manual.md)
- [Cloudflare setup](docs/cloudflare-setup.md)
- [Cloudflare operations runbook](docs/cloudflare-operations.md)
- [Architecture](docs/architecture.md)
- [API contracts](docs/api-contracts.md)
- [Wire protocol](docs/protocol.md)
- [Release guide](docs/release.md)
- [Development guide](docs/development.md)
- [Security policy](SECURITY.md)

## License

MIT. See [LICENSE](LICENSE).

## Build Locally

Relay:

```powershell
cd relay
go test ./...
go build -trimpath -o dist/httpssh-relay.exe ./cmd/httpssh-relay
```

Mobile:

```powershell
cd mobile
flutter pub get
flutter analyze
flutter test
flutter build apk --release
```

See [docs/development.md](docs/development.md) for full local workflow details.

## Status

The project is pre-1.0 and intended for a single trusted operator. Sessions are in-memory only; restarting the relay kills active shells. There is no telemetry, analytics SDK, or third-party crash reporting.
