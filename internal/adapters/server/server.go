// Package server is a read-only web view of this machine's setup: the root
// catalog and its tree, the workspaces installed here and how they have
// drifted, and the fleet of universes each one is running. It serves one
// embedded page and a small JSON API over the same use cases the command line
// runs, and changes nothing.
package server

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/cristiansteib/multiversectl/internal/app"
)

//go:embed ui
var uiFiles embed.FS

// requestTimeout bounds one API call. The slow ones ask the container runtime
// or the network, and a stuck one should not hold a connection open for ever.
const requestTimeout = 45 * time.Second

// Handler returns the page and the API. Unless open is set it answers only
// requests addressed to this machine by name or loopback address: the server
// has no login, and without that check a web page elsewhere could point a
// hostname it controls at 127.0.0.1 and read the setup through the browser.
func Handler(b Backend, version string, open bool) http.Handler {
	s := &srv{b: b, version: version}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/overview", s.overview)
	mux.HandleFunc("/api/catalog", s.catalog)
	mux.HandleFunc("/api/fleet", s.fleet)
	mux.HandleFunc("/api/workspaces/", s.workspace)
	mux.HandleFunc("/api/doctor", s.doctor)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { fail(w, http.StatusNotFound, "no such endpoint") })

	ui, _ := fs.Sub(uiFiles, "ui")
	mux.Handle("/", http.FileServerFS(ui))
	return guard(mux, open)
}

// guard applies what every response needs: read-only methods, no framing, no
// outside origins, nothing cached (the data is live) and the host check.
func guard(next http.Handler, open bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			fail(w, http.StatusMethodNotAllowed, "this server is read-only")
			return
		}
		if !open && !localHost(r.Host) {
			fail(w, http.StatusForbidden, "this server answers only on localhost; start it with --expose to reach it by another name")
			return
		}
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			fail(w, http.StatusForbidden, "cross-site requests are refused")
			return
		}
		h := w.Header()
		h.Set("Cache-Control", "no-store")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
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

type srv struct {
	b       Backend
	version string
}

func (s *srv) ctx(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), requestTimeout)
}

func (s *srv) overview(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()
	rep, err := s.b.Status(ctx, r.URL.Query().Get("fetch") == "1")
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	reply(w, struct {
		Version string           `json:"version"`
		Status  app.StatusReport `json:"status"`
	}{s.version, rep})
}

func (s *srv) catalog(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()
	v, err := s.b.Catalog(ctx, r.URL.Query().Get("fetch") == "1")
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	reply(w, v)
}

func (s *srv) fleet(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.ctx(r)
	defer cancel()
	v, err := s.b.Fleet(ctx, r.URL.Query().Get("workspace"))
	if err != nil {
		fail(w, http.StatusNotFound, err.Error())
		return
	}
	reply(w, v)
}

func (s *srv) workspace(w http.ResponseWriter, r *http.Request) {
	name := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/workspaces/"), "/")
	if name == "" || strings.Contains(name, "/") {
		fail(w, http.StatusNotFound, "no such workspace")
		return
	}
	ctx, cancel := s.ctx(r)
	defer cancel()
	v, err := s.b.Workspace(ctx, name)
	if err != nil {
		fail(w, http.StatusNotFound, err.Error())
		return
	}
	reply(w, v)
}

func (s *srv) doctor(w http.ResponseWriter, r *http.Request) {
	ws := r.URL.Query().Get("workspace")
	if ws == "" {
		fail(w, http.StatusBadRequest, "workspace is required")
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

func reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// Options say where to listen.
type Options struct {
	Addr    string
	Version string
	// Expose allows a non-loopback address and any Host header. There is no
	// login, so it is a decision the user makes out loud.
	Expose bool
}

// Serve listens until ctx ends. ready is told the address once it is bound, so
// a port of 0 can be reported as the one the system chose.
func Serve(ctx context.Context, b Backend, o Options, ready func(url string)) error {
	if !o.Expose {
		host, _, err := net.SplitHostPort(o.Addr)
		if err != nil {
			return fmt.Errorf("--addr %q: %w", o.Addr, err)
		}
		if host != "" && !localHost(host) {
			return fmt.Errorf("--addr %s is not a loopback address; the server has no login, so reaching it from the network needs --expose", o.Addr)
		}
		if host == "" {
			return fmt.Errorf("--addr %s listens on every interface; use 127.0.0.1:<port>, or --expose if you mean it", o.Addr)
		}
	}
	ln, err := net.Listen("tcp", o.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", o.Addr, err)
	}
	hs := &http.Server{
		Handler:           Handler(b, o.Version, o.Expose),
		ReadHeaderTimeout: 10 * time.Second,
	}
	if ready != nil {
		ready("http://" + ln.Addr().String())
	}
	done := make(chan error, 1)
	go func() { done <- hs.Serve(ln) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := hs.Shutdown(shutdown); err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
		return nil
	}
}
