# AGENTS.md

## Project Purpose

`httpssh` is a Windows PowerShell relay that exposes a pseudo-console over HTTP/WebSocket so that Flutter mobile clients (and a built-in web test client) can drive PowerShell through Cloudflare Tunnel + Cloudflare Access from anywhere, or directly over LAN with a shared bearer token. The relay is a Go binary running as a Windows service on Windows x64. Sessions are persisted server-side across client disconnects via an in-memory scrollback ring buffer.

## Source Of Truth

When the following files overlap, follow this order:

1. The current user request
2. `docs/product-spec.md`
3. `docs/architecture.md`
4. `docs/data-model.md`
5. `docs/protocol.md`
6. `docs/api-contracts.md`
7. `docs/user-manual.md`
8. `docs/release.md`
9. `docs/development.md`
10. `docs/implementation-plan.md` (historical roadmap)
11. `docs/task-breakdown.md` (historical task map)
12. `README.md`

If two files conflict, follow the higher-priority source and call out the mismatch in the change.

## Language Policy (Non-Negotiable)

- All in-repo artifacts MUST be in English: documentation, source identifiers, string literals, code comments, log messages, commit messages, CHANGELOG.
- The Flutter mobile app MUST be bilingual (English + Japanese) via `flutter_localizations` + `intl` with ARB files at `mobile/lib/l10n/app_en.arb` and `mobile/lib/l10n/app_ja.arb`. Default to device locale; allow manual override in settings.
- Never hardcode user-visible strings in widgets. All user-facing text must go through the i18n layer, with both `en` and `ja` translations added in the same change.
- The relay has no end-user UI surface; its logs and error responses stay English-only.
- The web test client UI is English-only (developer/admin tool).

## Default Workflow

- Read the relevant project docs before editing code.
- For feature implementation, prefer the smallest current slice implied by the request and the product/architecture docs. `docs/task-breakdown.md` is historical context, not a mandatory task queue.
- Preserve the declared architecture, schema, and wire protocol unless the user asks for a redesign.
- If a change requires updates to architecture, schema, or protocol, update the corresponding doc in the same task.
- After modifying code, run the smallest validation that proves the change works (unit test, `go test ./...`, `flutter test`, manual `websocat` smoke).

## Boundaries

- Keep responsibilities separated according to `docs/architecture.md`. ConPTY/PTY code lives in `relay/internal/conpty`; session/scrollback in `relay/internal/session`; HTTP/WS handlers in `relay/internal/server`; auth in `relay/internal/auth`.
- Domain rules and entity invariants are defined in `docs/data-model.md`.
- Do not introduce new third-party services, infra, dependencies, or runtimes without justification and a doc update.
- Do not weaken auth: every request must require the LAN bearer. The relay does not inspect any `Cf-*` header; Cloudflare Access is an outer edge layer for identity, not a relay-level credential.
- Do not silently widen scope beyond the requested task slice.

## Validation

- Go relay: `go test ./...` in `relay/`. Add table-driven tests for the session manager and ring buffer. ConPTY runtime tests must run on Windows.
- Flutter app: `flutter analyze` and `flutter test` in `mobile/`. Widget tests for the terminal screen.
- Web client: manual smoke against a local relay. If a TypeScript build pipeline is reintroduced later, type-check it with `tsc --noEmit`.
- E2E: launch relay locally, connect via the web client, send `Get-Date` and assert output.
- If a validation step cannot be run in the current environment (e.g. ConPTY tests on non-Windows CI), state what was skipped and why.

## When To Ask Questions

Ask a short question before proceeding only if the missing answer would materially change one of these:

- runtime/deployment model (e.g. moving relay off Windows)
- storage or session-persistence model (e.g. introducing a database)
- authentication or authorization behavior (e.g. removing LAN bearer)
- public REST or WebSocket protocol shape
- Cloudflare Access policy structure

Otherwise, proceed with the documented assumption and note it in the change.

## Output Rules

- All file edits in this repo MUST be in English (see Language Policy).
- Chat replies to the user remain in Japanese.
- Keep explanations concise and tied to specific files and line ranges.
- Call out assumptions, validation status, and tradeoffs explicitly.
