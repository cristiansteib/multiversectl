package app

import (
	"context"
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// With domain: none no name is asked of anybody: every host is forwarded to a
// port of its own, and every address a person or a service is given is that
// one, by IP.
func TestWithNoDomainEveryAddressIsTheForwardedOne(t *testing.T) {
	manifest := &domain.Manifest{
		Name: "acme", Domain: domain.DomainNone, Expose: domain.ExposeLAN,
		Proxy: domain.Proxy{Entrypoints: map[string]int{"web": 80}},
		Repos: map[string]domain.Repo{"api": {}},
		Apps: map[string]domain.App{"api": {Repo: "api", Hosts: map[string]domain.HostRule{
			"api": {Service: "web", Port: 8000}, "adm": {Service: "adm", Port: 9000},
		}}},
	}
	svc := New(Deps{System: fakeSystem{}, Files: &tempFiles{}}, "/ws", manifest, "test")

	if dom, err := svc.ResolveDomain(context.Background()); err != nil || dom != "localhost" {
		t.Errorf("the proxy's internal domain = %q, %v", dom, err)
	}
	rules, err := svc.resolveRules(context.Background(), svc.Universe("3"), "localhost")
	if err != nil {
		t.Fatal(err)
	}
	for key, rule := range rules {
		if rule.PortForward != domain.PortForwardOnly {
			t.Errorf("%s is not forwarded: %+v", key, rule)
		}
	}

	// What forwardPorts would have written once the universe started.
	st := domain.UniverseState{Name: "3", Slug: "3", Domain: "localhost", Hosts: hostnames(rules), Rules: rules}
	for key, port := range map[string]string{"api": "41000", "adm": "41001"} {
		rule := st.Rules[key]
		rule.ForwardURL = "http://192.0.2.7:" + port
		st.Rules[key] = rule
	}

	env := svc.runtimeEnv(svc.Universe("3"), st)
	for name, want := range map[string]string{
		"API_URL": "http://192.0.2.7:41000", "API_HOST": "192.0.2.7",
		"MULTIVERSE_ORIGINS": "http://192.0.2.7:41001,http://192.0.2.7:41000", "MULTIVERSE_HOSTS": "192.0.2.7",
	} {
		if env[name] != want {
			t.Errorf("%s = %q, want %q", name, env[name], want)
		}
	}
	for _, route := range svc.Routes(st, "api") {
		if got := svc.routeURL(route); got != route.ForwardURL {
			t.Errorf("%s listed at %q, want the forwarded %q", route.Name, got, route.ForwardURL)
		}
	}
	// Before the first start there is no forwarded port; nothing is running to
	// be reached, and the name that is shown is the internal one.
	st.Rules["api"] = domain.RouteRule{App: "api", Hostname: "api-3.localhost", Entrypoint: "web"}
	if got := svc.originOf(st, "api", "api-3.localhost"); got != "http://api-3.localhost" {
		t.Errorf("before a start: %q", got)
	}
}

// One host can be given by IP alone while the rest of the multiverse keeps its
// names: a service that should not be known by one (an identity provider whose
// issuer URL must be one address).
func TestAHostCanAskToBeReachedByIPAlone(t *testing.T) {
	manifest := &domain.Manifest{
		Name: "acme", Domain: "auto", Expose: domain.ExposeLAN,
		Proxy: domain.Proxy{Entrypoints: map[string]int{"web": 80}},
		Repos: map[string]domain.Repo{"api": {}},
		Apps: map[string]domain.App{"api": {Repo: "api", Hosts: map[string]domain.HostRule{
			"api":      {Service: "web", Port: 8000, PortForward: domain.PortForwardAuto},
			"keycloak": {Service: "keycloak", Port: 8080, PortForward: domain.PortForwardOnly},
		}}},
	}
	svc := New(Deps{System: fakeSystem{}, Files: &tempFiles{}}, "/ws", manifest, "test")
	rules, err := svc.resolveRules(context.Background(), svc.Universe("1"), "192-0-2-7.example.test")
	if err != nil {
		t.Fatal(err)
	}
	st := domain.UniverseState{Name: "1", Slug: "u1", Hosts: hostnames(rules), Rules: rules}
	for key, port := range map[string]string{"api": "41000", "keycloak": "43791"} {
		rule := st.Rules[key]
		rule.ForwardURL = "http://192.0.2.7:" + port
		st.Rules[key] = rule
	}
	urls := map[string][]string{}
	for _, route := range svc.Routes(st, "api") {
		key := strings.TrimSuffix(route.Name, "-"+st.Slug)
		urls[key] = append(urls[key], svc.routeURL(route))
	}
	if got := urls["keycloak"]; len(got) != 1 || got[0] != "http://192.0.2.7:43791" {
		t.Errorf("keycloak is at %v, want its forwarded address alone", got)
	}
	if got := urls["api"]; len(got) != 1 || got[0] != "http://api-u1.192-0-2-7.example.test" {
		t.Errorf("api is at %v, want its name (and its forward, listed beside it)", got)
	}
	env := svc.runtimeEnv(svc.Universe("1"), st)
	if env["KEYCLOAK_URL"] != "http://192.0.2.7:43791" || env["KEYCLOAK_HOST"] != "192.0.2.7" || env["API_URL"] != "http://api-u1.192-0-2-7.example.test" {
		t.Errorf("env: %v %v %v", env["KEYCLOAK_URL"], env["KEYCLOAK_HOST"], env["API_URL"])
	}
	// The summaries list the forwarded address once for a host that has no name.
	var listed []string
	for _, a := range svc.appSummaries(context.Background(), st, nil, nil, ListOptions{}) {
		for _, u := range a.URLs {
			listed = append(listed, u.Host+" "+u.URL)
		}
	}
	if strings.Count(strings.Join(listed, "\n"), "http://192.0.2.7:43791") != 1 {
		t.Errorf("the keycloak address is listed %v", listed)
	}
}
