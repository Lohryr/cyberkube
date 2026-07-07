package events

import "testing"

func TestHubBroadcastDeliversToRegisteredClients(t *testing.T) {
	h := NewHub()
	c1 := h.Register()
	c2 := h.Register()
	defer h.Unregister(c1)
	defer h.Unregister(c2)

	h.BroadcastRaw([]byte("hello"))

	for i, c := range []*Client{c1, c2} {
		select {
		case msg := <-c.send:
			if string(msg) != "hello" {
				t.Errorf("client %d got %q, want hello", i, msg)
			}
		default:
			t.Errorf("client %d received nothing", i)
		}
	}
}

func TestHubUnregisterStopsDelivery(t *testing.T) {
	h := NewHub()
	c := h.Register()
	h.Unregister(c)

	// Broadcasting after unregister must not panic (send channel closed).
	h.BroadcastRaw([]byte("late"))

	if _, ok := <-c.send; ok {
		t.Error("expected closed channel after Unregister")
	}
}

func TestHubUnregisterIsIdempotent(t *testing.T) {
	h := NewHub()
	c := h.Register()
	h.Unregister(c)
	h.Unregister(c) // must not panic (double close)
}

func TestHubSlowClientDoesNotBlockBroadcast(t *testing.T) {
	h := NewHub()
	slow := h.Register()
	defer h.Unregister(slow)

	// Fill the slow client's buffer, then broadcast once more: it must be
	// dropped, not block.
	for i := 0; i < cap(slow.send)+5; i++ {
		h.BroadcastRaw([]byte("x"))
	}
}
