# Release Guide

GitHub Releases are produced by `.github/workflows/release.yml`.

The workflow builds:

- `httpssh-relay-windows-amd64-<tag>.exe`
- `httpssh-mobile-android-<tag>.apk`
- one `.sha256` file per artifact

Release tags must look like `v0.1.0` or `v0.1.0-beta.1`.

The default Android application ID and iOS bundle identifier are both `com.nekoreset.httpssh`. GitHub Release APKs use this ID unless overridden by the release workflow environment.

## Required GitHub Secrets

The Android Release APK must be signed with a stable release key. Configure these repository secrets before publishing a tag:

| Secret | Purpose |
|---|---|
| `ANDROID_KEYSTORE_BASE64` | Base64-encoded `upload-keystore.jks` |
| `ANDROID_KEYSTORE_PASSWORD` | Keystore password |
| `ANDROID_KEY_ALIAS` | Key alias inside the keystore |
| `ANDROID_KEY_PASSWORD` | Key password |

The release workflow fails early if any signing secret is missing. This prevents publishing an APK signed with a throwaway debug key that cannot be upgraded reliably.

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
   go build -trimpath -o dist/httpssh-relay-windows-amd64.exe ./cmd/httpssh-relay

   cd ..\mobile
   flutter pub get
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
- The APK filename is normalized by the workflow after `flutter build apk --release`.
- The APK build number uses the GitHub Actions run number.
- The workflow uses GitHub's built-in `GITHUB_TOKEN` through `gh release`.
- iOS is not published by this workflow because iOS distribution requires Apple signing, provisioning, and a distribution channel such as TestFlight.
