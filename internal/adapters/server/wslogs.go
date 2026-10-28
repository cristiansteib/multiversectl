package server

import (
	"context"
	"encoding/json"
	"sync"
)

// What the page may ask over its websocket. Everything that changes state goes
// through POST /api/ops; the socket only asks to look again, and to read a log.
type wsRequest struct {
	Type string `json:"type"`
	// ID names a log stream, chosen by the page so it can tell several apart.
	ID         string `json:"id,omitempty"`
	Multiverse string `json:"multiverse,omitempty"`
	Universe   string `json:"universe,omitempty"`
	App        string `json:"app,omitempty"`
	Service    string `json:"service,omitempty"`
	Tail       int    `json:"tail,omitempty"`
	Follow     bool   `json:"follow,omitempty"`
}

// maxLogsPerPage bounds the streams one page holds open; maxLogStreams bounds
// them all.
const (
	maxLogsPerPage = 4
	maxLogChunk    = 16 << 10
)

// logStream is one open log; it is compared by identity when it is forgotten.
type logStream struct{ cancel context.CancelFunc }

// logStreams are the logs one page has open, so they can be stopped by name and
// all of them when it goes.
type logStreams struct {
	mu   sync.Mutex
	open map[string]*logStream
}

func (l *logStreams) add(id string, st *logStream) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if old, ok := l.open[id]; ok {
		old.cancel() // the same name asked for again replaces what was there
	} else if len(l.open) >= maxLogsPerPage {
		return false
	}
	l.open[id] = st
	return true
}

func (l *logStreams) stop(id string) {
	l.mu.Lock()
	st := l.open[id]
	delete(l.open, id)
	l.mu.Unlock()
	if st != nil {
		st.cancel()
	}
}

// forget removes a finished stream, unless its name has since been given to another.
func (l *logStreams) forget(id string, st *logStream) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.open[id] == st {
		delete(l.open, id)
	}
}

// logEvent is a piece of a log, or the end of it, for the page that asked.
func logEvent(kind, id, text, errText string) []byte {
	d, _ := json.Marshal(event{Type: kind, Data: map[string]string{"id": id, "text": text, "error": errText}})
	return d
}

// wsLogWriter is where a log is written: each piece goes to the page that asked,
// waiting for room, so a page that reads slowly slows the log and nothing else.
type wsLogWriter struct {
	ctx context.Context
	c   *client
	id  string
}

func (w *wsLogWriter) Write(p []byte) (int, error) {
	for off := 0; off < len(p); {
		end := min(off+maxLogChunk, len(p))
		select {
		case w.c.direct <- logEvent("log", w.id, string(p[off:end]), ""):
		case <-w.ctx.Done():
			return off, w.ctx.Err()
		}
		off = end
	}
	return len(p), nil
}

// startLogs begins one log for a page: its output goes to that page's socket,
// until it ends, the page stops it, or the connection closes.
func (s *Server) startLogs(conn context.Context, c *client, streams *logStreams, m wsRequest) {
	end := func(msg string) {
		select {
		case c.direct <- logEvent("logEnd", m.ID, "", msg):
		case <-conn.Done():
		}
	}
	if m.ID == "" || len(m.ID) > 64 || m.Multiverse == "" || m.Universe == "" || m.App == "" || m.Service == "" {
		end("multiverse, universe, app and service are required")
		return
	}
	select {
	case s.logSlots <- struct{}{}:
	default:
		end("too many logs are open; close one")
		return
	}
	ctx, cancel := context.WithCancel(conn)
	st := &logStream{cancel: cancel}
	if !streams.add(m.ID, st) {
		<-s.logSlots
		cancel()
		end("this page already has several logs open; close one")
		return
	}
	go func() {
		defer func() { <-s.logSlots }()
		defer streams.forget(m.ID, st)
		defer cancel()
		runCtx := ctx
		if !m.Follow {
			var stop context.CancelFunc
			runCtx, stop = context.WithTimeout(ctx, requestTimeout)
			defer stop()
		}
		err := s.b.Logs(runCtx, m.Multiverse, m.Universe, m.App, m.Service, m.Tail, m.Follow, &wsLogWriter{ctx: ctx, c: c, id: m.ID})
		if ctx.Err() != nil {
			return // stopped by the page, or it has gone: nobody to tell
		}
		if err != nil {
			end(err.Error())
			return
		}
		end("")
	}()
}
