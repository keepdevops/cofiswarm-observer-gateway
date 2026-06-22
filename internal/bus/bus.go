// Package bus connects the gateway to the cofiswarm ZMQ carrier: it SUBs to the bridge's
// egress wire (:5557) for live events and PUBs commands onto the bridge's ingress wire
// (:5556). The gateway is therefore an ordinary bus participant — a subscriber for the
// browser's read side and a publisher for its command side — exactly the diagram's
// "ZMQ PUB/SUB" leg between the WebSocket server and the backend swarm.
package bus

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/go-zeromq/zmq4"
)

// Bus owns the egress SUB and ingress PUB sockets.
type Bus struct {
	sub zmq4.Socket
	pub zmq4.Socket // nil when no command/ingress wire is configured

	ctx    context.Context
	cancel context.CancelFunc

	pubMu sync.Mutex // serializes ingress Send (zmq4 sockets are not concurrency-safe)
}

// New dials the egress SUB at egressAddr (filtered to filter, "" = all). If ingressAddr is
// non-empty it also dials a command PUB there. Returns an error if the egress dial fails.
func New(egressAddr, ingressAddr, filter string) (*Bus, error) {
	ctx, cancel := context.WithCancel(context.Background())
	sub := zmq4.NewSub(ctx)
	if err := sub.Dial(egressAddr); err != nil {
		cancel()
		return nil, fmt.Errorf("dial egress %s: %w", egressAddr, err)
	}
	if err := sub.SetOption(zmq4.OptionSubscribe, filter); err != nil {
		cancel()
		_ = sub.Close()
		return nil, fmt.Errorf("subscribe %q: %w", filter, err)
	}
	b := &Bus{sub: sub, ctx: ctx, cancel: cancel}
	if ingressAddr != "" {
		pub := zmq4.NewPub(ctx)
		if err := pub.Dial(ingressAddr); err != nil {
			cancel()
			_ = sub.Close()
			return nil, fmt.Errorf("dial ingress %s: %w", ingressAddr, err)
		}
		b.pub = pub
	}
	return b, nil
}

// Consume reads egress frames until ctx is cancelled, handing each decoded {topic, payload}
// to onEvent. Returns the Recv error so the caller can reconnect with backoff.
func (b *Bus) Consume(ctx context.Context, onEvent func(topic string, payload map[string]any)) error {
	for ctx.Err() == nil {
		msg, err := b.sub.Recv()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if len(msg.Frames) < 2 {
			log.Printf("bus: dropping malformed egress message (%d frames)", len(msg.Frames))
			continue
		}
		topic := string(msg.Frames[0])
		var payload map[string]any
		if err := json.Unmarshal(msg.Frames[1], &payload); err != nil {
			log.Printf("bus: egress %s: bad json: %v", topic, err)
			continue
		}
		onEvent(topic, payload)
	}
	return nil
}

// Publish emits a command frame [topic, json-payload] on the ingress PUB. It errors loudly
// when no ingress wire is configured (read-only gateway) so a command is never lost silently.
func (b *Bus) Publish(topic string, payload map[string]any) error {
	if b.pub == nil {
		return fmt.Errorf("no ingress wire configured; cannot publish %s", topic)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", topic, err)
	}
	b.pubMu.Lock()
	defer b.pubMu.Unlock()
	if err := b.pub.Send(zmq4.NewMsgFrom([]byte(topic), data)); err != nil {
		return fmt.Errorf("send %s: %w", topic, err)
	}
	return nil
}

// CanPublish reports whether the command/ingress wire is wired (for /healthz and command rejection).
func (b *Bus) CanPublish() bool { return b.pub != nil }

func (b *Bus) Close() error {
	b.cancel()
	err := b.sub.Close()
	if b.pub != nil {
		if perr := b.pub.Close(); err == nil {
			err = perr
		}
	}
	return err
}
