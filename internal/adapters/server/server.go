// Package server is a web view of this machine's setup and fleet, and a way to
// act on it: the root catalog and its tree, the multiverses installed here and
// how they have drifted, the universes each one is running, and the commands
// that start, stop, create and remove them. It serves one embedded page, a small
// JSON API and a websocket that pushes what changes, over the same use cases the
// command line runs.
//
// It has one credential, a token made when it starts (see auth.go), because a
// page that can remove a universe must not be reachable by anyone who can reach
// the port. With ReadOnly it offers no action at all.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/cristiansteib/multiversectl/internal/app"
)

// requestTimeout bounds one API call. The slow ones ask the container runtime
// or the network, and a stuck one should not hold a connection open for ever.
const requestTimeout = 45 * time.Second

// Options configure a server.
type Options struct {
	Addr string
	// Version, Commit and Date are what this binary was built as.
	Version, Commit, Date string
	// Expose allows a non-loopback address and any Host header. The token still
	// applies; it is the decision to be reachable from the network.
	Expose bool
	// ReadOnly offers no action: every POST is refused and the page hides the
	// buttons.
	ReadOnly bool
	// Token is the credential; made at random when empty.
	Token string
	// Restart replaces the running server with the newly installed one, after an
	// update; the process itself starts again when nil. Tests stand in for it.
	Restart func()
	// App is the web app's files; the build embedded in the binary when nil.
	App fs.FS
	// UpdateEvery is how often to look for a newer release; zero is
	// app.UpdateEvery.
	UpdateEvery time.Duration
}

// Server is the handler and what runs behind it: the hub that feeds the
// websocket, and the operations in progress.
type Server struct {
	b       Backend
	o       Options
	hub     *hub
	ops     *ops
	started time.Time
	look    chan struct{}
	runCtx  context.Context // what operations run under: the server's lifetime
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	// logSlots caps the log streams open at once (see maxLogStreams).
	logSlots chan struct{}
	// pair is the code that lets a second machine in without the link.
	pair pairing
	// restarting is set once an update has replaced the program: the server is
	// about to start again, so it takes nothing new.
	restarting atomic.Bool
	// lookedAt is when a page last made the server look for a release.
	lookedAt atomic.Int64
}

// New builds a server and starts what watches the fleet for changes. Close it.
func New(b Backend, o Options) *Server {
	if o.Token == "" {
		o.Token = newToken()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{b: b, o: o, started: time.Now(), look: make(chan struct{}, 1), runCtx: ctx, cancel: cancel, logSlots: make(chan struct{}, maxLogStreams)}
	s.hub = newHub()
	s.ops = newOps(s)
	s.wg.Add(1)
	go func() { defer s.wg.Done(); s.watch(ctx) }()
	s.wg.Add(1)
	go func() { defer s.wg.Done(); s.watchUpdates(ctx) }()
	return s
}

// Close stops the watcher and what it started. It is safe to call twice.
func (s *Server) Close() {
	s.cancel()
	s.wg.Wait()
}

// Token is the credential a browser trades for its cookie.
func (s *Server) Token() string { return s.o.Token }

// Handler returns the page, the API and the websocket. Unless Expose is set it
// answers only requests addressed to this machine by name or loopback address:
// without that check a web page elsewhere could point a hostname it controls at
// 127.0.0.1 and talk to the server through the browser.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/overview", s.overview)
	mux.HandleFunc("/api/catalog", s.catalog)
	mux.HandleFunc("/api/catalog/ignore", s.catalogIgnore)
	mux.HandleFunc("/api/update", s.updateLook)
	mux.HandleFunc("/api/catalog/plan", s.catalogPlan)
	mux.HandleFunc("/api/fleet", s.fleet)
	mux.HandleFunc("/api/multiverses/", s.multiverse)
	mux.HandleFunc("/api/doctor", s.doctor)
	mux.HandleFunc("/api/system", s.system)
	mux.HandleFunc("/api/next", s.next)
	mux.HandleFunc("/api/branches", s.branches)
	mux.HandleFunc("/api/ops", s.opsEndpoint)
	mux.HandleFunc("/api/universe/containers", s.containers)
	mux.HandleFunc("/api/universe/stats", s.stats)
	mux.HandleFunc("/api/universe/logs", s.logs)
	mux.HandleFunc("/api/universe/code", s.code)
	mux.HandleFunc("/api/universe/diff", s.diff)
	mux.HandleFunc("/api/pair", s.pairNew)
	mux.HandleFunc("/api/stats", s.fleetStats)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { fail(w, http.StatusNotFound, "no such endpoint") })
	mux.HandleFunc("/ws", s.ws)

	mux.HandleFunc("/style.css", serveSignInCSS)
	mux.HandleFunc("/app/", toRoot)
	files := s.o.App
	if files == nil {
		files, _ = fs.Sub(webappFiles, "webapp")
	}
	mux.Handle("/", appHandler(files))
	return s.guard(mux)
}

