package app_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/app"
	"github.com/cristiansteib/multiversectl/internal/domain"
)

// sharedProxy is a proxy that remembers what it was last asked to listen on, as
// the real one does through its compose file.
type sharedProxy struct {
	startProxy
	listeners  []domain.Listener
	running    bool
	ups        [][]domain.Listener
	downs      int
	serving    []string // containers the runtime says are routed through it
	servingErr error
}

func (p *sharedProxy) Serving(context.Context) ([]string, error) { return p.serving, p.servingErr }

func (p *sharedProxy) Down(context.Context) error {
	p.running = false
	p.downs++
	return nil
}

func (p *sharedProxy) Status(context.Context) (app.ProxyStatus, error) {
	return app.ProxyStatus{Running: p.running, Listeners: p.listeners}, nil
}

func (p *sharedProxy) EnsureUp(_ context.Context, r app.ProxyRequest) error {
	p.listeners, p.running = r.Listeners, true
	p.ups = append(p.ups, r.Listeners)
	return nil
}

type recordingRouter struct{ routes []app.Route }

func (r *recordingRouter) Materialize(_ context.Context, req app.OverrideRequest) ([]string, error) {
	r.routes = append(r.routes, req.Routes...)
	return nil, nil
}

func describe(ls []domain.Listener) string {
	var out []string
	for _, l := range ls {
		out = append(out, l.Name+"="+l.Bind+":"+map[bool]string{true: "tls", false: "plain"}[l.TLS]+":"+itoa(l.Port))
	}
	return strings.Join(out, " ")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for ; n > 0; n /= 10 {
		d = append([]byte{byte('0' + n%10)}, d...)
	}
	return string(d)
}

// sharedRig is acme (local, web on 80) about to start a universe, beside beta
// (open to the network, web on betaPort) which has a universe running, on a proxy
// that already listens the way beta asked.
func sharedRig(t *testing.T, betaPort int) (*renderRig, *startRuntime, *sharedProxy, *recordingRouter) {
	t.Helper()
	r, rt := startRig(t, "{}")
	beta := &domain.Manifest{Name: "beta", Path: "/other/multiverse.yaml", Expose: domain.ExposeLAN,
		Proxy: domain.Proxy{Entrypoints: map[string]int{"web": betaPort}}}
	r.svc.Manifest.Proxy.Entrypoints = map[string]int{"web": 80}
	r.svc.Manifests = fakeLoader{beta.Path: beta}
	r.svc.Config = installConfig{app.Config{Multiverses: map[string]app.MultiverseEntry{
		"acme": {Root: r.svc.Multiverse, Manifest: r.svc.Manifest.Path},
		"beta": {Root: "/other", Manifest: beta.Path},
	}}}
	rt.running = []app.Container{{Name: "beta-u2-api-web-1", Project: "beta-u2-api", Service: "web", Status: "Up 2 minutes",
		WorkingDir: "/other/universes/2/api", ConfigFile: "/other/universes/2/api/compose.yaml"}}
	px := &sharedProxy{running: true, listeners: []domain.Listener{{Name: "web", Port: betaPort, Bind: "0.0.0.0"}}}
	rr := &recordingRouter{}
	r.svc.Proxy, r.svc.Router = px, rr
	return r, rt, px, rr
}

// The finding that started this: a local and a LAN multiverse on ports of their
// own run at once, the proxy listens on both, and neither one's universes are
// taken off the air by the other starting.
func TestALocalAndALanMultiverseRunAtTheSameTime(t *testing.T) {
	r, rt, px, rr := sharedRig(t, 8080)
	if err := r.svc.Start(context.Background(), app.StartInput{Universe: "1"}); err != nil {
		t.Fatal(err)
	}
	if len(rt.ups) != 1 {
		t.Fatalf("started %v", rt.ups)
	}
	// The name beta's containers were labelled with is kept; the newcomer's port
	// gets its own, after its name.
	if len(px.ups) != 1 || describe(px.ups[0]) != "web=0.0.0.0:plain:8080 web-80=127.0.0.1:plain:80" {
		t.Fatalf("proxy was asked for %v", px.ups)
	}
	if len(rr.routes) != 1 || rr.routes[0].Entrypoint != "web-80" {
		t.Errorf("acme's router must use the proxy's name for port 80, got %+v", rr.routes)
	}
	if strings.Contains(r.out.text(), "being reconfigured") {
		t.Errorf("nothing of beta's moved:\n%s", r.out.text())
	}
}

// Two multiverses that call one port by two names share one listener, and a
// second start changes nothing.
func TestTheSamePortWantedTheSameWayIsOneListener(t *testing.T) {
	r, _, px, rr := sharedRig(t, 80)
	r.svc.Manifest.Expose = domain.ExposeLAN
	if err := r.svc.Start(context.Background(), app.StartInput{Universe: "1"}); err != nil {
		t.Fatal(err)
	}
	if describe(px.listeners) != "web=0.0.0.0:plain:80" || rr.routes[0].Entrypoint != "web" {
		t.Errorf("listeners %s, routes %+v", describe(px.listeners), rr.routes)
	}
}

// A listener nobody running needs is replaced rather than refused: only the
// universes that run are protected.
func TestAStaleListenerIsReplacedWhenNothingRunsOnIt(t *testing.T) {
	r, rt, px, _ := sharedRig(t, 80)
	rt.running = nil
	if err := r.svc.Start(context.Background(), app.StartInput{Universe: "1"}); err != nil {
		t.Fatal(err)
	}
	if describe(px.listeners) != "web=127.0.0.1:plain:80" {
		t.Errorf("listeners = %s", describe(px.listeners))
	}
	if !strings.Contains(r.out.text(), "port 80 published on 127.0.0.1 instead of 0.0.0.0") {
		t.Errorf("no warning:\n%s", r.out.text())
	}
}

