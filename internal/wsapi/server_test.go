package wsapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/keepdevops/cofiswarm-observer-gateway/internal/hub"
)

// fakePub records published commands and lets a test toggle command capability.
type fakePub struct {
	mu       sync.Mutex
	can      bool
	gotTopic string
	gotPay   map[string]any
}

func (f *fakePub) CanPublish() bool { return f.can }
func (f *fakePub) Publish(topic string, payload map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gotTopic, f.gotPay = topic, payload
	return nil
}

func dial(t *testing.T, srv *httptest.Server) (*websocket.Conn, context.Context) {
	t.Helper()
	ctx := context.Background()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return c, ctx
}

// A valid publish command is republished through the Publisher and acked back to the client.
func TestCommandPublishesAndAcks(t *testing.T) {
	h := hub.New()
	pub := &fakePub{can: true}
	srv := httptest.NewServer(New(h, pub, "swarm.observer.", nil).Handler())
	defer srv.Close()

	c, ctx := dial(t, srv)
	defer c.CloseNow()

	cmd, _ := json.Marshal(map[string]any{
		"type": "publish", "topic": "swarm.observer.hello", "payload": map[string]any{"from": "test"},
	})
	if err := c.Write(ctx, websocket.MessageText, cmd); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := readJSON(t, c, ctx)
	if got["type"] != "ack" || got["detail"] != "swarm.observer.hello" {
		t.Fatalf("reply = %v, want ack for the topic", got)
	}
	pub.mu.Lock()
	defer pub.mu.Unlock()
	if pub.gotTopic != "swarm.observer.hello" || pub.gotPay["from"] != "test" {
		t.Fatalf("publisher saw topic=%q payload=%v", pub.gotTopic, pub.gotPay)
	}
}

// A command outside the allowed prefix is rejected and never reaches the Publisher.
func TestCommandPrefixGuard(t *testing.T) {
	h := hub.New()
	pub := &fakePub{can: true}
	srv := httptest.NewServer(New(h, pub, "swarm.observer.", nil).Handler())
	defer srv.Close()

	c, ctx := dial(t, srv)
	defer c.CloseNow()

	cmd, _ := json.Marshal(map[string]any{"type": "publish", "topic": "swarm.kvpool.evict"})
	if err := c.Write(ctx, websocket.MessageText, cmd); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := readJSON(t, c, ctx)
	if got["type"] != "error" {
		t.Fatalf("reply = %v, want error", got)
	}
	pub.mu.Lock()
	defer pub.mu.Unlock()
	if pub.gotTopic != "" {
		t.Fatalf("guard leaked: publisher saw %q", pub.gotTopic)
	}
}

// An event broadcast to the hub is pushed to the connected client over the socket.
func TestEventPushedToClient(t *testing.T) {
	h := hub.New()
	srv := httptest.NewServer(New(h, &fakePub{can: true}, "swarm.observer.", nil).Handler())
	defer srv.Close()

	c, ctx := dial(t, srv)
	defer c.CloseNow()

	// Give the server a moment to register the client in the hub before broadcasting.
	frame, _ := json.Marshal(map[string]any{"topic": "swarm.slot.erase", "payload": map[string]any{"slot": 3}})
	deadline := time.Now().Add(2 * time.Second)
	for h.Count() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	h.Broadcast(frame)

	got := readJSON(t, c, ctx)
	if got["topic"] != "swarm.slot.erase" {
		t.Fatalf("event = %v, want swarm.slot.erase", got)
	}
}

func readJSON(t *testing.T, c *websocket.Conn, ctx context.Context) map[string]any {
	t.Helper()
	rctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, data, err := c.Read(rctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal %q: %v", data, err)
	}
	return m
}
