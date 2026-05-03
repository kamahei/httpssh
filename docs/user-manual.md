# User Manual

This manual covers installing and using `httpssh` as a single-operator tool.

## Requirements

- Windows 10 1809 or later, Windows 11 recommended.
- PowerShell 7 (`pwsh.exe`) on `PATH`, or Windows PowerShell as fallback.
- A long LAN bearer token. Generate one with:

  ```powershell
  -join ((1..32) | ForEach-Object { "{0:x2}" -f (Get-Random -Maximum 256) })
  ```

- Optional remote access: a Cloudflare-managed domain, Cloudflare Tunnel, and Cloudflare Access.

## Install The Relay

1. Download `httpssh-relay-windows-amd64-<tag>.exe` from the GitHub Release.
2. Create a directory on the Windows host:

   ```powershell
   New-Item -ItemType Directory -Force C:\Program Files\httpssh
   Copy-Item .\httpssh-relay-windows-amd64-v0.3.0.exe "C:\Program Files\httpssh\httpssh-relay.exe"
   ```

3. Copy `relay/config.example.yaml` to `C:\Program Files\httpssh\config.yaml`.
4. Edit `config.yaml`:

   ```yaml
   listen: "127.0.0.1:18822"
   shell: "auto"
   auth:
     lan_bearer: "<32+ random hex chars>"
   session:
     idle_timeout: "24h"
     scrollback_bytes: 4194304
     reap_interval: "60s"
   files:
     roots:
       - id: "home"
         name: "Home"
         path: "C:\\Users\\Owner"
     max_file_bytes: 1048576
   log:
     level: "info"
   ```

Use `127.0.0.1:18822` when Cloudflare Tunnel is the only remote ingress. Use a LAN address such as `192.168.1.20:18822` only when you want direct LAN access.

## Run In The Foreground

```powershell
cd "C:\Program Files\httpssh"
.\httpssh-relay.exe --config .\config.yaml
```

Health check:

```powershell
curl http://127.0.0.1:18822/api/health -H "Authorization: Bearer <lan_bearer>"
```

## Install As A Windows Service

From an elevated PowerShell in the repository checkout:

```powershell
pwsh -File relay/scripts/install-service.ps1 `
  -Binary "C:\Program Files\httpssh\httpssh-relay.exe" `
  -Config "C:\Program Files\httpssh\config.yaml"

Start-Service httpssh-relay
Get-Service httpssh-relay
```

Uninstall:

```powershell
pwsh -File relay/scripts/uninstall-service.ps1
```

## Install As A Per-User Logon Task

Use a logon task when the relay should inherit the signed-in user's `PATH`,
PowerShell profile, CLI credentials, SSH keys, and other user-scoped tools.
This is useful for Microsoft accounts that cannot be used as a Windows service
account with password logon. The relay starts only after that user logs on.

From a normal Command Prompt or PowerShell in the repository checkout:

```cmd
relay\scripts\install-logon-task.cmd
```

To install from a downloaded release executable instead:

```cmd
relay\scripts\install-logon-task.cmd "%USERPROFILE%\Downloads\httpssh-relay-windows-amd64-v0.3.0.exe"
```

The script copies the relay and config to `%LOCALAPPDATA%\httpssh`, registers a
current-user scheduled task named `httpssh-relay-logon`, starts it immediately,
and uses a hidden launcher so no console window is shown at logon. Logs are
written to `%LOCALAPPDATA%\httpssh\logs\httpssh-relay.log`.

Do not run the Windows service and the logon task at the same time on the same
`listen` address. Stop or uninstall `httpssh-relay` service before using the
logon task.

Uninstall:

```cmd
relay\scripts\uninstall-logon-task.cmd
```

By default this removes the task and stops the relay while keeping the copied
config and logs. Use `relay\scripts\uninstall-logon-task.cmd /remove-files` to
remove `%LOCALAPPDATA%\httpssh` too.

## Use The Web Test Client

Open:

```text
http://127.0.0.1:18822/web/
```

Then:

1. Click **Settings**.
2. Paste the LAN bearer.
3. Click **Save**.
4. Click **+ New** to create a PowerShell session.
5. Run `Get-Date` to verify terminal I/O.

The web client is for development, testing, and browser access through Cloudflare Access. It stores the LAN bearer in browser localStorage on that origin.

## Install The Android App

1. Download `httpssh-mobile-android-<tag>.apk` from the GitHub Release.
2. Copy it to the Android device.
3. Allow installation from the source you use to open the APK.
4. Install the APK.

The Android app name shown on the launcher is `httpssh`; its application ID is `com.nekoreset.httpssh`.

## Add A LAN Profile

1. Open `httpssh`.
2. Tap **+** on the Profiles screen.
3. Set:
   - Name: `Home LAN`
   - Base URL: `http://<windows-host-lan-ip>:18822`
   - Auth mode: LAN bearer
   - LAN bearer: the value from `config.yaml`
4. Save the profile.
5. Tap the profile, create a session, and run `Get-Date`.

## Add A Cloudflare Profile

First complete [Cloudflare setup](cloudflare-setup.md).

For Service Token mode:

1. Base URL: `https://pwsh.example.com`
2. Auth mode: Cloudflare Service Token
3. LAN bearer: the relay bearer from `config.yaml`
4. Cloudflare Client ID and Client Secret: values from the Cloudflare Access Service Token.

For browser SSO mode:

1. Base URL: `https://pwsh.example.com`
2. Auth mode: Cloudflare browser SSO
3. LAN bearer: the relay bearer from `config.yaml`
4. Follow the in-app browser login when prompted.

Cloudflare credentials satisfy the edge layer. The LAN bearer still satisfies the relay layer.

## Session Behavior

- Closing the mobile app or losing a network connection does not kill the shell immediately.
- The relay keeps the session alive and stores recent output in an in-memory ring buffer.
- Reconnecting sends a `replay` frame with recent scrollback, then resumes live output.
- Restarting the relay kills all sessions. Sessions are not persisted to disk.
- In the mobile terminal, tap the keyboard icon in the soft-key bar to open a temporary multiline IME input box. It lets you compose Japanese text safely, then send it with the Send button. The default appends Enter so a single-line command is submitted; turn off **Append Enter** when you only want to insert text.

## Use The Mobile File Browser

The mobile file browser is read-only and only exposes directories listed under `files.roots` in `config.yaml`. After editing roots, restart the relay.

1. Open a profile in the mobile app.
2. Tap the folder icon on the Sessions screen.
3. Pick a configured root.
4. Tap folders to navigate, or use the path button to enter a root-relative path.
5. Tap a text file to open the viewer.
6. Use search, previous/next match, copy all, syntax-highlight toggle, or add the current folder to bookmarks.

The relay rejects paths outside configured roots, binary files, and text files larger than `files.max_file_bytes`.

## Routine Maintenance

- Rotate the LAN bearer by editing `config.yaml`, restarting `httpssh-relay`, and updating every client profile.
- Rotate a Cloudflare Service Token in Cloudflare Zero Trust, then update the mobile profile.
- Update the relay by replacing `httpssh-relay.exe` and restarting the service.
- Update Android by installing the newer Release APK signed with the same release key.

For troubleshooting and rotation details, use the [Cloudflare operations runbook](cloudflare-operations.md).