// Starting and stopping universes of the two multiverses in turn: what the
// stopped one opened is closed, what the other needs is not.
func TestStoppingTheLastUniverseOfAMultiverseClosesOnlyItsPorts(t *testing.T) {
	r, rt, px, _ := sharedRig(t, 8080)
	ctx := context.Background()
	if err := r.svc.Start(ctx, app.StartInput{Universe: "1"}); err != nil {
		t.Fatal(err)
	}
	if len(px.listeners) != 2 {
		t.Fatalf("listeners = %s", describe(px.listeners))
	}
	acme := app.Container{Name: "acme-u1-api-web-1", Project: "acme-u1-api", Status: "Up 1 minute",
		WorkingDir: r.svc.Multiverse + "/universes/1/api", ConfigFile: r.svc.Multiverse + "/universes/1/api/compose.yaml"}
	rt.running = append(rt.running, acme)

	// beta's universe goes away while acme's still runs: 8080 closes, 80 stays.
	rt.running = []app.Container{acme}
	if err := r.svc.Stop(ctx, app.StopInput{Universe: "1"}); err != nil {
		t.Fatal(err)
	}
	if describe(px.listeners) != "web-80=127.0.0.1:plain:80" {
		t.Errorf("after beta stopped, listeners = %s", describe(px.listeners))
	}
	if !strings.Contains(r.out.text(), "stopped listening on") {
		t.Errorf("not said:\n%s", r.out.text())
	}

	// With nothing running at all the proxy is stopped: nothing is served by it.
	rt.running = nil
	before := len(px.ups)
	if err := r.svc.Stop(ctx, app.StopInput{Universe: "1"}); err != nil {
		t.Fatal(err)
	}
	if px.downs != 1 || px.running || len(px.ups) != before {
		t.Errorf("downs = %d, running = %v, ups %d -> %d", px.downs, px.running, before, len(px.ups))
	}
}

// A proxy that is already down is not stopped again, and one that does not list
// what the running universes ask for is not emptied.
func TestTrimLeavesAnUnrelatedOrStoppedProxyAlone(t *testing.T) {
	r, rt, px, _ := sharedRig(t, 8080)
	ctx := context.Background()
	px.running = false
	rt.running = nil
	if err := r.svc.Stop(ctx, app.StopInput{Universe: "1"}); err != nil {
		t.Fatal(err)
	}
	if px.downs != 0 {
		t.Errorf("a stopped proxy was stopped again")
	}

	// beta runs on 8080 but the proxy only lists port 9999.
	px.running, px.listeners = true, []domain.Listener{{Name: "other", Port: 9999, Bind: "0.0.0.0"}}
	rt.running = []app.Container{{Name: "beta-u2-api-web-1", Project: "beta-u2-api", Service: "web", Status: "Up 2 minutes",
		WorkingDir: "/other/universes/2/api", ConfigFile: "/other/universes/2/api/compose.yaml"}}
	if err := r.svc.Stop(ctx, app.StopInput{Universe: "1"}); err != nil {
		t.Fatal(err)
	}
	if len(px.ups) != 0 || px.downs != 0 || !px.running {
		t.Errorf("the proxy was touched: ups %v, downs %d", px.ups, px.downs)
	}
}

// The early warning: universe new, system proxy status and doctor all ask
// ProxyConflicts, which names both sides and the way out.
func TestTheConflictIsKnownBeforeAnythingIsBuilt(t *testing.T) {
	r, _, _, _ := sharedRig(t, 80)
	got := r.svc.ProxyConflicts(context.Background())
	if len(got) != 1 || !strings.Contains(got[0], "this multiverse (expose local) and universe 2 of beta (expose lan) both want port 80") ||
		!strings.Contains(got[0], "stop the universes of one of them first") {
		t.Fatalf("conflicts = %q", got)
	}
	r2, _, _, _ := sharedRig(t, 8080)
	if got := r2.svc.ProxyConflicts(context.Background()); len(got) != 0 {
		t.Errorf("distinct ports coexist, yet %q", got)
	}
}

// A container no manifest accounts for (another user's multiverse, one whose
// manifest cannot be read) still carries the served-by label: the proxy stays.
func TestTheProxyStaysUpWhileAContainerIsStillRoutedThroughIt(t *testing.T) {
	r, rt, px, _ := sharedRig(t, 8080)
	rt.running = nil
	px.serving = []string{"gamma-u1-api-web-1"}
	if err := r.svc.Stop(context.Background(), app.StopInput{Universe: "1"}); err != nil {
		t.Fatal(err)
	}
	if px.downs != 0 || !px.running {
		t.Errorf("the proxy was stopped under a container that uses it")
	}
	if !strings.Contains(r.out.text(), "gamma-u1-api-web-1") {
		t.Errorf("not said which container keeps it up:\n%s", r.out.text())
	}

	// When the runtime cannot answer, it is not assumed to be nothing.
	px.serving, px.servingErr = nil, errors.New("permission denied")
	if err := r.svc.Stop(context.Background(), app.StopInput{Universe: "1"}); err != nil {
		t.Fatal(err)
	}
	if px.downs != 0 || !px.running {
		t.Errorf("the proxy was stopped without knowing nothing uses it")
	}
}
