package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cristiansteib/multiversectl/internal/app"
)

// An operation is one thing the page asked the server to do: start a universe,
// say. It runs in the background and its output is sent to every open page as
// it happens, so the person sees what the command line would have shown. Only
// one runs at a time in a project; a second would be two compose commands
// fighting over the same project.

// opRequest is what a page may ask for. Every action is a use case the command
// line has too.
type opRequest struct {
	Action    string `json:"action"` // up, down, rm, new, checkout, update, use
	Workspace string `json:"project"`
	Universe  string `json:"universe,omitempty"`
	Name      string `json:"name,omitempty"` // new: the number or name, empty for the next free number
	Note      string `json:"note,omitempty"`
	// Branch is, for new, the name of the new branch in every repository (it must
	// not exist yet) and, for checkout, the existing branch to check out.
	Branch string `json:"branch,omitempty"`
	// Base is where new branches start; OnlyExisting leaves repositories without
	// the branch on their base when checking out.
	Base         string `json:"base,omitempty"`
	OnlyExisting bool   `json:"onlyExisting,omitempty"`
	// Confirm must be the universe's own name to remove it: the page asks for it
	// to be typed, and the server checks it, so a stray request cannot remove one.
	Confirm string `json:"confirm,omitempty"`
	// Force removes a universe that has uncommitted work in a worktree.
	Force bool `json:"force,omitempty"`
}

type opLine struct {
	Kind string `json:"kind"` // info, ok, warn, out, err
	Text string `json:"text"`
}

type opView struct {
	ID        string   `json:"id"`
	Action    string   `json:"action"`
	Title     string   `json:"title"`
	Workspace string   `json:"project"`
	Universe  string   `json:"universe,omitempty"`
	State     string   `json:"state"` // running, done, failed
	Error     string   `json:"error,omitempty"`
	Started   string   `json:"started"`
	Ended     string   `json:"ended,omitempty"`
	Lines     []opLine `json:"lines,omitempty"`
}

// maxLines keeps one chatty build from growing without bound.
const (
	maxLines   = 3000
	keepOps    = 30
	maxReqBody = 4 << 10
)

type op struct {
	mu    sync.Mutex
	view  opView
	lines []opLine
}

func (o *op) snapshot(withLines bool) opView {
	o.mu.Lock()
	defer o.mu.Unlock()
	v := o.view
	if withLines {
		v.Lines = append([]opLine(nil), o.lines...)
	}
	return v
}

type ops struct {
	s    *Server
	mu   sync.Mutex
	all  []*op
	busy map[string]string // project -> operation id
	seq  int
}

func newOps(s *Server) *ops { return &ops{s: s, busy: map[string]string{}} }

func (m *ops) list() []opView {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]opView, len(m.all))
	for i, o := range m.all {
		out[i] = o.snapshot(true)
	}
	return out
}

// opError is a request that cannot be started, and why, for the page to show.
type opError struct {
	code int
	body map[string]any
}

func (e *opError) Error() string { return fmt.Sprint(e.body["error"]) }

func bad(code int, msg string) *opError {
	return &opError{code: code, body: map[string]any{"error": msg}}
}

var needsUniverse = map[string]bool{"up": true, "down": true, "rm": true}

func title(r opRequest) string {
	switch r.Action {
	case "up":
		return "Start " + r.Universe + " in " + r.Workspace
	case "down":
		return "Stop " + r.Universe + " in " + r.Workspace
	case "rm":
		return "Remove " + r.Universe + " from " + r.Workspace
	case "new":
		if strings.TrimSpace(r.Name) == "" {
			return "Create a universe in " + r.Workspace
		}
		return "Create " + r.Name + " in " + r.Workspace
	case "checkout":
		return "Check out " + r.Branch + " in " + r.Workspace
	case "update":
		return "Update " + r.Workspace
	case "use":
		return "Make " + r.Workspace + " the current project"
	}
	return r.Action
}

// start checks a request and begins it.
func (m *ops) start(ctx context.Context, r opRequest) (opView, *opError) {
	switch r.Action {
	case "up", "down", "rm", "new", "checkout", "update", "use":
	default:
		return opView{}, bad(http.StatusBadRequest, fmt.Sprintf("unknown action %q", r.Action))
	}
	if r.Workspace == "" {
		return opView{}, bad(http.StatusBadRequest, "project is required")
	}
	if needsUniverse[r.Action] && r.Universe == "" {
		return opView{}, bad(http.StatusBadRequest, "universe is required")
	}
	if r.Action == "checkout" && strings.TrimSpace(r.Branch) == "" {
		return opView{}, bad(http.StatusBadRequest, "the branch to check out is required")
	}
	if r.Action == "rm" {
		if r.Confirm != r.Universe {
			return opView{}, bad(http.StatusBadRequest, "type the universe's name to confirm removing it")
		}
		if !r.Force {
			c, cancel := context.WithTimeout(ctx, requestTimeout)
			dirty, err := m.s.b.Unsaved(c, r.Workspace, r.Universe)
			cancel()
			if err != nil {
				return opView{}, bad(http.StatusNotFound, err.Error())
			}
			if len(dirty) > 0 {
				return opView{}, &opError{code: http.StatusConflict, body: map[string]any{
					"error": "uncommitted work in " + strings.Join(dirty, ", ") + ": removing the universe discards it",
					"dirty": dirty,
				}}
			}
		}
	}

	m.mu.Lock()
	if id, busy := m.busy[r.Workspace]; busy {
		m.mu.Unlock()
		return opView{}, bad(http.StatusConflict, "another operation ("+id+") is running in "+r.Workspace+"; wait for it to finish")
	}
	m.seq++
	o := &op{view: opView{
		ID: "op" + strconv.Itoa(m.seq), Action: r.Action, Title: title(r), Workspace: r.Workspace, Universe: r.Universe,
		State: "running", Started: time.Now().UTC().Format(time.RFC3339),
	}}
	m.busy[r.Workspace] = o.view.ID
	m.all = append(m.all, o)
	if len(m.all) > keepOps {
		m.all = m.all[len(m.all)-keepOps:]
	}
	m.mu.Unlock()

	m.s.hub.publish("", event{Type: "op", Data: o.snapshot(false)})
	go m.run(o, r)
	return o.snapshot(false), nil
}

