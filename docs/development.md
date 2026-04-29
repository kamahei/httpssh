# Development Guide

## Repository Layout

```text
relay/   Go Windows relay and embedded web test client assets
mobile/  Flutter mobile app
docs/    Product, protocol, operator, and maintainer documentation
```

## Relay

Requirements:

- Go 1.22 or later.
- Windows 10 1809 or later for real ConPTY runtime validation.

Common commands:

```powershell
cd relay
go mod tidy
go test ./...
go test -race ./...
go build -trimpath -o dist/httpssh-relay.exe ./cmd/httpssh-relay
```

With Task, `task build` writes `relay/httpssh-relay.exe` for the current
OS/architecture and `task build-windows` writes `relay/dist/httpssh-relay.exe`
for Windows x64. CI and GitHub Releases may use architecture/tag-qualified
filenames for uploaded artifacts, but the local Task output is the shorter
`dist/httpssh-relay.exe`.

Run locally:

```powershell
.\dist\httpssh-relay.exe --listen 127.0.0.1:18822 --bearer "dev-bearer-32-chars-or-more-12345" --log-level debug
```

Smoke test:

```powershell
pwsh -File relay/scripts/smoke.ps1
```

## Mobile

Requirements:

- Flutter 3.24.5. CI pins this version so `pubspec.lock` and the
  Flutter SDK-provided `flutter_localizations` dependency resolve
  reproducibly.
- Android Studio and Android SDK for APK builds.
- macOS and Xcode for iOS builds.

Common commands:

```powershell
cd mobile
flutter pub get
flutter gen-l10n
flutter analyze
flutter test
flutter build apk --release
```

The committed Android scaffold is under `mobile/android/`. Generated Flutter localization output under `mobile/lib/l10n/generated/` is ignored by git.

## CI

`.github/workflows/ci.yml` runs on pushes to `main` and pull requests:

- Relay job on `windows-latest`: `go test ./...` and a Windows relay build.
- Mobile job on `ubuntu-latest`: `flutter pub get`, `flutter analyze`, and `flutter test`.

Release packaging is handled separately by `.github/workflows/release.yml`.

## Documentation Rules

- Keep in-repo artifacts in English.
- Update docs in the same change when behavior, API shape, protocol frames, auth behavior, release packaging, or setup steps change.
- Prefer [docs/README.md](README.md) as the documentation index.
