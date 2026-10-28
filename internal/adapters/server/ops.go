package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
// one runs at a time in a multiverse; a second would be two compose commands
// fighting over the same multiverse.

// opRequest is what a page may ask for. Every action is a use case the command
// line has too.
type opRequest struct {
	Action     string `json:"action"` // up, down, rm, sync, restart, new, checkout, update, use
	Multiverse string `json:"multiverse"`
	Universe   string `json:"universe,omitempty"`
	// App and Service name what restart acts on: a service of one app.
	App     string `json:"app,omitempty"`
	Service string `json:"service,omitempty"`
	Name    string `json:"name,omitempty"` // new: the number or name, empty for the next free number
	Note    string `json:"note,omitempty"`
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
	ID         string   `json:"id"`
	Action     string   `json:"action"`
	Title      string   `json:"title"`
	Multiverse string   `json:"multiverse"`
	Universe   string   `json:"universe,omitempty"`
	State      string   `json:"state"` // running, done, failed
	Error      string   `json:"error,omitempty"`
	Started    string   `json:"started"`
	Ended      string   `json:"ended,omitempty"`
	Lines      []opLine `json:"lines,omitempty"`
}

// maxLines keeps one chatty build from growing without bound.
const (
	maxLines   = 3000
	keepOps    = 30
	maxReqBody = 4 << 10
)

type op struct {
	mu    sync.Mutex
	key   string // what it holds in the busy table
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
	busy map[string]string // multiverse -> operation id
	seq  int
}

func newOps(s *Server) *ops { return &ops{s: s, busy: map[string]string{}} }

// byID is the operation with that id, nil when it is unknown or has been
// dropped from the last few kept.
func (m *ops) byID(id string) *op {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, o := range m.all {
		if o.view.ID == id {
			return o
		}
	}
	return nil
}

// await waits for an operation to end, up to d or until ctx is done.
func (m *ops) await(ctx context.Context, o *op, d time.Duration) {
	if d <= 0 {
		return
	}
	end := time.NewTimer(d)
	defer end.Stop()
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for o.snapshot(false).State == "running" {
		select {
		case <-ctx.Done():
			return
		case <-end.C:
			return
		case <-tick.C:
		}
	}
}

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

// selfScope is what the update of the program holds. It is not a multiverse's
// name, which cannot start with a '#'.
const selfScope = "#self"

var needsUniverse = map[string]bool{"up": true, "down": true, "rm": true, "sync": true, "restart": true}

func title(r opRequest) string {
	switch r.Action {
	case "up":
		return "Start " + r.Universe + " in " + r.Multiverse
	case "down":
		return "Stop " + r.Universe + " in " + r.Multiverse
	case "rm":
		return "Remove " + r.Universe + " from " + r.Multiverse
	case "restart":
		return "Restart " + r.App + "/" + r.Service + " in " + r.Universe + " of " + r.Multiverse
	case "sync":
		return "Bring " + r.Universe + " up to date in " + r.Multiverse
	case "new":
		if strings.TrimSpace(r.Name) == "" {
			return "Create a universe in " + r.Multiverse
		}
		return "Create " + r.Name + " in " + r.Multiverse
	case "checkout":
		return "Check out " + r.Branch + " in " + r.Multiverse
	case "update":
		return "Update " + r.Multiverse
	case "selfupdate":
		return "Update multiversectl"
	}
	return r.Action
}

// universeOf is the universe an operation is about, so the page can list it with
// that universe's others: a new universe is named by the request, not yet known
// to the server as anything else.
func universeOf(r opRequest) string {
	if r.Universe == "" && (r.Action == "new" || r.Action == "checkout") {
		return strings.TrimSpace(r.Name)
	}
	return r.Universe
}

// What an operation holds while it runs. Universes are meant to run side by
// side, so starting, stopping or removing one does not wait for another's: each
// holds only its own universe. Creating a universe takes the next number and
// new branches in every repository, so creations take turns. Updating or
// switching a multiverse changes the multiverse itself, so it waits for everything
// in it, and everything waits for it.
func scopeOf(r opRequest) string {
	switch r.Action {
	case "up", "down", "rm", "sync", "restart":
		return r.Multiverse + "/" + r.Universe
	case "new", "checkout":
		return r.Multiverse + "#create"
	case "selfupdate":
		return selfScope
	}
	return r.Multiverse
}

