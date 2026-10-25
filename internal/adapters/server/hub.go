package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// How often the server looks at the machine while someone is watching. The
// fleet asks the container runtime about every universe, so it is quick enough
// to feel live and no quicker; an action that finishes asks for an immediate
// look instead of waiting for the next one.
const (
	fleetEvery  = 4 * time.Second
	statusEvery = 20 * time.Second
	writeWait   = 10 * time.Second
)

// event is one message to the page.
type event struct {
	Type string `json:"type"`
	Data any    `json:"data,omitempty"`
}

// hub fans events out to the connected pages. A topic keeps its last value, so
// a page that connects is told where things stand at once, and an unchanged
// value is not sent again.
type hub struct {
	mu      sync.Mutex
	clients map[*client]struct{}
	last    map[string][]byte
}

type client struct {
	send chan []byte
	drop func()
}

func newHub() *hub { return &hub{clients: map[*client]struct{}{}, last: map[string][]byte{}} }

func (h *hub) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

// publish sends an event to everyone. With a topic it is kept for pages that
// join later, and skipped when it equals what was last sent.
func (h *hub) publish(topic string, e event) {
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if topic != "" {
		if bytes.Equal(h.last[topic], data) {
			return
		}
		h.last[topic] = data
	}
	for c := range h.clients {
		select {
		case c.send <- data:
		default:
			// A page that cannot keep up is let go; it reconnects and is told
			// the current state, which is better than a growing backlog.
			go c.drop()
		}
	}
}

// join registers a page and returns what it should be told first.
func (h *hub) join(c *client) [][]byte {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[c] = struct{}{}
	var snap [][]byte
	for _, t := range []string{"overview", "fleet"} {
		if d, ok := h.last[t]; ok {
			snap = append(snap, d)
		}
	}
	return snap
}

func (h *hub) leave(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, c)
}

// watch looks at the machine and publishes what changed, but only while a page
// is open: nobody is served by asking the container runtime for no one.
func (s *Server) watch(ctx context.Context) {
	fleet := time.NewTicker(fleetEvery)
	status := time.NewTicker(statusEvery)
	defer fleet.Stop()
	defer status.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.look:
			s.look1(ctx, true, true)
		case <-fleet.C:
			if s.hub.count() > 0 {
				s.look1(ctx, true, false)
			}
		case <-status.C:
			if s.hub.count() > 0 {
				s.look1(ctx, false, true)
			}
		}
	}
}

func (s *Server) look1(ctx context.Context, fleet, status bool) {
	var wg sync.WaitGroup
	if fleet {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, cancel := context.WithTimeout(ctx, requestTimeout)
			defer cancel()
			v, err := s.b.Fleet(c, "")
			if err != nil {
				s.hub.publish("fleet", event{Type: "fleet", Data: fleetView{Workspaces: []fleetWorkspace{}, Error: err.Error()}})
				return
			}
			s.hub.publish("fleet", event{Type: "fleet", Data: v})
		}()
	}
	if status {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, cancel := context.WithTimeout(ctx, requestTimeout)
			defer cancel()
			rep, err := s.b.Status(c, false)
			if err != nil {
				return
			}
			s.hub.publish("overview", event{Type: "overview", Data: overviewView{Version: s.o.Version, ReadOnly: s.o.ReadOnly, Status: rep}})
		}()
	}
	wg.Wait()
}

// refresh asks the watcher to look now.
func (s *Server) refresh() {
	select {
	case s.look <- struct{}{}:
	default: // one is already queued
	}
}

// ws is the page's live connection: it is told the state when it joins and each
// change after, plus the output of what it asked to run. It may only ask to
// look again; everything that changes state goes through POST /api/ops, so
// there is one place that checks it.
func (s *Server) ws(w http.ResponseWriter, r *http.Request) {
	// Accept refuses an Origin that is not this host, which is what keeps a page
	// on another site from listening in.
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(1 << 10)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	c := &client{send: make(chan []byte, 64), drop: cancel}
	snap := s.hub.join(c)
	defer s.hub.leave(c)

	hello := event{Type: "hello", Data: helloView{Version: s.o.Version, Commit: s.o.Commit, Date: s.o.Date, ReadOnly: s.o.ReadOnly, Ops: s.ops.list()}}
	if !s.writeEvent(ctx, conn, hello) {
		return
	}
	for _, d := range snap {
		if err := writeRaw(ctx, conn, d); err != nil {
			return
		}
	}
	if len(snap) == 0 {
		s.refresh() // nobody has looked yet: this page is the reason to
	}

	go func() {
		defer cancel()
		for {
			_, msg, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var m struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(msg, &m) == nil && m.Type == "refresh" {
				s.refresh()
			}
		}
	}()

	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			conn.Close(websocket.StatusNormalClosure, "")
			return
		case d := <-c.send:
			if err := writeRaw(ctx, conn, d); err != nil {
				return
			}
		case <-ping.C:
			pctx, pcancel := context.WithTimeout(ctx, writeWait)
			err := conn.Ping(pctx)
			pcancel()
			if err != nil {
				return
			}
		}
	}
}

func (s *Server) writeEvent(ctx context.Context, conn *websocket.Conn, e event) bool {
	d, err := json.Marshal(e)
	return err == nil && writeRaw(ctx, conn, d) == nil
}

func writeRaw(ctx context.Context, conn *websocket.Conn, d []byte) error {
	wctx, cancel := context.WithTimeout(ctx, writeWait)
	defer cancel()
	return conn.Write(wctx, websocket.MessageText, d)
}
