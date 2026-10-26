package server

import (
	"net/http"
	"strconv"
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
	project, universe := r.URL.Query().Get("project"), r.URL.Query().Get("universe")
	if project == "" || universe == "" {
		fail(w, http.StatusBadRequest, "project and universe are required")
		return
	}
	ctx, cancel := s.ctx(r)
	defer cancel()
	rows, err := s.b.Containers(ctx, project, universe)
	if err != nil {
		fail(w, http.StatusNotFound, err.Error())
		return
	}
	reply(w, map[string]any{"containers": rows})
}

func (s *Server) stats(w http.ResponseWriter, r *http.Request) {
	project, universe := r.URL.Query().Get("project"), r.URL.Query().Get("universe")
	if project == "" || universe == "" {
		fail(w, http.StatusBadRequest, "project and universe are required")
		return
	}
	ctx, cancel := s.ctx(r)
	defer cancel()
	rows, err := s.b.Stats(ctx, project, universe)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	reply(w, map[string]any{"stats": rows})
}

// logs streams a service's log as plain text, one flush per write so a followed
// log shows as it happens. The request's own context ends it: closing the panel
// aborts the fetch, which kills the docker process behind it.
func (s *Server) logs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	project, universe, appKey, service := q.Get("project"), q.Get("universe"), q.Get("app"), q.Get("service")
	if project == "" || universe == "" || appKey == "" || service == "" {
		fail(w, http.StatusBadRequest, "project, universe, app and service are required")
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
			fail(w, http.StatusNotFound, err.Error())
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
