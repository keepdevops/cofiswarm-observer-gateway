// Package hub fans bus events out to every connected WebSocket client and is the single
// registry of who is connected. It is transport-agnostic: clients hand it a buffered send
// channel, the hub broadcasts pre-marshaled frames into each, and a slow client is dropped
// (its frame skipped) rather than allowed to stall the bus reader.
package hub

import "sync"

// Client is one connected browser. send carries pre-marshaled JSON frames to that client's
// writer goroutine; a full channel means the client is too slow and the frame is dropped.
type Client struct {
	send chan []byte
}

// Send returns the client's outbound frame channel (read by its writer goroutine).
func (c *Client) Send() <-chan []byte { return c.send }

// Hub tracks connected clients and broadcasts frames to all of them.
type Hub struct {
	mu      sync.RWMutex
	clients map[*Client]struct{}
}

func New() *Hub {
	return &Hub{clients: map[*Client]struct{}{}}
}

// Add registers a new client with a buffered send channel and returns it.
func (h *Hub) Add(buffer int) *Client {
	c := &Client{send: make(chan []byte, buffer)}
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
	return c
}

// Remove deregisters a client and closes its send channel so its writer goroutine exits.
func (h *Hub) Remove(c *Client) {
	h.mu.Lock()
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		close(c.send)
	}
	h.mu.Unlock()
}

// Broadcast delivers frame to every client without blocking: a client whose buffer is full
// skips this frame (returned in dropped) rather than back-pressuring the caller (the bus
// reader). Returns how many clients received it and how many dropped it.
func (h *Hub) Broadcast(frame []byte) (delivered, dropped int) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		select {
		case c.send <- frame:
			delivered++
		default:
			dropped++
		}
	}
	return delivered, dropped
}

// Count returns the number of connected clients (for /healthz and logging).
func (h *Hub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}
