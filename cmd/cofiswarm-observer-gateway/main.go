// Command cofiswarm-observer-gateway is the WebSocket gateway from the observer-panel
// diagram: it bridges browser dashboards (persistent full-duplex WebSocket) to the
// cofiswarm ZMQ carrier.
//
//	browser ⇄ ws://:8820/ws ⇄ gateway ⇄ SUB egress :5557 (events) / PUB ingress :5556 (commands)
//
// Live bus events SUB'd from the bridge egress are fanned out to every connected dashboard;
// commands sent by a dashboard are republished onto the bridge ingress (guarded by a topic
// prefix). The egress reader reconnects with capped backoff so a bridge restart is survivable.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/keepdevops/cofiswarm-observer-gateway/internal/bus"
	"github.com/keepdevops/cofiswarm-observer-gateway/internal/hub"
	"github.com/keepdevops/cofiswarm-observer-gateway/internal/wsapi"
)

func main() {
	addr := flag.String("listen", ":8820", "WebSocket/HTTP listen address")
	flag.Parse()

	egress := envOr("COFISWARM_ZMQ_EGRESS_ADDR", "tcp://127.0.0.1:5557") // events in
	ingress := os.Getenv("COFISWARM_ZMQ_ADDR")                           // commands out ("" disables)
	filter := envOr("COFISWARM_ZMQ_FILTER", "swarm.")
	cmdPrefix := envOr("COFISWARM_CMD_PREFIX", "swarm.observer.")
	origins := splitNonEmpty(os.Getenv("COFISWARM_WS_ORIGINS")) // e.g. "localhost:8820,panel.example"

	h := hub.New()

	// Retry the initial bus dial with capped backoff so the gateway survives starting
	// before the bridge is up (rather than crash-looping); the dashboard still serves.
	b := dialBus(egress, ingress, filter)
	defer b.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go consumeForever(ctx, b, h)

	srv := wsapi.New(h, b, cmdPrefix, origins)
	log.Printf("observer-gateway listening on %s (events<-%s, commands->%q, cmd-prefix=%q)",
		*addr, egress, ingress, cmdPrefix)
	log.Fatal(http.ListenAndServe(*addr, srv.Handler()))
}

// dialBus connects to the bridge, retrying with capped backoff so the gateway tolerates a
// bridge that isn't up yet instead of crash-looping. Returns once connected.
func dialBus(egress, ingress, filter string) *bus.Bus {
	backoff := time.Second
	for {
		b, err := bus.New(egress, ingress, filter)
		if err == nil {
			return b
		}
		log.Printf("bus: %v (retry in %s)", err, backoff)
		time.Sleep(backoff)
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

// consumeForever reads egress events into the hub, reconnecting with capped backoff so a
// bridge restart doesn't permanently sever the gateway.
func consumeForever(ctx context.Context, b *bus.Bus, h *hub.Hub) {
	backoff := time.Second
	for ctx.Err() == nil {
		err := b.Consume(ctx, func(topic string, payload map[string]any) {
			frame, mErr := json.Marshal(map[string]any{"topic": topic, "payload": payload})
			if mErr != nil {
				log.Printf("gateway: marshal %s: %v", topic, mErr)
				return
			}
			if _, dropped := h.Broadcast(frame); dropped > 0 {
				log.Printf("gateway: dropped %s for %d slow client(s)", topic, dropped)
			}
		})
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			log.Printf("gateway: egress error: %v (retry in %s)", err, backoff)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func splitNonEmpty(csv string) []string {
	var out []string
	for _, p := range strings.Split(csv, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
