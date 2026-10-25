package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cristiansteib/multiversectl/internal/app"
)

type fake struct {
	status  app.StatusReport
	catalog catalogView
	fleet   fleetView
	ws      workspaceView
	doctor  doctorView
	err     error
	asked   []string
}

func (f *fake) Status(_ context.Context, fetch bool) (app.StatusReport, error) {
	f.asked = append(f.asked, "status")
	if fetch {
		f.asked = append(f.asked, "fetch")
	}
	return f.status, f.err
}
func (f *fake) Catalog(context.Context, bool) (catalogView, error) { return f.catalog, f.err }
func (f *fake) Fleet(_ context.Context, ws string) (fleetView, error) {
	f.asked = append(f.asked, "fleet:"+ws)
	return f.fleet, f.err
}
func (f *fake) Workspace(_ context.Context, name string) (workspaceView, error) {
	f.asked = append(f.asked, "workspace:"+name)
	return f.ws, f.err
}
func (f *fake) Doctor(_ context.Context, ws, u string) (doctorView, error) {
	f.asked = append(f.asked, "doctor:"+ws+":"+u)
	return f.doctor, f.err
}

func get(t *testing.T, h http.Handler, method, target string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	req.Host = "localhost:7777"
	for k, v := range headers {
		if k == "Host" {
			req.Host = v
			continue
		}
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestEndpointsReturnTheBackendsViews(t *testing.T) {
	f := &fake{
		status:  app.StatusReport{Next: "multiversectl update acme", Workspaces: []app.WorkspaceStatus{{Name: "acme", Stale: true}}},
		catalog: catalogView{Root: nodeView{Name: "me", Kind: "catalog"}, Installs: 1},
		fleet:   fleetView{Workspaces: []fleetWorkspace{{Name: "acme", Universes: []universeView{{Name: "1"}}}}},
		ws:      workspaceView{Name: "acme"},
		doctor:  doctorView{OK: true, Checks: []checkView{{Name: "git", Status: "ok"}}},
	}
	h := Handler(f, "v9", false)
	for _, tt := range []struct{ path, want string }{
		{"/api/overview", `"next":"multiversectl update acme"`},
		{"/api/overview", `"version":"v9"`},
		{"/api/catalog", `"installed":1`},
		{"/api/fleet", `"name":"1"`},
		{"/api/workspaces/acme", `"name":"acme"`},
		{"/api/doctor?workspace=acme&universe=1", `"ok":true`},
	} {
		rec := get(t, h, "GET", tt.path, nil)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), tt.want) {
			t.Errorf("%s: %d %s, want %s", tt.path, rec.Code, rec.Body, tt.want)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Errorf("%s: content type %q", tt.path, ct)
		}
	}
	get(t, h, "GET", "/api/overview?fetch=1", nil)
	get(t, h, "GET", "/api/fleet?workspace=acme", nil)
	joined := strings.Join(f.asked, " ")
	for _, want := range []string{"fetch", "fleet:acme", "workspace:acme", "doctor:acme:1"} {
		if !strings.Contains(joined, want) {
			t.Errorf("backend was not asked %q: %s", want, joined)
		}
	}
}

func TestItIsReadOnly(t *testing.T) {
	h := Handler(&fake{}, "v", false)
	for _, m := range []string{"POST", "PUT", "DELETE", "PATCH"} {
		rec := get(t, h, m, "/api/overview", nil)
		if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") == "" {
			t.Errorf("%s: %d", m, rec.Code)
		}
	}
}

func TestOnlyThisMachineMayAskUnlessExposed(t *testing.T) {
	closed, open := Handler(&fake{}, "v", false), Handler(&fake{}, "v", true)
	for _, host := range []string{"localhost:7777", "127.0.0.1:7777", "[::1]:7777", "LOCALHOST"} {
		if rec := get(t, closed, "GET", "/api/overview", map[string]string{"Host": host}); rec.Code != 200 {
			t.Errorf("%s refused: %d", host, rec.Code)
		}
	}
	// A page elsewhere points a name it controls at 127.0.0.1: the Host says so.
	for _, host := range []string{"evil.example.test:7777", "192.168.1.5:7777", "localhost.evil.example.test"} {
		if rec := get(t, closed, "GET", "/api/overview", map[string]string{"Host": host}); rec.Code != http.StatusForbidden {
			t.Errorf("%s answered: %d", host, rec.Code)
		}
		if rec := get(t, open, "GET", "/api/overview", map[string]string{"Host": host}); rec.Code != 200 {
			t.Errorf("--expose should answer %s: %d", host, rec.Code)
		}
	}
	if rec := get(t, closed, "GET", "/api/overview", map[string]string{"Sec-Fetch-Site": "cross-site"}); rec.Code != http.StatusForbidden {
		t.Errorf("cross-site: %d", rec.Code)
	}
	if rec := get(t, closed, "GET", "/api/overview", map[string]string{"Sec-Fetch-Site": "same-origin"}); rec.Code != 200 {
		t.Errorf("same-origin: %d", rec.Code)
	}
}

