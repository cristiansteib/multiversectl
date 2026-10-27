package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/cristiansteib/multiversectl/internal/app"
	"github.com/cristiansteib/multiversectl/internal/domain"
)

const testToken = "test-token"

type fake struct {
	status     app.StatusReport
	catalog    catalogView
	fleet      fleetView
	ws         multiverseView
	doctor     doctorView
	system     systemView
	update     updateView
	looked     bool
	next       nextView
	branchList []branchView
	dirty      []string
	err        error

	mu    sync.Mutex
	asked []string
	did   []opRequest
	// hold, when set, makes Do wait on it, so an operation can be in progress.
	hold chan struct{}
	// doErr is what Do returns.
	doErr error
	// containers, stats and logText answer the side panel; logHold keeps a log
	// stream open until the request ends.
	containers []containerView
	stats      []statView
	fleetStats fleetStatsView
	code       codeView
	files      diffFilesView
	patch      patchView
	logText    string
	logHold    bool
}

func (f *fake) ask(s string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, s)
}
func (f *fake) asks() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.asked, " ")
}

func (f *fake) Status(_ context.Context, fetch bool) (app.StatusReport, error) {
	f.ask("status")
	if fetch {
		f.ask("fetch")
	}
	return f.status, f.err
}
func (f *fake) Catalog(context.Context, bool) (catalogView, error) { return f.catalog, f.err }
func (f *fake) Fleet(_ context.Context, ws string) (fleetView, error) {
	f.ask("fleet:" + ws)
	return f.fleet, f.err
}
func (f *fake) Multiverse(_ context.Context, name string) (multiverseView, error) {
	f.ask("multiverse:" + name)
	return f.ws, f.err
}
func (f *fake) Doctor(_ context.Context, ws, u string) (doctorView, error) {
	f.ask("doctor:" + ws + ":" + u)
	return f.doctor, f.err
}
func (f *fake) Containers(_ context.Context, ws, u string) ([]containerView, error) {
	f.ask("containers:" + ws + ":" + u)
	return f.containers, f.err
}
func (f *fake) Stats(_ context.Context, ws, u string) ([]statView, error) {
	f.ask("stats:" + ws + ":" + u)
	return f.stats, f.err
}
func (f *fake) Code(_ context.Context, ws, u string, fetch bool) (codeView, error) {
	f.ask("code:" + ws + ":" + u + map[bool]string{true: ":fetch", false: ""}[fetch])
	return f.code, f.err
}
func (f *fake) Diff(_ context.Context, ws, u, repo, scope, base string) (diffFilesView, error) {
	f.ask("diff:" + ws + ":" + u + ":" + repo + ":" + scope + ":" + base)
	return f.files, f.err
}
func (f *fake) Patch(_ context.Context, ws, u, repo, scope, base, file string) (patchView, error) {
	f.ask("patch:" + ws + ":" + u + ":" + repo + ":" + scope + ":" + base + ":" + file)
	return f.patch, f.err
}
func (f *fake) FleetStats(_ context.Context, ws string) (fleetStatsView, error) {
	f.ask("fleetstats:" + ws)
	return f.fleetStats, f.err
}
func (f *fake) Logs(ctx context.Context, ws, u, appKey, service string, tail int, follow bool, w io.Writer) error {
	f.ask(fmt.Sprintf("logs:%s:%s:%s/%s:%d:%v", ws, u, appKey, service, tail, follow))
	if f.err != nil {
		return f.err
	}
	_, _ = io.WriteString(w, f.logText)
	if f.logHold {
		<-ctx.Done()
	}
	return nil
}
func (f *fake) System(context.Context) (systemView, error) { return f.system, f.err }
func (f *fake) Update(_ context.Context, mode lookMode) (updateView, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.looked = f.looked || mode == lookNow
	return f.update, nil
}
func (f *fake) NextFree(_ context.Context, ws string) (nextView, error) {
	f.ask("next:" + ws)
	return f.next, f.err
}
func (f *fake) Branches(_ context.Context, ws string) ([]branchView, error) {
	f.ask("branches:" + ws)
	return f.branchList, f.err
}
func (f *fake) Unsaved(context.Context, string, string) ([]string, error) {
	return f.dirty, f.err
}
func (f *fake) Do(ctx context.Context, r opRequest, k Sink) error {
	f.mu.Lock()
	f.did = append(f.did, r)
	hold := f.hold
	f.mu.Unlock()
	k.Info("starting %s", r.Action)
	if r.Action == "new" {
		k.OK("universe 7 ready (acme-u7)") // as the real one reports the number it picked
	}
	_, _ = io.WriteString(k.Stdout(), "compose says hello\nsecond line")
	if hold != nil {
		select {
		case <-hold:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if f.doErr == nil {
		k.OK("done %s", r.Action)
	}
	return f.doErr
}

// serve starts a server and returns it with its handler.
func serve(t *testing.T, f *fake, o Options) (*Server, http.Handler) {
	t.Helper()
	o.Token = testToken
	s := New(f, o)
	t.Cleanup(s.Close)
	return s, s.Handler()
}

// do sends a request signed in with the session cookie unless headers say
// otherwise (a "Cookie" entry replaces it, an empty one removes it).
func do(t *testing.T, h http.Handler, method, target, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Host = "localhost:7777"
	req.AddCookie(&http.Cookie{Name: cookieName, Value: testToken})
	for k, v := range headers {
		switch {
		case k == "Host":
			req.Host = v
		case k == "Cookie" && v == "":
			req.Header.Del("Cookie")
		default:
			req.Header.Set(k, v)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func get(t *testing.T, h http.Handler, target string, headers map[string]string) *httptest.ResponseRecorder {
	return do(t, h, "GET", target, "", headers)
}

var actionHeaders = map[string]string{csrfHeader: csrfValue, "Content-Type": "application/json"}

func post(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	return do(t, h, "POST", "/api/ops", body, actionHeaders)
}

func TestEndpointsReturnTheBackendsViews(t *testing.T) {
	f := &fake{
		status:  app.StatusReport{Next: "multiversectl catalog update acme", Multiverses: []app.MultiverseStatus{{Name: "acme", Stale: true}}},
		catalog: catalogView{Root: nodeView{Name: "me", Kind: "catalog"}, Installs: 1},
		fleet:   fleetView{Multiverses: []fleetMultiverse{{Name: "acme", Universes: []universeView{{Name: "1"}}}}},
		ws:      multiverseView{Name: "acme"},
		doctor:  doctorView{OK: true, Checks: []checkView{{Name: "git", Status: "ok"}}},
	}
	_, h := serve(t, f, Options{Version: "v9"})
	for _, tt := range []struct{ path, want string }{
		{"/api/overview", `"next":"multiversectl catalog update acme"`},
		{"/api/overview", `"version":"v9"`},
		{"/api/catalog", `"installed":1`},
		{"/api/fleet", `"name":"1"`},
		{"/api/multiverses/acme", `"name":"acme"`},
		{"/api/doctor?multiverse=acme&universe=1", `"ok":true`},
	} {
		rec := get(t, h, tt.path, nil)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), tt.want) {
			t.Errorf("%s: %d %s, want %s", tt.path, rec.Code, rec.Body, tt.want)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Errorf("%s: content type %q", tt.path, ct)
		}
	}
	get(t, h, "/api/overview?fetch=1", nil)
	get(t, h, "/api/fleet?multiverse=acme", nil)
	for _, want := range []string{"fetch", "fleet:acme", "multiverse:acme", "doctor:acme:1"} {
		if !strings.Contains(f.asks(), want) {
			t.Errorf("backend was not asked %q: %s", want, f.asks())
		}
	}
}

func TestNothingIsServedWithoutTheToken(t *testing.T) {
	_, h := serve(t, &fake{}, Options{})
	for _, path := range []string{"/", "/app.js", "/api/overview", "/api/fleet", "/ws", "/api/ops"} {
		if rec := get(t, h, path, map[string]string{"Cookie": ""}); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without a cookie: %d", path, rec.Code)
		}
		if rec := get(t, h, path, map[string]string{"Cookie": cookieName + "=wrong"}); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s with the wrong cookie: %d", path, rec.Code)
		}
	}
	// A wrong token in the link gets nothing either, and sets no cookie.
	rec := get(t, h, "/?t=wrong", map[string]string{"Cookie": ""})
	if rec.Code != http.StatusUnauthorized || rec.Header().Get("Set-Cookie") != "" {
		t.Errorf("wrong token: %d %q", rec.Code, rec.Header().Get("Set-Cookie"))
	}
	if body := get(t, h, "/", map[string]string{"Cookie": ""}).Body.String(); !strings.Contains(body, "multiversectl server status") {
		t.Errorf("the refusal should say how to get the link: %s", body)
	}
}

func TestTheTokenIsTradedForACookieAndLeavesTheAddress(t *testing.T) {
	_, h := serve(t, &fake{}, Options{})
	rec := get(t, h, "/?t="+testToken+"&x=1", map[string]string{"Cookie": ""})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/?x=1" || strings.Contains(loc, testToken) {
		t.Errorf("the token must not stay in the address: %q", loc)
	}
	c := rec.Result().Cookies()
	if len(c) != 1 || c[0].Name != cookieName || !c[0].HttpOnly || c[0].SameSite != http.SameSiteStrictMode || c[0].Value != testToken {
		t.Errorf("cookie = %+v", c)
	}
}