func (m *ops) run(o *op, r opRequest) {
	k := &sink{o: o, m: m}
	err := m.s.b.Do(m.s.runCtx, r, k)
	k.flush()

	o.mu.Lock()
	o.view.Ended = time.Now().UTC().Format(time.RFC3339)
	if err != nil {
		o.view.State, o.view.Error = "failed", err.Error()
	} else {
		o.view.State = "done"
	}
	o.mu.Unlock()

	m.mu.Lock()
	delete(m.busy, r.Workspace)
	m.mu.Unlock()
	m.s.hub.publish("", event{Type: "op", Data: o.snapshot(false)})
	m.s.refresh() // what it did is on the page in a moment, not at the next look
}

// sink is where an operation's words go: both the use case's own messages and
// what the commands it runs print.
type sink struct {
	o        *op
	m        *ops
	out, err lineWriter
}

var _ Sink = (*sink)(nil)

func (k *sink) line(kind, text string) {
	text = strings.TrimRight(ansi.ReplaceAllString(text, ""), " \t\r")
	if strings.TrimSpace(text) == "" {
		return
	}
	k.o.mu.Lock()
	if len(k.o.lines) >= maxLines {
		k.o.lines = k.o.lines[1:]
	}
	l := opLine{Kind: kind, Text: text}
	k.o.lines = append(k.o.lines, l)
	id := k.o.view.ID
	k.o.mu.Unlock()
	k.m.s.hub.publish("", event{Type: "opline", Data: map[string]any{"id": id, "kind": l.Kind, "text": l.Text}})
}

func (k *sink) Info(f string, a ...any)  { k.line("info", fmt.Sprintf(f, a...)) }
func (k *sink) OK(f string, a ...any)    { k.line("ok", fmt.Sprintf(f, a...)) }
func (k *sink) Warn(f string, a ...any)  { k.line("warn", fmt.Sprintf(f, a...)) }
func (k *sink) Plain(f string, a ...any) { k.line("info", fmt.Sprintf(f, a...)) }
func (k *sink) Table(headers []string, rows [][]string) {
	k.line("info", strings.Join(headers, "  "))
	for _, r := range rows {
		k.line("info", strings.Join(r, "  "))
	}
}

func (k *sink) Stdout() io.Writer {
	k.out = lineWriter{emit: func(s string) { k.line("out", s) }}
	return &k.out
}
func (k *sink) Stderr() io.Writer {
	k.err = lineWriter{emit: func(s string) { k.line("err", s) }}
	return &k.err
}
func (k *sink) flush() { k.out.flush(); k.err.flush() }

// lineWriter turns what a command writes into lines. Build tools redraw with a
// carriage return, so that ends a line too.
type lineWriter struct {
	mu   sync.Mutex
	buf  []byte
	emit func(string)
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	for {
		// On the bytes, not a copy of them: a build tool can write 32 KiB of
		// log in one call, and converting the rest of the buffer to a string
		// on every line would copy it once per line.
		i := bytes.IndexAny(w.buf, "\r\n")
		if i < 0 {
			break
		}
		w.emit(string(w.buf[:i]))
		w.buf = w.buf[i+1:]
	}
	return len(p), nil
}

func (w *lineWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.buf) > 0 && w.emit != nil {
		w.emit(string(w.buf))
		w.buf = nil
	}
}

var ansi = regexp.MustCompile("\x1b\\[[0-9;?]*[ -/]*[@-~]")

func (s *Server) opsEndpoint(w http.ResponseWriter, r *http.Request) {
	// Only a POST starts an operation. guard puts GET and HEAD together as
	// reads, and applies the read-only refusal and the CSRF header to POST
	// alone, so anything that is not a POST has to be answered as a read here
	// or it would pass through both checks.
	if r.Method != http.MethodPost {
		reply(w, map[string]any{"ops": s.ops.list()})
		return
	}
	var req opRequest
	body := http.MaxBytesReader(w, r.Body, maxReqBody)
	if err := json.NewDecoder(body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "the request is not valid JSON")
		return
	}
	v, oerr := s.ops.start(r.Context(), req)
	if oerr != nil {
		failWith(w, oerr.code, oerr.body)
		return
	}
	replyWith(w, http.StatusAccepted, v)
}

var _ = app.Output(nil)
