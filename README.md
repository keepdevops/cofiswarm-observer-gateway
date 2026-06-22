# cofiswarm-observer-gateway

WebSocket gateway for the Observer Panel: bridges browser dashboards (persistent
full-duplex `ws://`/`wss://`) to the cofiswarm ZMQ carrier.

```
browser ⇄ ws://:8820/ws ⇄ gateway ⇄ SUB egress :5557 (events) / PUB ingress :5556 (commands)
```

- **Events** — SUBs the zmq-bridge egress (`COFISWARM_ZMQ_EGRESS_ADDR`, default
  `tcp://127.0.0.1:5557`, filter `COFISWARM_ZMQ_FILTER` default `swarm.`) and fans every
  frame out to all connected dashboards. Reconnects with capped backoff.
- **Commands** — a dashboard may publish onto the bus; the gateway PUBs to the ingress
  (`COFISWARM_ZMQ_ADDR`, e.g. `tcp://127.0.0.1:5556`; unset = read-only). Commands are
  guarded by `COFISWARM_CMD_PREFIX` (default `swarm.observer.`) so the browser can't inject
  arbitrary topics.
- **Dashboard** — a configurable widget panel at `/` (served with `/dashboard.js`) connects
  to `/ws` and renders live telemetry. Widgets are **draggable** (reorder), **resizable**,
  and **add/removable**, with the layout persisted to `localStorage`. Built-in widget kinds:
  online-component count, events/sec, slot-pressure and KV-pressure line charts, events-by-
  topic bars, and a live event feed — all bound to bus topics. Dependency-free (canvas
  charts, no CDN) so the gateway stays a single offline-safe binary. `/healthz` reports
  client count + command capability.

WebSocket `Origin` allowlist: `COFISWARM_WS_ORIGINS` (comma-separated; empty = same-origin).

## Run

```bash
COFISWARM_ZMQ_EGRESS_ADDR=tcp://127.0.0.1:5557 \
COFISWARM_ZMQ_ADDR=tcp://127.0.0.1:5556 \
go run ./cmd/cofiswarm-observer-gateway --listen :8820
```