func TestActionsNeedTheHeaderTheOriginAndAWritableServer(t *testing.T) {
	f := &fake{}
	_, h := serve(t, f, Options{})
	body := `{"action":"up","multiverse":"acme","universe":"1"}`

	if rec := do(t, h, "POST", "/api/ops", body, map[string]string{"Content-Type": "application/json"}); rec.Code != http.StatusForbidden {
		t.Errorf("without the header: %d", rec.Code)
	}
	evil := map[string]string{csrfHeader: csrfValue, "Origin": "http://evil.example.test"}
	if rec := do(t, h, "POST", "/api/ops", body, evil); rec.Code != http.StatusForbidden {
		t.Errorf("from another origin: %d", rec.Code)
	}
	if rec := do(t, h, "POST", "/api/ops", body, map[string]string{csrfHeader: csrfValue, "Sec-Fetch-Site": "cross-site"}); rec.Code != http.StatusForbidden {
		t.Errorf("cross-site: %d", rec.Code)
	}
	if rec := do(t, h, "POST", "/api/ops", body, map[string]string{csrfHeader: csrfValue, "Origin": "http://localhost:7777"}); rec.Code != http.StatusAccepted {
		t.Errorf("from the page itself: %d %s", rec.Code, rec.Body)
	}
	for _, m := range []string{"PUT", "DELETE", "PATCH"} {
		if rec := do(t, h, m, "/api/overview", "", actionHeaders); rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: %d", m, rec.Code)
		}
	}

	_, ro := serve(t, &fake{}, Options{ReadOnly: true})
	if rec := post(t, ro, body); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "read-only") {
		t.Errorf("read-only server: %d %s", rec.Code, rec.Body)
	}
	if rec := get(t, ro, "/api/overview", nil); rec.Code != 200 {
		t.Errorf("read-only still reads: %d", rec.Code)
	}
}

