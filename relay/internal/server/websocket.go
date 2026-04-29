package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"httpssh/relay/internal/session"
)

const (
	wsSubprotocol  = "httpssh.v1"
	wsReadLimit    = 1 << 20 // 1 MiB
	wsServerPing   = 90 * time.Second
	wsServerPongTO = 30 * time.Second
)

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess, err := s.mgr.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}

	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols: []string{wsSubprotocol},
	})
	if err != nil {
		s.logger.Warn("websocket upgrade failed", "err", err)
		return
	}
	if c.Subprotocol() != wsSubprotocol {
		_ = c.Close(websocket.StatusPolicyViolation, "subprotocol must be "+wsSubprotocol)
		return
	}
	c.SetReadLimit(wsReadLimit)

	ctx := r.Context()
	defer func() {
		_ = c.CloseNow()
	}()

	subCh, subDone, unsubscribe := sess.Subscribe(ctx)
	defer unsubscribe()

	// Writer goroutine: drain subCh into the WS.
	writeErr := make(chan error, 1)
	go func() {
		writeErr <- pumpWrite(ctx, c, subCh, subDone)
	}()

	// Reader loop runs on this goroutine.
	readErr := pumpRead(ctx, c, sess)

	// First-error wins; report through the close code.
	closeCode := websocket.StatusNormalClosure
	closeReason := ""
	select {
	case err := <-writeErr:
		if isFatalWS(err) {
			closeCode = websocket.StatusInternalError
			closeReason = err.Error()
		}
	default:
	}
	if isFatalWS(readErr) {
		closeCode = websocket.StatusInternalError
		closeReason = readErr.Error()
	}
	_ = c.Close(closeCode, closeReason)
}

func pumpRead(ctx context.Context, c *websocket.Conn, sess *session.Session) error {
	for {
		var f session.ClientFrame
		if err := wsjson.Read(ctx, c, &f); err != nil {
			return err
		}
		switch f.T {
		case session.FrameIn:
			if err := sess.WriteInput([]byte(f.D)); err != nil {
				return err
			}
		case session.FrameResize:
			if err := sess.Resize(f.C, f.R); err != nil {
				_ = wsjson.Write(ctx, c, session.ServerFrame{
					T:       session.FrameError,
					Message: err.Error(),
				})
			}
		case session.FramePing:
			_ = wsjson.Write(ctx, c, session.ServerFrame{T: session.FramePong})
		default:
			// Unknown types are ignored; clients may use them as forward-compat hooks.
		}
	}
}

func pumpWrite(ctx context.Context, c *websocket.Conn, in <-chan session.ServerFrame, subDone <-chan struct{}) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-subDone:
			return nil
		case frame := <-in:
			writeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			err := wsjson.Write(writeCtx, c, frame)
			cancel()
			if err != nil {
				return err
			}
		}
	}
}

// isFatalWS returns true for errors worth surfacing as an internal-error
// close code. Normal client-initiated closes and context cancellations are
// not "fatal" in that sense.
func isFatalWS(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if websocket.CloseStatus(err) == websocket.StatusNormalClosure {
		return false
	}
	if websocket.CloseStatus(err) == websocket.StatusGoingAway {
		return false
	}
	return true
}
