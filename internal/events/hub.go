// Package events implements the real-time event fan-out behind
// WS /api/v1/events: an in-memory hub per pod, fed by PostgreSQL LISTEN/
// NOTIFY so every replica's clients see the same events regardless of which
// pod produced them.
package events

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/CyberKube-ISEN/cyberkube/internal/metrics"
)

// Event types broadcast to WebSocket clients.
const (
	TypeScoreboardUpdated = "scoreboard.updated"
	TypeChallengeSolved   = "challenge.solved"
	TypeInstanceStatus    = "instance.status"
)

// Channel is the PostgreSQL NOTIFY channel used for cross-replica fan-out.
const Channel = "cyberkube_events"

// Event is the JSON envelope delivered to WebSocket clients.
type Event struct {
	Type    string    `json:"type"`
	Payload any       `json:"payload"`
	Ts      time.Time `json:"ts"`
}

// Client is one connected WebSocket subscriber's outbound queue.
type Client struct {
	send chan []byte
}

// Hub fans out already-serialized event frames to every client connected to
// this pod. It never talks to PostgreSQL directly — see Publisher and
// Listen for the cross-replica bridge.
type Hub struct {
	mu      sync.Mutex
	clients map[*Client]struct{}
}

// NewHub creates an empty Hub.
func NewHub() *Hub {
	return &Hub{clients: map[*Client]struct{}{}}
}

// Register adds a new client and returns it. Callers must Unregister it
// exactly once when the connection closes.
func (h *Hub) Register() *Client {
	c := &Client{send: make(chan []byte, 16)}
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
	metrics.ActiveWSClients.Inc()
	return c
}

// Unregister removes a client and closes its send channel. Safe to call
// more than once.
func (h *Hub) Unregister(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		close(c.send)
		metrics.ActiveWSClients.Dec()
	}
}

// BroadcastRaw fans out an already-serialized frame to every connected
// client on this pod. Slow clients that would block are dropped rather than
// stalling the broadcast for everyone else.
func (h *Hub) BroadcastRaw(data []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		select {
		case c.send <- data:
		default:
		}
	}
}

// marshal serializes an Event to its wire format.
func marshal(eventType string, payload any) ([]byte, error) {
	return json.Marshal(Event{Type: eventType, Payload: payload, Ts: time.Now().UTC()})
}
