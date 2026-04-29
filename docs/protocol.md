# Wire Protocol (`httpssh.v1`)

The WebSocket endpoint at `/api/sessions/{id}/io` carries terminal I/O between client and relay. Frames are JSON text frames (UTF-8). All frames have a `t` (type) field. Unknown types are ignored by the receiver but logged.

## Subprotocol Negotiation

Client requests `Sec-WebSocket-Protocol: httpssh.v1`. Relay must echo `httpssh.v1` in the upgrade response. If the client does not request this subprotocol, the relay closes with code `1002` (protocol error).

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

The relay batches PTY reads with a small coalescing window (≤ 16 ms) to reduce frame count. Per-frame payload is capped at 64 KiB; larger reads are split into multiple frames.

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
{ "t": "error", "code": "<machine_code>", "message": "<human>" }
```

Codes: `pty_write_failed`, `resize_rejected`, `frame_too_large`, `internal`.

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

#### `ping`

Heartbeat. The relay replies with `pong`. Clients should send `ping` every 20 seconds while idle to keep CDN intermediaries from closing the WebSocket.

```json
{ "t": "ping" }
```

## Sequencing Rules

1. The first frame after upgrade is always one server `replay`.
2. The relay never sends `out` before `replay`.
3. The relay never re-sends `replay` on the same WebSocket; reconnection requires a fresh WebSocket.
4. The relay must coalesce at most 16 ms of PTY output into a single `out` frame and never produce empty `out` frames.
5. The client must process `replay` before any user input is sent.

## Multi-Subscriber Semantics

- Multiple WebSockets may attach to the same session simultaneously (e.g., the same user on phone and laptop).
- All subscribers receive the same `out` stream after their own `replay`.
- Inputs from any subscriber are merged into a single PTY input stream in the order received.
- If one subscriber's output buffer fills (default 256 frames pending), the relay closes that subscriber's WS with `4503` and continues serving the others.

## Disconnect and Replay

When a WebSocket closes (any reason), the session continues running server-side. On the next attach:

- Relay sends a fresh `replay` containing the latest ring-buffer contents (which may include output produced after the old WS closed).
- The client should clear its terminal and write the replay payload, since there is no message-level "since cursor" mechanism in v1. ANSI clear-screen sequences in the replay payload normally produce a sane re-render.

## Heartbeat and Timeouts

- Server idle timeout (no inbound frames for 90 s): server sends `ping`; if no `pong` in 30 s, server closes with `1001` (going away).
- Client should send `ping` every 20 s when otherwise idle.
- Cloudflare Tunnel idle WS timeout is 100 s; the 20 s client-side ping keeps it open.

## Example Session

```text
C → S    GET /api/sessions/01HXY.../io  Upgrade: websocket  Sec-WebSocket-Protocol: httpssh.v1
S → C    101 Switching Protocols  Sec-WebSocket-Protocol: httpssh.v1
S → C    {"t":"replay","d":"PS C:\\Users\\Owner> "}
C → S    {"t":"resize","c":120,"r":40}
C → S    {"t":"in","d":"Get-Date\r"}
S → C    {"t":"out","d":"\r\n\r\nMonday, April 29, 2026 ..."}
C → S    {"t":"ping"}
S → C    {"t":"pong"}
... time passes ... C disconnects
... S keeps PTY alive, scrollback continues to fill ...
C → S    GET /api/sessions/01HXY.../io  Upgrade: websocket  ...
S → C    {"t":"replay","d":"<recent buffer including missed output>"}
```

## Future Extensions (Not in v1)

- Binary frames for raw PTY bytes (eliminates UTF-8 overhead for non-text streams).
- `cursor`-style replay with last-acknowledged offset for delta-only resync.
- `attach-readonly` flag for shared-screen scenarios.
