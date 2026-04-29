# httpssh mobile

Flutter app for Android and iOS. It connects to one or more `httpssh-relay` instances over LAN HTTP+bearer or over Cloudflare Tunnel + Cloudflare Access.

Default Android application ID: `com.nekoreset.httpssh`.
Default iOS bundle identifier: `com.nekoreset.httpssh`.

For local/private builds, override the application ID in the ignored
`android/local.properties` file with `httpssh.applicationId=<your.app.id>`.

## Prerequisites

- Flutter 3.22 or later.
- Android Studio + Android SDK for Android builds.
- macOS + Xcode for iOS builds.
- A running relay reachable from the device or emulator.

## Setup

```sh
flutter pub get
flutter gen-l10n
```

The Android scaffold is committed under `android/`. Generated localization files under `lib/l10n/generated/` are ignored by git.
The iOS scaffold is committed under `ios/`, but iOS builds still require macOS and Xcode.

## Run

```sh
flutter run -d <device>
```

The first launch shows an empty Profiles screen. Add a profile pointing at the relay, for example `http://192.168.1.20:18822` with the LAN bearer from `config.yaml`.

## Build

```sh
flutter analyze
flutter test
flutter build apk --release
flutter build ios --release --no-codesign
```

Release APK signing and GitHub Release packaging are documented in [`../docs/release.md`](../docs/release.md).

## Layout

```text
lib/
  main.dart                  Entrypoint
  app.dart                   MaterialApp, theme, and locale wiring
  api/                       REST client and WebSocket URL builder
  auth/                      Secure storage wrapper
  l10n/                      ARB files
  models/                    Profile and session models
  screens/                   Profiles, sessions, settings, and terminal UI
  state/                     Riverpod state
  terminal/                  xterm.dart session and resize logic
test/
  terminal/                  Terminal behavior tests
```