// guard applies what every response needs: the host check, no cross-site
// requests, the credential, a header only the page's own script sends on
// anything that changes state, and no framing, no outside origins and nothing
// cached.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Cache-Control", "no-store")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")

		if !s.o.Expose && !localHost(r.Host) {
			fail(w, http.StatusForbidden, "this server answers only on localhost; start it with --expose to reach it by another name")
			return
		}
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" || !sameOrigin(r) {
			fail(w, http.StatusForbidden, "cross-site requests are refused")
			return
		}
		if exchange(w, r, s.o.Token) {
			return
		}
		// Typing a pairing code is the one thing a request without the cookie
		// may do, and the sign-in page's stylesheet the one thing it may read:
		// the page is shown before anything of this server's has been allowed
		// to load, and an unstyled page is a page nobody trusts.
		if r.URL.Path == pairPath {
			s.pairUse(w, r)
			return
		}
		if !authorized(r, s.o.Token) {
			if r.URL.Path == "/style.css" && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
				next.ServeHTTP(w, r)
				return
			}
			if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/ws" {
				fail(w, http.StatusUnauthorized, "not signed in: open the link the server printed, or ask for a code with: multiversectl server pair")
				return
			}
			s.signIn(w, http.StatusUnauthorized, "")
			return
		}
		switch r.Method {
		case http.MethodGet, http.MethodHead:
		case http.MethodPost:
			if s.o.ReadOnly {
				fail(w, http.StatusForbidden, "this server was started read-only")
				return
			}
			if r.Header.Get(csrfHeader) != csrfValue {
				fail(w, http.StatusForbidden, "missing "+csrfHeader)
				return
			}
		default:
			w.Header().Set("Allow", "GET, HEAD, POST")
			fail(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func localHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s *Server) ctx(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), requestTimeout)
}

func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()
	rep, err := s.b.Status(ctx, r.URL.Query().Get("fetch") == "1")
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	reply(w, overviewOf(s.o.Version, s.o.ReadOnly, rep))
}

func (s *Server) catalog(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()
	v, err := s.b.Catalog(ctx, r.URL.Query().Get("fetch") == "1", func(e app.CatalogEvent) {
		// Not a topic: a step is news once, and a page that joins later is
		// better served by the tree the request returns than by old steps.
		s.hub.publish("", event{Type: "catalogProgress", Data: catalogProgress{Repo: e.Repo, Ref: e.Ref, Phase: e.Phase, Detail: e.Detail, Millis: e.Millis}})
	})
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	reply(w, v)
}

