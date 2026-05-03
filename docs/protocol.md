# Wire Protocol (`httpssh.v1`)

The WebSocket endpoint at `/api/sessions/{id}/io` carries terminal I/O between client and relay. Frames are JSON text frames (UTF-8). All frames have a `t` (type) field. Unknown client frame types are ignored by the relay for forward compatibility.

## Subprotocol Negotiation

Client requests `Sec-WebSocket-Protocol: httpssh.v1`. Relay must echo `httpssh.v1` in the upgrade response. If the client does not request this subprotocol, the current relay accepts the upgrade without a negotiated subprotocol and then closes it with policy violation code `1008`.

## Frame Types

### Server → Client

#### `replay`

Sent exactly once, immediately after the WebSocket upgrade succeeds, before any `out` frames.

```json
{ "t": "replay", "d": "<utf-8 string of recent scrollback>" }
```

`d` contains the latest scrollback bytes (default ≤ 4 MiB) decoded as UTF-8 with replacement for invalid byte sequences. The client should write `d` to its terminal before processing further frames.

#### `out`

Live output from the PTY.

```json
{ "t": "out", "d": "<utf-8 chunk>" }
```

The relay currently sends one `out` frame per PTY read, with a 32 KiB read buffer. It does not coalesce output frames.

#### `exit`

Sent when the underlying shell process exits. After this frame the relay closes the WebSocket with code `1000`.

```json
{ "t": "exit", "code": 0 }
```

#### `pong`

Reply to a client `ping`.

```json
{ "t": "pong" }
```

#### `error`

Out-of-band error notification (does not necessarily close the WS).

```json
{ "t": "error", "message": "<human>" }
```

The current relay sends this for rejected `resize` requests. It does not include a machine-readable error code in the frame.

### Client → Server

#### `in`

Keystrokes / pasted data. The relay writes `d` to the PTY verbatim.

```json
{ "t": "in", "d": "ls -la\r" }
```

The client is responsible for any local-echo policy: ConPTY-driven shells generally echo themselves. The mobile and web clients do NOT do local echo.

#### `resize`

Inform the relay of new terminal dimensions.

```json
{ "t": "resize", "c": 120, "r": 40 }
```

Validation: `1 ≤ c ≤ 500`, `1 ≤ r ≤ 200`. Out-of-range requests are rejected with an `error` frame and ignored.

ConPTY may emit a full-screen repaint as a side effect of a resize. The relay suppresses those repaint bursts from both live `out` delivery and replay scrollback because they redraw already-visible screen contents rather than new shell output.

#### `ping`

Heartbeat. The relay replies with `pong`. Clients should send `ping` every 20 seconds while idle to keep CDN intermediaries from closing the WebSocket.

```json
{ "t": "ping" }
```

## Sequencing Rules

1. The first frame after upgrade is always one server `replay`.
2. The relay never sends `out` before `replay`.
3. The relay never re-sends `replay` on the same WebSocket; reconnection requires a fresh WebSocket.
4. The relay sends PTY output as non-empty `out` frames as reads arrive.
5. The client must process `replay` before any user input is sent.

## Multi-Subscriber Semantics

- Multiple WebSockets may attach to the same session simultaneously (e.g., the same user on phone and laptop).
- All subscribers receive the same `out` stream after their own `replay`.
- Inputs from any subscriber are merged into a single PTY input stream in the order received.
- If one subscriber's output buffer fills (default 256 frames pending), the relay cancels that subscriber and continues serving the others. The current implementation does not use a custom close code for this path.

## Disconnect and Replay

When a WebSocket closes (any reason), the session continues running server-side. On the next attach:

- Relay sends a fresh `replay` containing the latest ring-buffer contents (which may include output produced after the old WS closed).
- The client should clear its terminal and write the replay payload, since there is no message-level "since cursor" mechanism in v1. ANSI clear-screen sequences in the replay payload normally produce a sane re-render.

## Heartbeat and Timeouts

- The relay replies to client `ping` frames with `pong`.
- The first-party mobile and web clients send `ping` every 20 seconds while the WebSocket is open.
- The current relay does not run a separate server-side WebSocket ping timeout. Client-side ping is used to keep Cloudflare Tunnel and intermediate network devices from closing idle WebSockets.

## Example Session

```text
C → S    GET /api/sessions/4f3c2a1d9e8b7c6a554433221100ffee/io  Upgrade: websocket  Sec-WebSocket-Protocol: httpssh.v1
S → C    101 Switching Protocols  Sec-WebSocket-Protocol: httpssh.v1
S → C    {"t":"replay","d":"PS C:\\Users\\Owner> "}
C → S    {"t":"resize","c":120,"r":40}
C → S    {"t":"in","d":"Get-Date\r"}
S → C    {"t":"out","d":"\r\n\r\nMonday, April 29, 2026 ..."}
C → S    {"t":"ping"}
S → C    {"t":"pong"}
... time passes ... C disconnects
... S keeps PTY alive, scrollback continues to fill ...
C → S    GET /api/sessions/4f3c2a1d9e8b7c6a554433221100ffee/io  Upgrade: websocket  ...
S → C    {"t":"replay","d":"<recent buffer including missed output>"}
```

## In-Band Signals Consumed by the Relay

The relay also reads a single in-band escape sequence out of the PTY's
output stream as it pumps frames to subscribers — it does not strip the
sequence; clients see it as part of the `out` payload as usual.

- **OSC 9;9 — current working directory.** Form: `ESC ] 9 ; 9 ; <path>
  BEL` (or `ESC ] 9 ; 9 ; <path> ESC \`). When the relay spawns a
  shell, it injects a small bootstrap (`-EncodedCommand` for
  pwsh/powershell, `prompt $E]9;9;$P$E\$P$G` for cmd) that wraps the
  user's prompt with this emitter. The relay parses the sequence with
  a byte-streaming state machine that survives splits across PTY
  reads, then updates `Session.cwd`. The current CWD is exposed via
  `GET /api/sessions` and `GET /api/sessions/{id}` (as the `cwd`
  field) and used by `GET /api/sessions/{id}/files/list` and `read`.
  See `docs/architecture.md`.

  The OSC 9;9 sequence is also recognized by Windows Terminal and
  ConHost for their own "set tab CWD" UX, so injecting it does not
  break those terminals if the operator runs the shell outside the
  relay.

## Future Extensions (Not in v1)

- Binary frames for raw PTY bytes (eliminates UTF-8 overhead for non-text streams).
- `cursor`-style replay with last-acknowledged offset for delta-only resync.
- `attach-readonly` flag for shared-screen scenarios.