// guard puts GET and HEAD together as reads, and applies the read-only refusal
// and the CSRF header to POST alone. A HEAD carrying a body therefore reached
// the ops endpoint having passed neither, and it started the operation.
func TestOnlyAPostStartsAnOperation(t *testing.T) {
	const body = `{"action":"rm","multiverse":"acme","universe":"1","confirm":"1","force":true}`
	for _, method := range []string{"HEAD", "GET"} {
		f := &fake{}
		_, ro := serve(t, f, Options{ReadOnly: true})
		if rec := do(t, ro, method, "/api/ops", body, nil); rec.Code != 200 {
			t.Errorf("%s on a read-only server: %d %s", method, rec.Code, rec.Body)
		}
		f.mu.Lock()
		did := len(f.did)
		f.mu.Unlock()
		if did != 0 {
			t.Errorf("%s started %d operations on a read-only server", method, did)
		}

		// Not read-only either: without the CSRF header nothing may start.
		g := &fake{}
		_, rw := serve(t, g, Options{})
		if rec := do(t, rw, method, "/api/ops", body, nil); rec.Code != 200 {
			t.Errorf("%s: %d %s", method, rec.Code, rec.Body)
		}
		g.mu.Lock()
		did = len(g.did)
		g.mu.Unlock()
		if did != 0 {
			t.Errorf("%s without %s started %d operations", method, csrfHeader, did)
		}
	}
}

