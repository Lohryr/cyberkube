package events

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nhooyr.io/websocket"
)

func TestServeWSDeliversBroadcastEvents(t *testing.T) {
	hub := NewHub()
	h := &Handler{Hub: hub}

	srv := httptest.NewServer(http.HandlerFunc(h.ServeWS))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Wait for the server to register the client before broadcasting,
	// otherwise the event could be sent before ServeWS calls Register.
	deadline := time.Now().Add(2 * time.Second)
	for {
		hub.mu.Lock()
		n := len(hub.clients)
		hub.mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("server never registered the client")
		}
		time.Sleep(time.Millisecond)
	}

	data, err := marshal(TypeScoreboardUpdated, nil)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	hub.BroadcastRaw(data)

	_, msg, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	var ev Event
	if err := json.Unmarshal(msg, &ev); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if ev.Type != TypeScoreboardUpdated {
		t.Errorf("type = %q, want %q", ev.Type, TypeScoreboardUpdated)
	}
}
