package app

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// A routed service joins the universe's own network. A host reached by IP alone has
// no route in the proxy, so it joins no network shared with it.
func TestRoutesUseTheUniversesOwnNetworkAndNoneForAnAddressByIPAlone(t *testing.T) {
	manifest := &domain.Manifest{Name: "acme", Proxy: domain.Proxy{Network: "legacy-shared"}}
	svc := New(Deps{}, "/ws", manifest, "test")
	st := domain.UniverseState{
		Name: "3", Slug: "u3", Project: "acme-u3",
		Rules: map[string]domain.RouteRule{
			"web": {App: "web", Service: "web", Port: 80, Entrypoint: "web", Hostname: "web-u3.example.test"},
			"api": {App: "web", Service: "api", Port: 8000, Entrypoint: "web", Hostname: "api-u3.example.test",
				PortForward: domain.PortForwardOnly, HostPort: 41000, ForwardURL: "http://10.0.0.5:41000"},
		},
	}
	got := map[string]string{}
	for _, r := range svc.Routes(st, "web") {
		got[r.Service] = r.Network
	}
	if got["web"] != "acme-u3-web_default" {
		t.Errorf("a routed service is on %q, want the app's own acme-u3-web_default", got["web"])
	}
	if net, ok := got["api"]; !ok || net != "" {
		t.Errorf("a service reached by IP alone is on %q (present: %v), want no network", net, ok)
	}
}

// Two universes of one manifest never get the same network, whatever the hosts.
func TestTwoUniversesNeverShareARouteNetwork(t *testing.T) {
	manifest := &domain.Manifest{Name: "acme"}
	svc := New(Deps{}, "/ws", manifest, "test")
	rule := map[string]domain.RouteRule{"auth": {App: "auth", Service: "auth", Port: 8080, Entrypoint: "web", Hostname: "auth.example.test"}}
	one := svc.Routes(domain.UniverseState{Name: "1", Slug: "u1", Project: "acme-u1", Rules: rule}, "auth")
	two := svc.Routes(domain.UniverseState{Name: "2", Slug: "u2", Project: "acme-u2", Rules: rule}, "auth")
	if len(one) != 1 || len(two) != 1 || one[0].Network == two[0].Network {
		t.Fatalf("universes 1 and 2 are on %v and %v", one, two)
	}
}

// The pools bound how many networks Docker can create; a network whose subnet was
// chosen outside them uses none of that room.
func TestPoolUseCountsOnlyTheSubnetsTakenFromThePools(t *testing.T) {
	pools := []addressPool{{Base: "192.168.192.0/20", Size: 24}}
	capacity, used := poolUse(pools, []string{
		"192.168.192.0/24", "192.168.193.0/24", // from the pool
		"10.0.0.0/24",   // chosen by hand, elsewhere
		"172.17.0.0/16", // the default bridge: not in this pool
		"not a subnet",
	})
	if capacity != 16 || used != 2 {
		t.Fatalf("capacity %d, used %d; want 16 and 2 (a /20 split into /24s holds 16)", capacity, used)
	}
}

func TestTheBuiltinPoolsHoldAboutThirtyNetworks(t *testing.T) {
	capacity, _ := poolUse(builtinPools, nil)
	if capacity != 31 {
		t.Fatalf("builtin capacity = %d, want 31 (15 /16s of 172.x plus a /16 of 192.168 split into /20s)", capacity)
	}
}

type poolCommands struct {
	Commands
	info, ls, inspect string
}

func (c poolCommands) RunHost(_ context.Context, cmd HostCommand) (string, error) {
	switch {
	case len(cmd.Args) > 1 && cmd.Args[1] == "info":
		return c.info, nil
	case len(cmd.Args) > 2 && cmd.Args[2] == "ls":
		return c.ls, nil
	default:
		return c.inspect, nil
	}
}

// Running out of pool is told by name, with what to change, before compose fails
// with a message that says neither.
func TestFullAddressPoolsAreReportedWithTheFix(t *testing.T) {
	var subnets []string
	for i := 192; i < 208; i++ {
		subnets = append(subnets, fmt.Sprintf("192.168.%d.0/24", i))
	}
	svc := New(Deps{Commands: poolCommands{
		info:    `[{"Base":"192.168.192.0/20","Size":24}]`,
		ls:      "a b c",
		inspect: strings.Join(subnets, " "),
	}}, "/ws", &domain.Manifest{Name: "acme"}, "test")
	var got []Check
	svc.checkNetworkPool(context.Background(), func(name string, st Status, f string, a ...any) {
		got = append(got, Check{Name: name, Status: st, Detail: fmt.Sprintf(f, a...)})
	})
	if len(got) != 1 || got[0].Status != StatusWarn {
		t.Fatalf("got %+v, want one warning", got)
	}
	for _, want := range []string{"hold 16 networks", "restart fine", "default-address-pools", "restart Docker"} {
		if !strings.Contains(got[0].Detail, want) {
			t.Errorf("the report lacks %q: %s", want, got[0].Detail)
		}
	}
}

// Only the proxy belongs on the old shared network now; anything else on it is a
// container of a universe that has not been brought up since universes got their own.
func TestOnlyTheProxyIsExpectedOnTheOldSharedNetwork(t *testing.T) {
	got := sharedNetworkMembers("multiverse-proxy-traefik-1 acme-u1-web-auth-1 acme-u2-web-auth-1 multiverse-proxy-ab12cd34-traefik-1")
	if len(got) != 2 || got[0] != "acme-u1-web-auth-1" || got[1] != "acme-u2-web-auth-1" {
		t.Fatalf("members = %v, want the two universe containers", got)
	}
	if got := sharedNetworkMembers("multiverse-proxy-traefik-1"); len(got) != 0 {
		t.Fatalf("members = %v, want none", got)
	}
}

// The network of a route is guessed before the compose files are read, and the
// compose files have the last word: a service that names its own network is not on
// <project>_default, and connecting the proxy to a network that does not exist
// would fail the start.
func TestTheComposeFilesDecideWhichNetworkTheProxyIsPointedAt(t *testing.T) {
	cfg := ProjectConfig{Services: map[string]ServiceConfig{
		"web":  {Networks: []string{"acme-u3-web_internal", "acme-u3-web_admin"}},
		"api":  {Networks: []string{"acme-u3-web_default"}},
		"edge": {}, // a runtime that tells us nothing: keep the guess
	}}
	routes := onTheirOwnNetworks([]Route{
		{Service: "web", Network: "acme-u3-web_default"},
		{Service: "api", Network: "acme-u3-web_default"},
		{Service: "edge", Network: "acme-u3-web_default"},
		{Service: "direct", Network: ""}, // reached by IP alone: it joins nothing
	}, cfg)
	got := map[string]string{}
	for _, r := range routes {
		got[r.Service] = r.Network
	}
	want := map[string]string{
		"web":    "acme-u3-web_internal",
		"api":    "acme-u3-web_default",
		"edge":   "acme-u3-web_default",
		"direct": "",
	}
	for service, network := range want {
		if got[service] != network {
			t.Errorf("%s is reached on %q, want %q", service, got[service], network)
		}
	}
}
