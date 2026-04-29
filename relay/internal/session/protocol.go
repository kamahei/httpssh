package session

// Frame types for the WebSocket wire protocol described in docs/protocol.md.

const (
	// Server -> client frame types.
	FrameReplay = "replay"
	FrameOut    = "out"
	FrameExit   = "exit"
	FramePong   = "pong"
	FrameError  = "error"

	// Client -> server frame types.
	FrameIn     = "in"
	FrameResize = "resize"
	FramePing   = "ping"
)

// ServerFrame is the JSON shape for frames the relay sends to clients.
// Fields are encoded with omitempty so that minimal frames stay small.
type ServerFrame struct {
	T       string `json:"t"`
	D       string `json:"d,omitempty"`
	Code    *int   `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// ClientFrame is the JSON shape for frames the relay receives from clients.
// Inbound `code`/`message` fields are accepted but ignored.
type ClientFrame struct {
	T string `json:"t"`
	D string `json:"d,omitempty"`
	C uint16 `json:"c,omitempty"`
	R uint16 `json:"r,omitempty"`
}