// conflict says which running operation stops this one from starting. The
// caller holds m.mu.
func (m *ops) conflict(r opRequest, key string) (string, bool) {
	// While the program is being replaced nothing else may start, and it does not
	// start while anything else runs: it ends by starting the server again, which
	// would cut that short.
	if id, ok := m.busy[selfScope]; ok {
		return id, true
	}
	if key == selfScope {
		for _, id := range m.busy {
			return id, true
		}
	}
	if id, ok := m.busy[r.Multiverse]; ok {
		return id, true // the multiverse itself is being changed
	}
	if key == r.Multiverse { // this one changes the multiverse: nothing else may be running in it
		for k, id := range m.busy {
			if strings.HasPrefix(k, r.Multiverse+"/") || strings.HasPrefix(k, r.Multiverse+"#") {
				return id, true
			}
		}
	}
	id, ok := m.busy[key]
	return id, ok
}

// start checks a request and begins it.
func (m *ops) start(ctx context.Context, r opRequest) (opView, *opError) {
	if m.s.restarting.Load() {
		return opView{}, bad(http.StatusServiceUnavailable, "the server is restarting to run the update; try again in a moment")
	}
	switch r.Action {
	case "up", "down", "rm", "sync", "restart", "new", "checkout", "update", "selfupdate":
	default:
		return opView{}, bad(http.StatusBadRequest, fmt.Sprintf("unknown action %q", r.Action))
	}
	if r.Multiverse == "" && r.Action != "selfupdate" {
		return opView{}, bad(http.StatusBadRequest, "multiverse is required")
	}
	if needsUniverse[r.Action] && r.Universe == "" {
		return opView{}, bad(http.StatusBadRequest, "universe is required")
	}
	if r.Action == "restart" && (r.App == "" || r.Service == "") {
		return opView{}, bad(http.StatusBadRequest, "the app and the service to restart are required")
	}
	if r.Action == "checkout" && strings.TrimSpace(r.Branch) == "" {
		return opView{}, bad(http.StatusBadRequest, "the branch to check out is required")
	}
	if r.Action == "rm" {
		if r.Confirm != r.Universe {
			return opView{}, bad(http.StatusBadRequest, "type the universe's name to confirm removing it")
		}
		// Asked here as well as in the use case, which refuses too: the page
		// needs the list of repositories to put in front of the person before
		// it offers to go ahead, and an operation that has already started is
		// too late to be offering anything.
		if !r.Force {
			c, cancel := context.WithTimeout(ctx, requestTimeout)
			dirty, err := m.s.b.Unsaved(c, r.Multiverse, r.Universe)
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
	key := scopeOf(r)
	if id, busy := m.conflict(r, key); busy {
		m.mu.Unlock()
		where := ""
		if r.Multiverse != "" && key != selfScope {
			where = " in " + r.Multiverse
		}
		return opView{}, bad(http.StatusConflict, "another operation ("+id+") is running"+where+"; wait for it to finish")
	}
	m.seq++
	o := &op{key: key, view: opView{
		ID: "op" + strconv.Itoa(m.seq), Action: r.Action, Title: title(r), Multiverse: r.Multiverse, Universe: universeOf(r),
		State: "running", Started: time.Now().UTC().Format(time.RFC3339),
	}}
	m.busy[key] = o.view.ID
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
	restart := errors.Is(err, ErrRestart)
	if restart {
		err = nil
	}

	o.mu.Lock()
	o.view.Ended = time.Now().UTC().Format(time.RFC3339)
	if err != nil {
		o.view.State, o.view.Error = "failed", err.Error()
	} else {
		o.view.State = "done"
	}
	o.mu.Unlock()

	m.mu.Lock()
	delete(m.busy, o.key)
	m.mu.Unlock()
	m.s.hub.publish("", event{Type: "op", Data: o.snapshot(false)})
	m.s.refresh() // what it did is on the page in a moment, not at the next look
	if restart {
		m.s.restartSoon()
	}
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

// Universe tells the operation which universe it made, when the request left the
// name to the server (the next free number): from then on it is listed with that
// universe's others.
func (k *sink) Universe(name string) {
	k.o.mu.Lock()
	k.o.view.Universe = name
	k.o.mu.Unlock()
	k.m.s.hub.publish("", event{Type: "op", Data: k.o.snapshot(false)})
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