// catalogIgnore leaves a branch of the catalog tree out on this machine, or brings
// it back: {"path": "backend/legacy", "ignore": true}. Only a POST, which is what
// the guard holds to the read-only refusal and the CSRF header.
func (s *Server) catalogIgnore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		fail(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	var req struct {
		Path   string `json:"path"`
		Ignore bool   `json:"ignore"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxReqBody)).Decode(&req); err != nil || strings.TrimSpace(req.Path) == "" {
		fail(w, http.StatusBadRequest, "the path of the catalog entry is required")
		return
	}
	ctx, cancel := s.ctx(r)
	defer cancel()
	if err := s.b.Ignore(ctx, req.Path, req.Ignore); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	s.refresh() // the overview's drift reads the tree too
	reply(w, map[string]any{"path": strings.Trim(req.Path, "/"), "ignored": req.Ignore})
}

// updateLook is how a page asks whether a newer release is out, rather than waiting
// for the server's own look every half hour. A look made for a page is at most one
// a minute: what it finds is told to every open page, and a page left open asks
// again on its own.
func (s *Server) updateLook(w http.ResponseWriter, r *http.Request) {
	mode := readCached
	if now := time.Now().UnixNano(); now-s.lookedAt.Load() > int64(time.Minute) {
		s.lookedAt.Store(now)
		mode = lookNow
	}
	ctx, cancel := s.ctx(r)
	defer cancel()
	v, _ := s.b.Update(ctx, mode) // a failed look still carries what the earlier ones found
	if mode == lookNow {
		s.hub.publish("update", event{Type: "update", Data: v})
	}
	reply(w, v)
}

// catalogPlan says what installing a path of the catalog would set up. It only
// reads: the install itself is an operation.
func (s *Server) catalogPlan(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		fail(w, http.StatusBadRequest, "the catalog path is required")
		return
	}
	ctx, cancel := s.ctx(r)
	defer cancel()
	v, err := s.b.Plan(ctx, path, r.URL.Query()["exclude"])
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	reply(w, v)
}

func (s *Server) fleet(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()
	v, err := s.b.Fleet(ctx, r.URL.Query().Get("multiverse"))
	if err != nil {
		fail(w, http.StatusNotFound, err.Error())
		return
	}
	reply(w, v)
}

func (s *Server) multiverse(w http.ResponseWriter, r *http.Request) {
	name := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/multiverses/"), "/")
	if name == "" || strings.Contains(name, "/") {
		fail(w, http.StatusNotFound, "no such multiverse")
		return
	}
	ctx, cancel := s.ctx(r)
	defer cancel()
	v, err := s.b.Multiverse(ctx, name)
	if err != nil {
		fail(w, http.StatusNotFound, err.Error())
		return
	}
	reply(w, v)
}

func (s *Server) doctor(w http.ResponseWriter, r *http.Request) {
	ws := r.URL.Query().Get("multiverse")
	if ws == "" {
		fail(w, http.StatusBadRequest, "multiverse is required")
		return
	}
	ctx, cancel := s.ctx(r)
	defer cancel()
	v, err := s.b.Doctor(ctx, ws, r.URL.Query().Get("universe"))
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	reply(w, v)
}

// projectRepo is where the project is; a release is a page of it.
const projectRepo = "https://github.com/cristiansteib/multiversectl"

func releaseURL(version string) string {
	return projectRepo + "/releases/tag/v" + strings.TrimPrefix(version, "v")
}

func (s *Server) system(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()
	v, err := s.b.System(ctx)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	v.Server = versionInfo{Version: s.o.Version, Commit: s.o.Commit, Date: s.o.Date}
	v.Links = systemLinks{Repo: projectRepo, Releases: projectRepo + "/releases", Issues: projectRepo + "/issues"}
	if app.Released(s.o.Version) {
		v.Server.URL = releaseURL(s.o.Version)
	}
	if v.Latest.Version != "" {
		v.Latest.URL = releaseURL(v.Latest.Version)
	}
	v.Listen, v.Started, v.ReadOnly = s.listenAddr(r), s.started.UTC().Format(time.RFC3339), s.o.ReadOnly
	reply(w, v)
}

func (s *Server) next(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()
	v, err := s.b.NextFree(ctx, r.URL.Query().Get("multiverse"))
	if err != nil {
		fail(w, http.StatusNotFound, err.Error())
		return
	}
	reply(w, v)
}

func (s *Server) branches(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()
	v, err := s.b.Branches(ctx, r.URL.Query().Get("multiverse"))
	if err != nil {
		fail(w, http.StatusNotFound, err.Error())
		return
	}
	reply(w, map[string]any{"branches": v})
}

func (s *Server) listenAddr(r *http.Request) string { return r.Host }

func reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

// replyWith is reply with a status of its own. WriteHeader freezes the header
// map, so the content type has to be set before it and not by reply after.
func replyWith(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, msg string) {
	failWith(w, code, map[string]any{"error": msg})
}

func failWith(w http.ResponseWriter, code int, body map[string]any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

// Serve listens until ctx ends. ready is told the address once it is bound,
// with the token in it, so a port of 0 can be reported as the one the system
// chose and the link it prints is one that opens.
func Serve(ctx context.Context, b Backend, o Options, ready func(url string)) error {
	if !o.Expose {
		host, _, err := net.SplitHostPort(o.Addr)
		if err != nil {
			return fmt.Errorf("--addr %q: %w", o.Addr, err)
		}
		if host != "" && !localHost(host) {
			return fmt.Errorf("--addr %s is not a loopback address; reaching the server from the network needs --expose", o.Addr)
		}
		if host == "" {
			return fmt.Errorf("--addr %s listens on every interface; use 127.0.0.1:<port>, or --expose if you mean it", o.Addr)
		}
	}
	ln, err := listenNear(o.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", o.Addr, err)
	}
	// What it is actually listening on, which is not what was asked for when
	// the port was 0: the server has to be able to say where to reach it.
	o.Addr = ln.Addr().String()
	srv := New(b, o)
	defer srv.Close()
	hs := &http.Server{
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	if ready != nil {
		ready("http://" + ln.Addr().String() + "/?" + tokenParam + "=" + srv.Token())
	}
	done := make(chan error, 1)
	go func() { done <- hs.Serve(ln) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		// Stop what runs behind the handler first, then ask the HTTP server to
		// finish what is in flight, and close what is still open after a moment:
		// a page left connected must not make Ctrl+C wait.
		srv.Close()
		shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := hs.Shutdown(shutdown); err != nil {
			_ = hs.Close()
			if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
				return err
			}
		}
		return nil
	}
}

// listenNear listens on addr, and when its port is taken by another process
// tries the next ones, then any free port: a person who just wants the page
// should not have to hunt for a port. The address the server ends up on is the
// listener's, and it is reported from there.
func listenNear(addr string) (net.Listener, error) {
	ln, err := net.Listen("tcp", addr)
	if err == nil || !errors.Is(err, syscall.EADDRINUSE) {
		return ln, err
	}
	host, port, splitErr := net.SplitHostPort(addr)
	n, convErr := strconv.Atoi(port)
	if splitErr != nil || convErr != nil || n == 0 {
		return nil, err
	}
	for next := n + 1; next <= n+20 && next <= 65535; next++ {
		if ln, e := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(next))); e == nil {
			fmt.Fprintf(os.Stderr, "port %d is in use; listening on %d\n", n, next)
			return ln, nil
		}
	}
	if ln, e := net.Listen("tcp", net.JoinHostPort(host, "0")); e == nil {
		fmt.Fprintf(os.Stderr, "port %d is in use; listening on %s\n", n, ln.Addr())
		return ln, nil
	}
	return nil, err
}
