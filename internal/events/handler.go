package events

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"nhooyr.io/websocket"
)

// writeTimeout bounds how long a single frame write may take before the
// connection is considered dead.
const writeTimeout = 10 * time.Second

// Handler serves the authenticated real-time event feed.
type Handler struct {
	Hub *Hub
}

// ServeWS handles GET /api/v1/events. It is a server-push-only feed (no
// client messages are expected): CloseRead lets the websocket library
// service pings/close frames in the background while this goroutine only
// writes.
func (h *Handler) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		slog.Error("websocket accept failed", "err", err)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	ctx := conn.CloseRead(r.Context())
	client := h.Hub.Register()
	defer h.Hub.Unregister(client)

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-client.send:
			if !ok {
				return
			}
			if err := writeMessage(ctx, conn, msg); err != nil {
				return
			}
		}
	}
}

func writeMessage(ctx context.Context, conn *websocket.Conn, msg []byte) error {
	writeCtx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return conn.Write(writeCtx, websocket.MessageText, msg)
}
