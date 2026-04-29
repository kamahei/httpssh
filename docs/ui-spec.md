# UI Spec

This document covers the two GUI surfaces: the Flutter mobile app and the embedded web test client. The relay has no end-user UI.

## Mobile App (Flutter)

### Localization

- Languages: English (`en`) and Japanese (`ja`).
- Implementation: `flutter_localizations` + `intl` with ARB files at `mobile/lib/l10n/app_en.arb` and `mobile/lib/l10n/app_ja.arb`.
- Default locale: device locale; if unsupported, fall back to English.
- Manual override: settings screen with three options: `System default`, `English`, `Japanese`.
- Rule: every user-visible string is added to both ARB files in the same change. No hardcoded literals in widgets.

### Screens

#### 1. Profiles Screen (entry point)

- AppBar title: `httpssh` (not localized; product name).
- Body: list of `Profile` cards. Each card shows name, base URL, and a small badge for auth mode (`LAN` / `CF Token` / `CF Browser`).
- Empty state: illustration + localized i18n message for adding the first connection profile.
- Floating action button: `+` opens a profile editor modal.
- Tap a profile → push **Sessions Screen**.
- Long-press a profile → action sheet: Edit / Duplicate / Delete.

Profile editor modal fields:
- Name (text).
- Base URL (text, validated as `http(s)://...`).
- Auth mode (segmented: `LAN bearer` / `Cloudflare Service Token` / `Cloudflare browser SSO`).
- Conditional fields:
  - LAN: Bearer (password input).
  - Service Token: Client ID + Client Secret (password inputs).
  - Browser SSO: no extra fields; the app opens an in-app browser session and captures the Cloudflare Access cookie from the webview cookie jar.
- Save button (disabled until valid).

#### 2. Sessions Screen

- AppBar title: profile name; trailing icon = `+ New session`.
- Body: list of live sessions (from `GET /api/sessions`). Each row shows title, dimensions, localized last I/O relative time, and subscriber count.
- Empty state: localized i18n message indicating there are no sessions and the user can tap `+` to create one.
- Pull-to-refresh.
- Tap a row → push **Terminal Screen** with that session attached.
- Swipe a row left → action: Rename / Kill.
- Tap `+ New session` → bottom sheet to pick shell (`pwsh` / `powershell` / `cmd`) and dimensions; on confirm, calls `POST /api/sessions` and pushes **Terminal Screen**.

#### 3. Terminal Screen

Layout from top to bottom:
1. AppBar: tab strip (one tab per attached session) + `+` button to add a tab + overflow menu (close current tab, rename, fullscreen).
2. Terminal widget (xterm.dart) filling the remaining space.
3. Soft keyboard helper bar (above the system keyboard): `Tab` / `Esc` / `Ctrl` (sticky modifier) / `↑` / `↓` / `←` / `→` / `Ctrl+C` / `Ctrl+L`.

Behaviors:
- Each tab maintains its own `xterm Terminal` instance and WebSocket.
- Background tabs continue to receive output and store it in their xterm buffer (no separate ring buffer needed on the client; xterm has its own scrollback).
- Status indicator on tab title: dot color = `green` (live), `amber` (reconnecting), `red` (closed).
- On WebSocket close, the app shows an inline banner: "Reconnecting..." with a `Cancel` action. Auto-retries with exponential backoff capped at 30 s.
- On terminal resize (rotation, keyboard show/hide), the app sends
  `{"t":"resize","c":N,"r":M}` and continues. In wrap mode, PowerShell
  sessions keep the remote column count at least 120 while xterm.dart wraps
  locally, because PowerShell can truncate formatted output at the console
  width.
- Long-press the terminal → "Copy selection" / "Paste from clipboard".

### Theming

- Material 3.
- Light and dark themes; default follows system.
- Terminal background and palette: tweakable in settings (Solarized Dark default; xterm `defaultTheme` allowed alternatives).

### Settings Screen (drawer or gear in Profiles AppBar)

- Language: System / English / Japanese.
- Theme: System / Light / Dark.
- Terminal palette.
- About: app version, source link, license.

### Translation Keys (illustrative subset)

```
profilesEmptyTitle
profilesEmptyDescription
profileAddTooltip
profileEditTitle
profileFieldName
profileFieldBaseUrl
profileFieldAuthMode
profileAuthBearerOnly
profileAuthBearerPlusServiceToken
profileAuthBearerPlusBrowserSso
profileAuthBearerOnlyHint
profileAuthBearerPlusServiceTokenHint
profileAuthBearerPlusBrowserSsoHint
profileAuthBrowserSsoNote
profileFieldLanBearer
profileFieldCfClientId
profileFieldCfClientSecret
sessionsEmptyTitle
sessionsCreateTitle
sessionShellPwsh
sessionShellPowerShell
sessionShellCmd
terminalReconnecting
terminalCancel
terminalCloseTab
settingsLanguage
settingsLanguageSystem
settingsLanguageEnglish
settingsLanguageJapanese
settingsTheme
settingsAbout
errorAuthFailed
errorNotFound
errorServerUnavailable
```

## Web Test Client

### Purpose

A built-in single-page app for:
- Verifying the relay during development.
- Exercising the Cloudflare Access browser/Google OAuth flow without installing the mobile app.
- Operator admin tasks: list/kill stuck sessions, rename, watch.

### Localization

English-only. This is a developer/admin tool, and the user accepted that the mobile app is the primary localized surface.

### Layout (single page, three columns at desktop, single column at mobile)

```
┌────────────────────────────────────────────────────────────┐
│ httpssh — pwsh.example.com           [+ New]  [Settings]   │
├──────────┬─────────────────────────────────────────────────┤
│ Sessions │ Tabs: [pwsh 14:01] [logs 14:12] [+]             │
│  • 14:01 │ ┌─────────────────────────────────────────────┐ │
│  • 14:12 │ │                                             │ │
│  • idle  │ │            xterm.js terminal                │ │
│          │ │                                             │ │
│          │ │                                             │ │
│          │ └─────────────────────────────────────────────┘ │
└──────────┴─────────────────────────────────────────────────┘
```

### Behaviors

- On load: poll `GET /api/sessions` immediately. If the bearer is missing or wrong, show a status banner; the user opens Settings and stores the LAN bearer in localStorage as `httpssh.lanBearer`.
- Sessions panel: refreshes every 5 s; click to open in a new tab.
- New button creates a `pwsh` session via `POST /api/sessions`, then opens the WS.
- Tabs use `@xterm/addon-fit` to size the terminal to the container.
- Settings modal: LAN bearer and optional Cloudflare Service Token override for developer/testing mode.
- Disconnect handling: same banner as the mobile app; auto-reconnect with backoff.

### Build

- Source: `relay/internal/server/webfs/index.html`, `relay/internal/server/webfs/app.js`, and `relay/internal/server/webfs/style.css`.
- Build: the Go binary embeds the `webfs/` directory via `go:embed all:webfs`.
- Dev workflow: edit the embedded assets and restart or rebuild the relay.

### Accessibility

- Keyboard navigation throughout.
- xterm.js is used for terminal rendering; no screen-reader-specific setting is currently exposed.
- Sufficient color contrast in both light and dark themes.
