# Release Guide

GitHub Releases are produced by `.github/workflows/release.yml`.

The workflow builds:

- `httpssh-relay-windows-amd64-<tag>.exe`
- `httpssh-mobile-android-<tag>.apk`
- one `.sha256` file per artifact

Release tags must look like `v0.1.0` or `v0.1.0-beta.1`.

The default Android application ID and iOS bundle identifier are both `com.example.httpssh` (placeholder for the OSS source). For store releases override these to your own ID — see `mobile/README.md` for the local override mechanism (`android/local.properties` and `ios/Flutter/Local.xcconfig`, both gitignored).

## Optional GitHub Secrets For Android Signing

For store-grade Android Release APKs, sign with a stable release key. Configure these repository secrets before publishing a tag:

| Secret | Purpose |
|---|---|
| `ANDROID_KEYSTORE_BASE64` | Base64-encoded `upload-keystore.jks` |
| `ANDROID_KEYSTORE_PASSWORD` | Keystore password |
| `ANDROID_KEY_ALIAS` | Key alias inside the keystore |
| `ANDROID_KEY_PASSWORD` | Key password |

If any of these secrets are missing, the release workflow logs a warning and falls back to Android debug signing so the APK still builds. Debug-signed builds are fine for OSS distribution and sideloading, but cannot be upgraded by an APK signed with a different key, and they are rejected by Google Play. Configure all four secrets before publishing builds intended for store upload or for upgrading a previously stably-signed release.

## Create An Android Release Key

Run this once on a trusted machine and store the keystore in a password manager or secure backup.

```powershell
keytool -genkeypair `
  -v `
  -keystore upload-keystore.jks `
  -storetype JKS `
  -keyalg RSA `
  -keysize 2048 `
  -validity 10000 `
  -alias upload
```

Convert it to a GitHub secret value:

```powershell
[Convert]::ToBase64String([IO.File]::ReadAllBytes(".\upload-keystore.jks")) | Set-Clipboard
```

Paste the clipboard value into `ANDROID_KEYSTORE_BASE64`.

## Local Android Signing

For local signed builds, create `mobile/android/key.properties`:

```properties
storeFile=upload-keystore.jks
storePassword=<password>
keyAlias=upload
keyPassword=<password>
```

Place `upload-keystore.jks` at `mobile/android/app/upload-keystore.jks`.

`mobile/android/key.properties` and keystore files are ignored by git.

## Publish A Release

1. Update versions:
   - `mobile/pubspec.yaml` `version:`
   - `mobile/lib/screens/settings_screen.dart` About version, if needed
   - release notes, if you maintain a changelog later
2. Validate locally:

   ```powershell
   cd relay
   go test ./...
   go build -trimpath -o dist/httpssh-relay.exe ./cmd/httpssh-relay

   cd ..\mobile
   flutter pub get
   flutter gen-l10n
   flutter analyze
   flutter test
   flutter build apk --release
   ```

3. Commit the version changes.
4. Create and push a tag:

   ```powershell
   git tag v0.1.0
   git push origin v0.1.0
   ```

5. Wait for the `Release` workflow to finish.
6. Confirm the GitHub Release contains:
   - `httpssh-relay-windows-amd64-v0.1.0.exe`
   - `httpssh-relay-windows-amd64-v0.1.0.exe.sha256`
   - `httpssh-mobile-android-v0.1.0.apk`
   - `httpssh-mobile-android-v0.1.0.apk.sha256`

## Manual Re-run

Use the workflow's **Run workflow** button and provide an existing tag such as `v0.1.0`. The workflow checks out that tag before building.

## Verify Downloaded Assets

On Windows:

```powershell
Get-FileHash .\httpssh-relay-windows-amd64-v0.1.0.exe -Algorithm SHA256
Get-Content .\httpssh-relay-windows-amd64-v0.1.0.exe.sha256
```

The hashes should match.

## Release Workflow Notes

- The relay version is embedded with Go linker flags from the tag without the leading `v`.
- Local relay builds normally use `relay/dist/httpssh-relay.exe`; the release workflow renames the uploaded relay asset to `httpssh-relay-windows-amd64-<tag>.exe`.
- The APK filename is normalized by the workflow after `flutter build apk --release`.
- The APK build number uses the GitHub Actions run number.
- The workflow pins Flutter 3.24.5 to keep dependency resolution aligned with `pubspec.lock`.
- The release workflow does not use the `actions/setup-java` Gradle cache because Gradle lock files can make post-job cache archiving unreliable on Windows runners.
- The workflow uses GitHub's built-in `GITHUB_TOKEN` through `gh release`.
- iOS is not published by this workflow because iOS distribution requires Apple signing, provisioning, and a distribution channel such as TestFlight.
