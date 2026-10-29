package domain

import (
	"strings"
	"testing"
)

func TestDefaultServiceMustBeOneOfTheAppsListedServices(t *testing.T) {
	m := &Manifest{
		Name: "acme", Domain: "example.test", Expose: ExposeLocal,
		Repos: map[string]Repo{"api": {}},
		Apps:  map[string]App{"api": {Repo: "api", Services: []string{"web", "db"}, DefaultService: "worker"}},
	}
	err := m.Validate()
	if err == nil || !strings.Contains(err.Error(), "apps.api.defaultService") {
		t.Fatalf("err = %v", err)
	}
	app := m.Apps["api"]
	app.DefaultService = "web"
	m.Apps["api"] = app
	if err := m.Validate(); err != nil && strings.Contains(err.Error(), "defaultService") {
		t.Fatalf("a listed service was refused: %v", err)
	}
}

func TestTheProxyIsChecked(t *testing.T) {
	for name, tc := range map[string]struct {
		proxy Proxy
		want  string
	}{
		"an unsupported kind": {Proxy{Kind: "caddy", Entrypoints: map[string]int{"web": 80}}, `"caddy" is not supported`},
		"two on one port":     {Proxy{Entrypoints: map[string]int{"web": 80, "alt": 80}}, "already entrypoint"},
		"a port out of range": {Proxy{Entrypoints: map[string]int{"web": 0}}, "out of range"},
	} {
		m := &Manifest{Name: "acme", Domain: "example.test", Expose: ExposeLocal, Proxy: tc.proxy, Repos: map[string]Repo{"api": {URL: "x"}}}
		if err := m.Validate(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
	}
}

// A group that is only a profile starts nothing when the app names its
// services, since compose then starts only those.
func TestAnOptionalGroupWithoutServicesNeedsTheAppToLeaveThemOut(t *testing.T) {
	m := &Manifest{Name: "acme", Domain: "example.test", Expose: ExposeLocal, Proxy: Proxy{Entrypoints: map[string]int{"web": 80}},
		Repos: map[string]Repo{"api": {URL: "x"}},
		Apps:  map[string]App{"api": {Repo: "api", Services: []string{"web"}, Optional: map[string][]string{"celery": nil}}}}
	if err := m.Validate(); err == nil || !strings.Contains(err.Error(), "optional.celery.services") {
		t.Errorf("err = %v", err)
	}
	m.Apps["api"] = App{Repo: "api", Optional: map[string][]string{"celery": nil}}
	if err := m.Validate(); err != nil {
		t.Errorf("a profile-only group on an app that starts every service: %v", err)
	}
}

func TestABaseIsABranchNotTheRemotes(t *testing.T) {
	m := &Manifest{Name: "acme", Domain: "example.test", Expose: ExposeLocal, Proxy: Proxy{Entrypoints: map[string]int{"web": 80}},
		Repos: map[string]Repo{"api": {URL: "x", Base: "origin/develop"}}}
	if err := m.Validate(); err == nil || !strings.Contains(err.Error(), "name the branch (develop)") {
		t.Errorf("err = %v", err)
	}
}

// An address on a TLS entrypoint says https, and leaves out 443 as an address
// on a plain one leaves out 80.
func TestAnOriginFollowsItsEntrypointsScheme(t *testing.T) {
	m := &Manifest{Proxy: Proxy{Entrypoints: map[string]int{"web": 443, "devapi": 8443, "plain": 80, "alt": 8001},
		TLS: map[string]bool{"web": true, "devapi": true}}}
	for ep, want := range map[string]string{
		"web": "https://api-u3.example.test", "devapi": "https://api-u3.example.test:8443",
		"plain": "http://api-u3.example.test", "alt": "http://api-u3.example.test:8001",
	} {
		if got := m.Origin(ep, "api-u3.example.test"); got != want {
			t.Errorf("%s: %s, want %s", ep, got, want)
		}
	}
}

// One repository's own file cannot turn an entrypoint every universe's origins
// follow, and certificates two multiverses disagree about are a clash.
func TestTLSIsTheMultiversesToSay(t *testing.T) {
	m := &Manifest{Name: "acme", Domain: "example.test", Expose: ExposeLocal,
		Proxy: Proxy{Entrypoints: map[string]int{"web": 80}, TLS: map[string]bool{}},
		Repos: map[string]Repo{"api": {URL: "x"}}, Apps: map[string]App{}}
	err := m.Merge("api", Part{Entrypoints: map[string]int{"web": 80}, TLS: map[string]bool{"web": true}})
	if err == nil || !strings.Contains(err.Error(), "TLS") || m.Proxy.TLS["web"] {
		t.Errorf("err = %v, tls = %v", err, m.Proxy.TLS)
	}
	if err := m.Merge("api", Part{Entrypoints: map[string]int{"rpc": 8443}, TLS: map[string]bool{"rpc": true}}); err != nil || !m.Proxy.TLS["rpc"] {
		t.Errorf("a new entrypoint of the repository's own: %v, %v", err, m.Proxy.TLS)
	}
	b := *m
	b.Proxy.Certificates = []Certificate{{CertFile: "/c.pem", KeyFile: "/k.pem"}}
	if got := strings.Join(ProxyClashes("a", m, "b", &b), "\n"); !strings.Contains(got, "certificates") {
		t.Errorf("clashes = %q", got)
	}
}