// WriteHeader freezes the header map, so a 202 has to carry its content type
// before it and not from reply afterwards. Over a real connection, not a
// recorder: a ResponseRecorder hands back the live map and cannot show it.
func TestAnAcceptedOperationSaysItIsJSON(t *testing.T) {
	s, _ := serve(t, &fake{}, Options{})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	req, err := http.NewRequest("POST", ts.URL+"/api/ops", strings.NewReader(`{"action":"up","multiverse":"acme","universe":"1"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: cookieName, Value: testToken})
	req.Header.Set(csrfHeader, csrfValue)
	req.Header.Set("Content-Type", "application/json")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("%d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Errorf("Content-Type %q; nosniff then tells the client to read JSON as text", got)
	}
}

func TestOnlyThisMachineMayAskUnlessExposed(t *testing.T) {
	_, closed := serve(t, &fake{}, Options{})
	_, open := serve(t, &fake{}, Options{Expose: true})
	for _, host := range []string{"localhost:7777", "127.0.0.1:7777", "[::1]:7777", "LOCALHOST"} {
		if rec := get(t, closed, "/api/overview", map[string]string{"Host": host}); rec.Code != 200 {
			t.Errorf("%s refused: %d", host, rec.Code)
		}
	}
	// A page elsewhere points a name it controls at 127.0.0.1: the Host says so.
	for _, host := range []string{"evil.example.test:7777", "192.168.1.5:7777", "localhost.evil.example.test"} {
		if rec := get(t, closed, "/api/overview", map[string]string{"Host": host}); rec.Code != http.StatusForbidden {
			t.Errorf("%s answered: %d", host, rec.Code)
		}
		if rec := get(t, open, "/api/overview", map[string]string{"Host": host}); rec.Code != 200 {
			t.Errorf("--expose should answer %s: %d", host, rec.Code)
		}
	}
	if rec := get(t, closed, "/api/overview", map[string]string{"Sec-Fetch-Site": "cross-site"}); rec.Code != http.StatusForbidden {
		t.Errorf("cross-site: %d", rec.Code)
	}
	if rec := get(t, closed, "/api/overview", map[string]string{"Sec-Fetch-Site": "same-origin"}); rec.Code != 200 {
		t.Errorf("same-origin: %d", rec.Code)
	}
	if rec := get(t, open, "/api/overview", map[string]string{"Host": "evil.example.test", "Cookie": ""}); rec.Code != http.StatusUnauthorized {
		t.Errorf("--expose still needs the token: %d", rec.Code)
	}
}

func TestEveryResponseCarriesTheSecurityHeaders(t *testing.T) {
	_, h := serve(t, &fake{}, Options{})
	for _, path := range []string{"/", "/api/overview", "/app.js", "/nope"} {
		rec := get(t, h, path, nil)
		hd := rec.Header()
		// http.FileServer removes Cache-Control from its own 404s; nothing is cached there.
		noStore := hd.Get("Cache-Control") == "no-store" || path == "/nope"
		csp := hd.Get("Content-Security-Policy")
		if !noStore || hd.Get("X-Content-Type-Options") != "nosniff" || hd.Get("X-Frame-Options") != "DENY" ||
			!strings.Contains(csp, "default-src 'self'") || !strings.Contains(csp, "connect-src 'self'") {
			t.Errorf("%s: headers %v", path, hd)
		}
	}
	if rec := get(t, h, "/", map[string]string{"Cookie": ""}); rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Error("a refusal carries the headers too")
	}
}

func TestThePageAndItsAssetsAreServed(t *testing.T) {
	_, h := serve(t, &fake{}, Options{})
	if rec := get(t, h, "/", nil); rec.Code != 200 || !strings.Contains(rec.Body.String(), "multiversectl") || !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
		t.Errorf("/: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	for _, p := range []string{"/app.js", "/style.css"} {
		if rec := get(t, h, p, nil); rec.Code != 200 || rec.Body.Len() == 0 {
			t.Errorf("%s: %d", p, rec.Code)
		}
	}
	if rec := get(t, h, "/etc/passwd", nil); rec.Code != 404 {
		t.Errorf("unknown file: %d", rec.Code)
	}
	// Nothing in the page loads from outside, which is what the CSP enforces, and
	// nothing is built from strings the data could turn into markup.
	page := get(t, h, "/", nil).Body.String() + get(t, h, "/app.js", nil).Body.String()
	// The CSP forbids inline styles, so a style set from script is silently dropped.
	for _, banned := range []string{"https://", "http://cdn", "style: '", "innerHTML", "outerHTML", "insertAdjacentHTML", "eval(", "document.write"} {
		if strings.Contains(strings.ReplaceAll(page, "http://www.w3.org/2000/svg", ""), banned) {
			t.Errorf("the page contains %q", banned)
		}
	}
}

func TestErrorsAreJSONWithTheRightStatus(t *testing.T) {
	_, h := serve(t, &fake{err: errors.New("no project \"x\"")}, Options{})
	for path, code := range map[string]int{
		"/api/fleet?multiverse=x": 404, "/api/multiverses/x": 404, "/api/overview": 500, "/api/doctor": 400, "/api/nope": 404, "/api/multiverses/": 404, "/api/multiverses/a/b": 404, "/api/system": 500,
	} {
		rec := get(t, h, path, nil)
		var body map[string]any
		if rec.Code != code || json.Unmarshal(rec.Body.Bytes(), &body) != nil || body["error"] == "" {
			t.Errorf("%s: %d %s, want %d with an error", path, rec.Code, rec.Body, code)
		}
	}
}

func TestTheSystemViewHasTheVersions(t *testing.T) {
	f := &fake{system: systemView{
		Tools:  []toolVersion{{Name: "git", Version: "2.43.0", OK: true}, {Name: "glab", Note: "not installed"}},
		Latest: latestView{Version: "v1.2.0", Newer: true},
		Format: "multiverse/v1alpha1",
	}}
	_, h := serve(t, f, Options{Version: "v1.1.0", Commit: "abc1234", Date: "2026-10-25"})
	var got systemView
	rec := get(t, h, "/api/system", nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != 200 {
		t.Fatalf("%d %s %v", rec.Code, rec.Body, err)
	}
	if got.Server.Version != "v1.1.0" || got.Server.Commit != "abc1234" || len(got.Tools) != 2 || !got.Latest.Newer || got.Format == "" || got.Started == "" {
		t.Errorf("system = %+v", got)
	}
}

// operations

func wait(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(3 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

func listOps(t *testing.T, h http.Handler) []opView {
	t.Helper()
	var body struct {
		Ops []opView `json:"ops"`
	}
	if err := json.Unmarshal(get(t, h, "/api/ops", nil).Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Ops
}

func TestAnActionRunsInTheBackgroundAndKeepsItsOutput(t *testing.T) {
	f := &fake{}
	_, h := serve(t, f, Options{})
	rec := post(t, h, `{"action":"up","multiverse":"acme","universe":"3"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	wait(t, "the operation to finish", func() bool { o := listOps(t, h); return len(o) == 1 && o[0].State == "done" })
	o := listOps(t, h)[0]
	var text []string
	for _, l := range o.Lines {
		text = append(text, l.Kind+":"+l.Text)
	}
	joined := strings.Join(text, "|")
	for _, want := range []string{"info:starting up", "out:compose says hello", "out:second line", "ok:done up"} {
		if !strings.Contains(joined, want) {
			t.Errorf("output lacks %q: %s", want, joined)
		}
	}
	if o.Title != "Start 3 in acme" || o.Ended == "" {
		t.Errorf("op = %+v", o)
	}
	if len(f.did) != 1 || f.did[0].Universe != "3" {
		t.Errorf("did %+v", f.did)
	}
}

func TestAFailedActionSaysWhy(t *testing.T) {
	_, h := serve(t, &fake{doErr: errors.New("port is already allocated")}, Options{})
	post(t, h, `{"action":"down","multiverse":"acme","universe":"1"}`)
	wait(t, "failure", func() bool { o := listOps(t, h); return len(o) == 1 && o[0].State == "failed" })
	if o := listOps(t, h)[0]; o.Error != "port is already allocated" {
		t.Errorf("error = %q", o.Error)
	}
}

func TestRemovingNeedsTheNameTypedAndRefusesUncommittedWork(t *testing.T) {
	f := &fake{dirty: []string{"api", "web"}}
	_, h := serve(t, f, Options{})

	if rec := post(t, h, `{"action":"rm","multiverse":"acme","universe":"3"}`); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "type the universe's name") {
		t.Errorf("no confirmation: %d %s", rec.Code, rec.Body)
	}
	if rec := post(t, h, `{"action":"rm","multiverse":"acme","universe":"3","confirm":"4"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("wrong confirmation: %d", rec.Code)
	}
	rec := post(t, h, `{"action":"rm","multiverse":"acme","universe":"3","confirm":"3"}`)
	var body struct {
		Error string   `json:"error"`
		Dirty []string `json:"dirty"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusConflict || len(body.Dirty) != 2 || !strings.Contains(body.Error, "uncommitted work in api, web") {
		t.Errorf("uncommitted work: %d %s", rec.Code, rec.Body)
	}
	if len(f.did) != 0 {
		t.Error("nothing may run before the person has agreed")
	}
	if rec := post(t, h, `{"action":"rm","multiverse":"acme","universe":"3","confirm":"3","force":true}`); rec.Code != http.StatusAccepted {
		t.Errorf("forced: %d %s", rec.Code, rec.Body)
	}
}

func TestUniversesRunSideBySideButAUniverseHasOneActionAtATime(t *testing.T) {
	f := &fake{hold: make(chan struct{})}
	_, h := serve(t, f, Options{})
	if rec := post(t, h, `{"action":"up","multiverse":"acme","universe":"1"}`); rec.Code != http.StatusAccepted {
		t.Fatalf("first: %d", rec.Code)
	}
	// Another universe of the same project does not wait for it.
	if rec := post(t, h, `{"action":"up","multiverse":"acme","universe":"2"}`); rec.Code != http.StatusAccepted {
		t.Errorf("a second universe in the same project: %d %s", rec.Code, rec.Body)
	}
	// The same universe does.
	if rec := post(t, h, `{"action":"down","multiverse":"acme","universe":"1"}`); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "another operation") {
		t.Errorf("the same universe twice: %d %s", rec.Code, rec.Body)
	}
	// Creating takes the next number, so creations take turns; with nothing creating, it starts.
	if rec := post(t, h, `{"action":"new","multiverse":"acme"}`); rec.Code != http.StatusAccepted {
		t.Errorf("creating while others start: %d %s", rec.Code, rec.Body)
	}
	if rec := post(t, h, `{"action":"new","multiverse":"acme","name":"fix/login"}`); rec.Code != http.StatusConflict {
		t.Errorf("two creations at once: %d %s", rec.Code, rec.Body)
	}
	// Changing the project waits for everything in it.
	if rec := post(t, h, `{"action":"update","multiverse":"acme"}`); rec.Code != http.StatusConflict {
		t.Errorf("an update while universes are busy: %d %s", rec.Code, rec.Body)
	}
	// Another project is free.
	if rec := post(t, h, `{"action":"up","multiverse":"other","universe":"2"}`); rec.Code != http.StatusAccepted {
		t.Errorf("another project is free: %d", rec.Code)
	}
	close(f.hold)
	wait(t, "everything to finish", func() bool {
		for _, o := range listOps(t, h) {
			if o.State == "running" {
				return false
			}
		}
		return true
	})
	if rec := post(t, h, `{"action":"update","multiverse":"acme"}`); rec.Code != http.StatusAccepted {
		t.Errorf("an update when nothing runs: %d %s", rec.Code, rec.Body)
	}
	// And while it runs, nothing else in the project may start.
	f2 := &fake{hold: make(chan struct{})}
	_, h2 := serve(t, f2, Options{})
	if rec := post(t, h2, `{"action":"update","multiverse":"acme"}`); rec.Code != http.StatusAccepted {
		t.Fatalf("update: %d", rec.Code)
	}
	if rec := post(t, h2, `{"action":"up","multiverse":"acme","universe":"1"}`); rec.Code != http.StatusConflict {
		t.Errorf("a start during an update: %d %s", rec.Code, rec.Body)
	}
	close(f2.hold)
}

func TestBadActionsAreRefusedBeforeAnythingRuns(t *testing.T) {
	f := &fake{}
	_, h := serve(t, f, Options{})
	for body, want := range map[string]string{
		`{"action":"format-disk","multiverse":"acme"}`: "unknown action",
		`{"action":"up"}`:                           "multiverse is required",
		`{"action":"up","multiverse":"acme"}`:       "universe is required",
		`{"action":"checkout","multiverse":"acme"}`: "branch to check out is required",
		`not json`: "not valid JSON",
	} {
		rec := post(t, h, body)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), want) {
			t.Errorf("%s: %d %s, want %q", body, rec.Code, rec.Body, want)
		}
	}
	if len(f.did) != 0 {
		t.Errorf("ran %+v", f.did)
	}
}