func TestEveryResponseCarriesTheSecurityHeaders(t *testing.T) {
	h := Handler(&fake{}, "v", false)
	for _, path := range []string{"/", "/api/overview", "/app.js", "/nope"} {
		rec := get(t, h, "GET", path, nil)
		hd := rec.Header()
		// http.FileServer removes Cache-Control from its own 404s; nothing is cached there.
		noStore := hd.Get("Cache-Control") == "no-store" || path == "/nope"
		if !noStore || hd.Get("X-Content-Type-Options") != "nosniff" ||
			hd.Get("X-Frame-Options") != "DENY" || !strings.Contains(hd.Get("Content-Security-Policy"), "default-src 'self'") {
			t.Errorf("%s: headers %v", path, hd)
		}
	}
}

func TestThePageAndItsAssetsAreServed(t *testing.T) {
	h := Handler(&fake{}, "v", false)
	if rec := get(t, h, "GET", "/", nil); rec.Code != 200 || !strings.Contains(rec.Body.String(), "multiversectl") || !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
		t.Errorf("/: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	for _, p := range []string{"/app.js", "/style.css"} {
		if rec := get(t, h, "GET", p, nil); rec.Code != 200 || rec.Body.Len() == 0 {
			t.Errorf("%s: %d", p, rec.Code)
		}
	}
	if rec := get(t, h, "GET", "/etc/passwd", nil); rec.Code != 404 {
		t.Errorf("unknown file: %d", rec.Code)
	}
	// Nothing in the page loads from outside, which is what the CSP enforces.
	page := get(t, h, "GET", "/", nil).Body.String() + get(t, h, "GET", "/app.js", nil).Body.String()
	for _, banned := range []string{"https://", "http://cdn", "innerHTML", "eval("} {
		if strings.Contains(strings.ReplaceAll(page, "http://www.w3.org/2000/svg", ""), banned) {
			t.Errorf("the page contains %q", banned)
		}
	}
}

func TestErrorsAreJSONWithTheRightStatus(t *testing.T) {
	h := Handler(&fake{err: errors.New("no workspace \"x\"")}, "v", false)
	for path, code := range map[string]int{
		"/api/fleet?workspace=x": 404, "/api/workspaces/x": 404, "/api/overview": 500, "/api/doctor": 400, "/api/nope": 404, "/api/workspaces/": 404, "/api/workspaces/a/b": 404,
	} {
		rec := get(t, h, "GET", path, nil)
		var body map[string]string
		if rec.Code != code || json.Unmarshal(rec.Body.Bytes(), &body) != nil || body["error"] == "" {
			t.Errorf("%s: %d %s, want %d with an error", path, rec.Code, rec.Body, code)
		}
	}
}

func TestServeRefusesWhatWouldExposeItself(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:0", ":0", "192.168.1.5:0"} {
		err := Serve(context.Background(), &fake{}, Options{Addr: addr}, nil)
		if err == nil || !strings.Contains(err.Error(), "--expose") {
			t.Errorf("%s: err = %v", addr, err)
		}
	}
	if err := Serve(context.Background(), &fake{}, Options{Addr: "nonsense"}, nil); err == nil {
		t.Error("a malformed address must fail")
	}
}

func TestServeListensAndStopsWithItsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	urls := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, &fake{status: app.StatusReport{Next: "x"}}, Options{Addr: "127.0.0.1:0", Version: "v"}, func(u string) { urls <- u })
	}()
	var url string
	select {
	case url = <-urls:
	case <-time.After(3 * time.Second):
		t.Fatal("never became ready")
	}
	res, err := http.Get(url + "/api/overview")
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("get: %v %v", res, err)
	}
	res.Body.Close()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("did not stop")
	}
}

func TestUniverseViewKeepsWhatThePageNeeds(t *testing.T) {
	created := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	v := toUniverse(app.Summary{
		Name: "3", Dir: "/w/universes/3", Running: 2, Created: created, Note: "PR 412",
		Apps: []app.AppSummary{{Key: "api", Repo: "api", Branch: "u3", Running: 2, Expected: 3,
			URLs:     []app.HostURL{{Host: "api", Service: "web", URL: "http://api-u3.example.test"}},
			Services: []app.ServiceStatus{{Name: "web", State: app.StateRunning, Detail: "Up 2 hours"}, {Name: "worker", State: app.StateStopped}}}},
		Checkouts: []app.Checkout{{Repo: "api", Branch: "u3", Path: "/w/universes/3/api", App: "api"}},
	})
	if v.Created != "2026-10-01T12:00:00Z" || v.LastStarted != "" {
		t.Errorf("times: %q %q", v.Created, v.LastStarted)
	}
	if len(v.Apps) != 1 || v.Apps[0].Expected != 3 || len(v.Apps[0].Services) != 2 || v.Apps[0].URLs[0].URL != "http://api-u3.example.test" || len(v.Checkouts) != 1 {
		t.Errorf("view = %+v", v)
	}
	if b, _ := json.Marshal(toUniverse(app.Summary{Name: "x"})); !strings.Contains(string(b), `"apps":[]`) || !strings.Contains(string(b), `"checkouts":[]`) {
		t.Errorf("empty lists must be [] for the page, not null: %s", b)
	}
}
