package hub

import "testing"

func TestBroadcastDeliversToAll(t *testing.T) {
	h := New()
	a := h.Add(4)
	b := h.Add(4)
	if got := h.Count(); got != 2 {
		t.Fatalf("count = %d, want 2", got)
	}
	delivered, dropped := h.Broadcast([]byte("x"))
	if delivered != 2 || dropped != 0 {
		t.Fatalf("delivered=%d dropped=%d, want 2/0", delivered, dropped)
	}
	if string(<-a.Send()) != "x" || string(<-b.Send()) != "x" {
		t.Fatal("clients did not receive the frame")
	}
}

func TestBroadcastDropsSlowClient(t *testing.T) {
	h := New()
	h.Add(1)                        // registered client, buffer size 1
	_, _ = h.Broadcast([]byte("1")) // fills the buffer
	delivered, dropped := h.Broadcast([]byte("2"))
	if delivered != 0 || dropped != 1 {
		t.Fatalf("delivered=%d dropped=%d, want 0/1 (slow client dropped)", delivered, dropped)
	}
}

func TestRemoveClosesSendAndDeregisters(t *testing.T) {
	h := New()
	c := h.Add(1)
	h.Remove(c)
	if h.Count() != 0 {
		t.Fatalf("count = %d after remove, want 0", h.Count())
	}
	if _, ok := <-c.Send(); ok {
		t.Fatal("send channel should be closed after Remove")
	}
	h.Remove(c) // double-remove must not panic
}