// websocket

func dial(t *testing.T, ts *httptest.Server, headers http.Header) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+"/ws", &websocket.DialOptions{HTTPHeader: headers})
}

func signedIn() http.Header {
	h := http.Header{}
	h.Add("Cookie", cookieName+"="+testToken)
	return h
}

func read(t *testing.T, c *websocket.Conn) event {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var raw struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	return event{Type: raw.Type, Data: raw.Data}
}

func TestTheWebsocketTellsAPageWhereThingsStandAndWhatChanges(t *testing.T) {
	f := &fake{fleet: fleetView{Multiverses: []fleetMultiverse{{Name: "acme"}}}, status: app.StatusReport{Next: "x"}}
	s, _ := serve(t, f, Options{Version: "v7", Commit: "c0ffee"})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	c, _, err := dial(t, ts, signedIn())
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()

	hello := read(t, c)
	var h helloView
	if err := json.Unmarshal(hello.Data.(json.RawMessage), &h); err != nil || hello.Type != "hello" || h.Version != "v7" || h.Commit != "c0ffee" {
		t.Fatalf("hello = %+v %v", hello, err)
	}
	seen := map[string]bool{}
	for !seen["fleet"] || !seen["overview"] {
		seen[read(t, c).Type] = true
	}
	if !seen["fleet"] || !seen["overview"] {
		t.Errorf("a new page should be told the fleet and the overview: %v", seen)
	}

	// An action's progress arrives as it happens, without asking for it.
	rec := do(t, ts.Config.Handler, "POST", "/api/ops", `{"action":"up","multiverse":"acme","universe":"1"}`, actionHeaders)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	types := map[string]bool{}
	for done := false; !done; {
		e := read(t, c)
		types[e.Type] = true
		if e.Type == "op" {
			var o opView
			_ = json.Unmarshal(e.Data.(json.RawMessage), &o)
			done = o.State == "done"
		}
	}
	if !types["opline"] || !types["op"] {
		t.Errorf("events seen: %v", types)
	}
}

