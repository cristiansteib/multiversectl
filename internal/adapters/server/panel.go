package server

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/cristiansteib/multiversectl/internal/app"
	"github.com/cristiansteib/multiversectl/internal/domain"
)

// The side panel of a universe asks for these when it opens and stops when it
// closes: nothing here runs for a universe nobody is looking at. All three are
// reads, so they work in --read-only too; they name containers by the app and
// service they belong to and the backend looks the container up among the
// universe's own, so a page cannot ask for any other.

// maxLogStreams bounds the log streams held open at once, each of which is a
// docker process and a connection.
const maxLogStreams = 12

func (s *Server) containers(w http.ResponseWriter, r *http.Request) {
	project, universe := r.URL.Query().Get("multiverse"), r.URL.Query().Get("universe")
	if project == "" || universe == "" {
		fail(w, http.StatusBadRequest, "multiverse and universe are required")
		return
	}
	ctx, cancel := s.ctx(r)
	defer cancel()
	rows, err := s.b.Containers(ctx, project, universe)
	if err != nil {
		failErr(w, err)
		return
	}
	reply(w, map[string]any{"containers": rows})
}

func (s *Server) stats(w http.ResponseWriter, r *http.Request) {
	project, universe := r.URL.Query().Get("multiverse"), r.URL.Query().Get("universe")
	if project == "" || universe == "" {
		fail(w, http.StatusBadRequest, "multiverse and universe are required")
		return
	}
	ctx, cancel := s.ctx(r)
	defer cancel()
	rows, err := s.b.Stats(ctx, project, universe)
	if err != nil {
		failErr(w, err)
		return
	}
	reply(w, map[string]any{"stats": rows})
}

// code is one universe's repositories. It is its own request because it costs
// a handful of git calls and a call to a hosting service per repository, which
// the fleet listing is polled far too often to carry.
func (s *Server) code(w http.ResponseWriter, r *http.Request) {
	project, universe := r.URL.Query().Get("multiverse"), r.URL.Query().Get("universe")
	if project == "" || universe == "" {
		fail(w, http.StatusBadRequest, "multiverse and universe are required")
		return
	}
	ctx, cancel := s.ctx(r)
	defer cancel()
	v, err := s.b.Code(ctx, project, universe, r.URL.Query().Get("fetch") == "1")
	if err != nil {
		failErr(w, err)
		return
	}
	reply(w, v)
}

// diff is the files of one repository's change, or with a file, that file's
// diff. A read, like the rest of the panel, and it names no path of its own:
// the backend only answers for files that are in the change.
func (s *Server) diff(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	project, universe, repo, scope := q.Get("multiverse"), q.Get("universe"), q.Get("repo"), q.Get("scope")
	if project == "" || universe == "" || repo == "" {
		fail(w, http.StatusBadRequest, "multiverse, universe and repo are required")
		return
	}
	if scope == "" {
		scope = app.ScopeBranch
	}
	if scope != app.ScopeBranch && scope != app.ScopeUncommitted {
		fail(w, http.StatusBadRequest, "scope is branch or uncommitted")
		return
	}
	base := q.Get("base")
	ctx, cancel := s.ctx(r)
	defer cancel()
	if file := q.Get("file"); file != "" {
		v, err := s.b.Patch(ctx, project, universe, repo, scope, base, file)
		if err != nil {
			failErr(w, err)
			return
		}
		reply(w, v)
		return
	}
	v, err := s.b.Diff(ctx, project, universe, repo, scope, base)
	if err != nil {
		failErr(w, err)
		return
	}
	reply(w, v)
}

// fleetStats is every running container of every installed multiverse, or of one
// when the page asks for one: the stats view plots the whole machine.
func (s *Server) fleetStats(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()
	v, err := s.b.FleetStats(ctx, r.URL.Query().Get("multiverse"))
	if err != nil {
		failErr(w, err)
		return
	}
	reply(w, v)
}

// failErr says 404 for something that is not there and 500 for everything
// else, so a page can tell a typo from a runtime that is down.
func failErr(w http.ResponseWriter, err error) {
	if errors.Is(err, domain.ErrNotFound) {
		fail(w, http.StatusNotFound, err.Error())
		return
	}
	fail(w, http.StatusInternalServerError, err.Error())
}

// logs streams a service's log as plain text, one flush per write so a followed
// log shows as it happens. The request's own context ends it: closing the panel
// aborts the fetch, which kills the docker process behind it.
func (s *Server) logs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	project, universe, appKey, service := q.Get("multiverse"), q.Get("universe"), q.Get("app"), q.Get("service")
	if project == "" || universe == "" || appKey == "" || service == "" {
		fail(w, http.StatusBadRequest, "multiverse, universe, app and service are required")
		return
	}
	tail, _ := strconv.Atoi(q.Get("tail"))
	follow := q.Get("follow") == "1"

	select {
	case s.logSlots <- struct{}{}:
		defer func() { <-s.logSlots }()
	default:
		fail(w, http.StatusTooManyRequests, "too many logs are open; close one")
		return
	}

	ctx := r.Context()
	if !follow {
		var cancel func()
		ctx, cancel = s.ctx(r)
		defer cancel()
	}
	h := w.Header()
	h.Set("Content-Type", "text/plain; charset=utf-8")
	h.Set("X-Accel-Buffering", "no")
	fw := &flushWriter{w: w, rc: http.NewResponseController(w)}
	if err := s.b.Logs(ctx, project, universe, appKey, service, tail, follow, fw); err != nil {
		if !fw.wrote {
			failErr(w, err)
			return
		}
		_, _ = fw.Write([]byte("\n[logs stopped: " + err.Error() + "]\n"))
	}
}

// flushWriter sends each write on at once.
type flushWriter struct {
	w     http.ResponseWriter
	rc    *http.ResponseController
	wrote bool
}

func (f *flushWriter) Write(p []byte) (int, error) {
	f.wrote = true
	n, err := f.w.Write(p)
	_ = f.rc.Flush()
	return n, err
}
