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