func TestChangesAreOnlyPushedWhenThereIsSomethingNew(t *testing.T) {
	f := &fake{fleet: fleetView{Multiverses: []fleetMultiverse{{Name: "acme"}}}}
	s, _ := serve(t, f, Options{})
	c1 := &client{send: make(chan []byte, 8), drop: func() {}}
	s.hub.join(c1)
	// The release check runs on its own and tells the same clients.
	fleets := func() (n int) {
		for {
			select {
			case d := <-c1.send:
				if strings.Contains(string(d), `"type":"fleet"`) {
					n++
				}
			default:
				return n
			}
		}
	}
	fleets()
	s.look1(context.Background(), true, false)
	s.look1(context.Background(), true, false)
	s.look1(context.Background(), true, false)
	if events := fleets(); events != 1 {
		t.Errorf("an unchanged fleet was sent %d times, want once", events)
	}
	f.mu.Lock()
	f.fleet = fleetView{Multiverses: []fleetMultiverse{{Name: "acme", Universes: []universeView{{Name: "1"}}}}}
	f.mu.Unlock()
	s.look1(context.Background(), true, false)
	if n := fleets(); n != 1 {
		t.Errorf("a changed fleet should be sent: %d queued", n)
	}
}

func TestTheWebsocketRefusesStrangers(t *testing.T) {
	s, _ := serve(t, &fake{}, Options{})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	if _, resp, err := dial(t, ts, nil); err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("without the cookie: %v %v", resp, err)
	}
	foreign := signedIn()
	foreign.Set("Origin", "http://evil.example.test")
	if _, resp, err := dial(t, ts, foreign); err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Errorf("from another site: %v %v", resp, err)
	}
}

func TestServeListensWithATokenAndStopsWithItsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	urls := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, &fake{status: app.StatusReport{Next: "x"}}, Options{Addr: "127.0.0.1:0", Version: "v"}, func(u string) { urls <- u })
	}()
	var link string
	select {
	case link = <-urls:
	case <-time.After(3 * time.Second):
		t.Fatal("never became ready")
	}
	u, err := url.Parse(link)
	if err != nil || u.Query().Get(tokenParam) == "" {
		t.Fatalf("the link must carry the token: %q", link)
	}
	// Without it nothing answers; following the link signs a browser in.
	plain, err := http.Get("http://" + u.Host + "/api/overview")
	if err != nil || plain.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token: %v %v", plain, err)
	}
	plain.Body.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	res, err := client.Get(link)
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("the link: %v %v", res, err)
	}
	res.Body.Close()
	res, err = client.Get("http://" + u.Host + "/api/overview")
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("after signing in: %v %v", res, err)
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

func TestNewAndCheckoutAreActionsAndTheFormHelpersAreEndpoints(t *testing.T) {
	f := &fake{next: nextView{Number: 4, Skipped: []string{"3 (branch u3 exists in api)"}}, branchList: []branchView{{Name: "feat/x", Repos: []string{"api"}}}}
	_, h := serve(t, f, Options{})

	var n nextView
	if err := json.Unmarshal(get(t, h, "/api/next?multiverse=acme", nil).Body.Bytes(), &n); err != nil || n.Number != 4 || len(n.Skipped) != 1 {
		t.Errorf("next = %+v, %v", n, err)
	}
	if body := get(t, h, "/api/branches?multiverse=acme", nil).Body.String(); !strings.Contains(body, `"feat/x"`) || !strings.Contains(body, `"api"`) {
		t.Errorf("branches: %s", body)
	}
	if !strings.Contains(f.asks(), "next:acme") || !strings.Contains(f.asks(), "branches:acme") {
		t.Errorf("asked %s", f.asks())
	}

	// A new universe may leave its name out: the next free number is used.
	if rec := post(t, h, `{"action":"new","multiverse":"acme"}`); rec.Code != http.StatusAccepted {
		t.Errorf("new with no name: %d %s", rec.Code, rec.Body)
	}
	wait(t, "new", func() bool { o := listOps(t, h); return len(o) == 1 && o[0].State == "done" })
	if rec := post(t, h, `{"action":"checkout","multiverse":"acme","branch":"feat/x","onlyExisting":true}`); rec.Code != http.StatusAccepted {
		t.Errorf("checkout: %d %s", rec.Code, rec.Body)
	}
	wait(t, "checkout", func() bool { o := listOps(t, h); return len(o) == 2 && o[1].State == "done" })
	if o := listOps(t, h); o[0].Title != "Create a universe in acme" || o[1].Title != "Check out feat/x in acme" {
		t.Errorf("titles: %q, %q", o[0].Title, o[1].Title)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.did) != 2 || f.did[1].Branch != "feat/x" || !f.did[1].OnlyExisting {
		t.Errorf("did %+v", f.did)
	}
}

