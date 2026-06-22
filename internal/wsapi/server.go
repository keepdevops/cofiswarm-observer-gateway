// Package wsapi is the browser-facing side of the gateway: a WebSocket endpoint that pushes
// live bus events to connected dashboards (full-duplex) and accepts commands to republish
// onto the bus. It also serves a tiny embedded dashboard so the gateway is usable standalone.
package wsapi

import (
	"context"
	_ "embed"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/keepdevops/cofiswarm-observer-gateway/internal/hub"
)

//go:embed index.html
var indexHTML []byte

// Publisher is the command sink: the WS read loop republishes browser commands through it.
type Publisher interface {
	Publish(topic string, payload map[string]any) error
	CanPublish() bool
}

// Server wires the hub (event fan-out) and a Publisher (command sink) to HTTP/WS handlers.
type Server struct {
	hub            *hub.Hub
	pub            Publisher
	cmdPrefix      string   // commands may only publish topics with this prefix (anti-injection)
	allowedOrigins []string // WS Origin allowlist; empty => same-origin only
}

func New(h *hub.Hub, p Publisher, cmdPrefix string, allowedOrigins []string) *Server {
	return &Server{hub: h, pub: p, cmdPrefix: cmdPrefix, allowedOrigins: allowedOrigins}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(indexHTML)
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok", "clients": s.hub.Count(), "commands": s.pub.CanPublish(),
		})
	})
	mux.HandleFunc("/ws", s.handleWS)
	return mux
}

// command is the browser->gateway message envelope. type "publish" republishes {topic,
// payload} onto the bus (subject to the cmdPrefix guard); unknown types are rejected.
type command struct {
	Type    string         `json:"type"`
	Topic   string         `json:"topic"`
	Payload map[string]any `json:"payload"`
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: s.allowedOrigins})
	if err != nil {
		log.Printf("ws: accept: %v", err)
		return
	}
	defer c.CloseNow()
	ctx := r.Context()

	client := s.hub.Add(64)
	defer s.hub.Remove(client)
	log.Printf("ws: client connected (%d total)", s.hub.Count())

	// Writer: drain the hub's per-client channel to the socket until either side closes.
	go func() {
		for frame := range client.Send() {
			wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := c.Write(wctx, websocket.MessageText, frame)
			cancel()
			if err != nil {
				return
			}
		}
	}()

	// Reader: handle inbound commands until the client disconnects.
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			log.Printf("ws: client disconnected: %v", websocket.CloseStatus(err))
			return
		}
		s.handleCommand(c, ctx, data)
	}
}

// handleCommand validates and dispatches one inbound command, replying with an ack/error
// frame so the UI gets explicit feedback (never a silent failure).
func (s *Server) handleCommand(c *websocket.Conn, ctx context.Context, data []byte) {
	var cmd command
	if err := json.Unmarshal(data, &cmd); err != nil {
		s.reply(c, ctx, "error", "malformed command json")
		return
	}
	if cmd.Type != "publish" {
		s.reply(c, ctx, "error", "unsupported command type: "+cmd.Type)
		return
	}
	if !strings.HasPrefix(cmd.Topic, s.cmdPrefix) {
		s.reply(c, ctx, "error", "topic must start with "+s.cmdPrefix)
		return
	}
	if err := s.pub.Publish(cmd.Topic, cmd.Payload); err != nil {
		log.Printf("ws: command publish %s: %v", cmd.Topic, err)
		s.reply(c, ctx, "error", "publish failed: "+err.Error())
		return
	}
	s.reply(c, ctx, "ack", cmd.Topic)
}

func (s *Server) reply(c *websocket.Conn, ctx context.Context, kind, detail string) {
	frame, _ := json.Marshal(map[string]string{"type": kind, "detail": detail})
	wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := c.Write(wctx, websocket.MessageText, frame); err != nil {
		log.Printf("ws: reply write: %v", err)
	}
}