// Where something lives is worked out once, on the server, in one notation, so
// the page draws a platform's mark and never has to read a URL.
func TestNodesAndOverviewsCarryTheirLocationInOneNotation(t *testing.T) {
	t.Setenv("HOME", "/home/me")
	index := domain.Source{Repo: "https://github.com/me/index.git", Path: "catalog.yaml"}
	tree := domain.Node{Name: "index", Kind: domain.CatalogNode, Source: index, Children: []domain.Node{
		{Name: "personal", Path: "personal", Kind: domain.CatalogNode, Source: domain.Source{Repo: index.Repo, Path: "catalogs/personal.yaml"}},
		{Name: "acme", Path: "acme", Kind: domain.CatalogNode, Source: domain.Source{Repo: "git@gitlab.com:acme/index.git", Path: "catalog.yaml"}},
		{Name: "notes", Path: "notes", Kind: domain.MultiverseNode, Source: domain.Source{Path: "/home/me/notes/multiverse.yaml"}},
	}}
	v := toNode(tree, map[string]string{})
	got := map[string]string{}
	for _, c := range append([]nodeView{v}, v.Children...) {
		got[c.Name] = c.Loc.Short + "|" + c.Within
	}
	for name, want := range map[string]string{
		"index":    "github:me/index|",
		"personal": "github:me/index|catalogs/personal.yaml", // the same repository as the root: the file says which catalog
		"acme":     "gitlab:acme/index|",
		"notes":    "file:~/notes/multiverse.yaml|",
	} {
		if got[name] != want {
			t.Errorf("%s: %q, want %q", name, got[name], want)
		}
	}
	if v.Loc.Platform != "github" || v.Children[1].Loc.Platform != "gitlab" || v.Children[2].Loc.Platform != "file" {
		t.Errorf("platforms: %+v", v)
	}

	rep := app.StatusReport{
		Catalog:     app.CatalogStatus{Source: "https://github.com/me/index.git"},
		Multiverses: []app.MultiverseStatus{{Name: "acme", RepoURL: "git@gitlab.com:acme/multiverse.git", Ref: "main"}, {Name: "local"}},
	}
	o := overviewOf("v1", false, rep)
	if o.RootLoc == nil || o.RootLoc.Short != "github:me/index" {
		t.Errorf("root: %+v", o.RootLoc)
	}
	if s := o.Sources["acme"]; s.Short != "gitlab:acme/multiverse@main" || s.Platform != "gitlab" {
		t.Errorf("source: %+v", s)
	}
	if _, ok := o.Sources["local"]; ok {
		t.Error("a project with no repository has no source to draw")
	}
}

func TestAPageIsToldOfANewerReleaseAndTheServerKeepsLooking(t *testing.T) {
	n := app.UpdateNotice{Current: "v1.0.0", Latest: "v1.0.3", Releases: 3, Fixes: 4, Highlights: []string{"a fix"}}
	f := &fake{update: updateView{Notice: &n}}
	s, _ := serve(t, f, Options{Version: "v1.0.0", UpdateEvery: 10 * time.Millisecond})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	c, _, err := dial(t, ts, signedIn())
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()

	// It is not tied to a page being open: the look that leaves its result for
	// the command line is made either way, and a page that joins is told at once.
	for {
		e := read(t, c)
		if e.Type != "update" {
			continue
		}
		var got updateView
		if err := json.Unmarshal(e.Data.(json.RawMessage), &got); err != nil || got.Notice == nil || got.Notice.Latest != "v1.0.3" || got.Notice.Fixes != 4 {
			t.Fatalf("update = %s %v", e.Data, err)
		}
		break
	}
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		f.mu.Lock()
		looked := f.looked
		f.mu.Unlock()
		if looked {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the server never asked for a new look")
		}
	}
}

func TestTheDiffOfAChangeIsReadOnlyAndFollowsWhatIsAsked(t *testing.T) {
	f := &fake{files: diffFilesView{Base: "main", Files: []fileView{{Path: "a.go", Status: "modified", Added: 2}}}, patch: patchView{Path: "a.go", Patch: "@@"}}
	_, h := serve(t, f, Options{ReadOnly: true})
	rec := do(t, h, "GET", "/api/universe/diff?multiverse=acme&universe=3&repo=api&base=release", "", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"a.go"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	rec = do(t, h, "GET", "/api/universe/diff?multiverse=acme&universe=3&repo=api&scope=uncommitted&file=a.go", "", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"patch":"@@"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if got := f.asks(); !strings.Contains(got, "diff:acme:3:api:branch:release") || !strings.Contains(got, "patch:acme:3:api:uncommitted::a.go") {
		t.Errorf("asked %q", got)
	}
	if rec := do(t, h, "GET", "/api/universe/diff?multiverse=acme&universe=3", "", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("a diff of no repository: %d", rec.Code)
	}
	if rec := do(t, h, "GET", "/api/universe/diff?multiverse=acme&universe=3&repo=api&scope=bogus", "", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("a scope that is neither: %d", rec.Code)
	}
}

// The opt-out keeps the command line from talking to GitHub on its own, and the
// web view is no different; a build from source has no release to be behind.
func TestTheServerDoesNotLookWhereTheOptOutIsSetOrForADevBuild(t *testing.T) {
	for name, tc := range map[string]struct {
		version string
		optOut  bool
	}{"opted out": {"v1.0.0", true}, "dev build": {"dev", false}} {
		t.Run(name, func(t *testing.T) {
			if tc.optOut {
				t.Setenv(app.UpdateOptOutEnv, "1")
			}
			f := &fake{}
			serve(t, f, Options{Version: tc.version, UpdateEvery: 5 * time.Millisecond})
			time.Sleep(60 * time.Millisecond)
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.looked {
				t.Error("it looked")
			}
		})
	}
}
