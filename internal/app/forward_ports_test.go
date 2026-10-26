package app

import (
	"context"
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

type forwardSystem struct {
	System
	free  []int        // what the kernel offers next, in order
	taken map[int]bool // ports another process holds
}

func (f *forwardSystem) LANIP(context.Context) (string, error) { return "192.168.1.5", nil }
func (f *forwardSystem) PortFree(_ context.Context, port int) (bool, error) {
	return !f.taken[port], nil
}
func (f *forwardSystem) FreePort(context.Context) (int, error) {
	port := f.free[0]
	f.free = f.free[1:]
	return port, nil
}

type forwardStore struct {
	Store
	others []domain.UniverseState
}

func (f forwardStore) List() ([]domain.UniverseState, error) { return f.others, nil }

type forwardRuntime struct {
	Runtime
	up bool
}

func (f forwardRuntime) Containers(context.Context, string) ([]Container, error) {
	if f.up {
		return []Container{{Name: "acme-u1-api-web-1", Status: "Up 2 minutes"}}, nil
	}
	return nil, nil
}

// The port is chosen once and kept: a phone given the address must keep
// working across restarts, a port another universe holds is never reused, and
// a kept port is only replaced when something else took it while the universe
// was down (the universe's own containers make it look taken while it runs).
func TestForwardPorts(t *testing.T) {
	rule := func() map[string]domain.RouteRule {
		return map[string]domain.RouteRule{
			"api": {App: "api", Service: "web", Port: 8000, PortForward: domain.PortForwardAuto},
			"ui":  {App: "ui", Service: "app", Port: 4200}, // no portForward: untouched
		}
	}
	kept := domain.UniverseState{ID: "1", Rules: map[string]domain.RouteRule{
		"api": {PortForward: domain.PortForwardAuto, HostPort: 50001},
	}}
	other := domain.UniverseState{ID: "2", Rules: map[string]domain.RouteRule{"api": {HostPort: 50002}}}

	for _, tc := range []struct {
		name    string
		st      domain.UniverseState
		sys     *forwardSystem
		up      bool
		others  []domain.UniverseState
		want    int
		wantURL string
	}{
		{"first time takes what the kernel offers", domain.UniverseState{ID: "1"}, &forwardSystem{free: []int{50010}}, false, nil, 50010, "http://192.168.1.5:50010"},
		{"a kept port that is still free is kept", kept, &forwardSystem{}, false, nil, 50001, "http://192.168.1.5:50001"},
		{"a kept port is kept while our own containers hold it", kept, &forwardSystem{taken: map[int]bool{50001: true}}, true, nil, 50001, "http://192.168.1.5:50001"},
		{"a kept port taken while the universe was down is replaced", kept, &forwardSystem{taken: map[int]bool{50001: true}, free: []int{50011}}, false, nil, 50011, "http://192.168.1.5:50011"},
		{"another universe's port is skipped", domain.UniverseState{ID: "1"}, &forwardSystem{free: []int{50002, 50012}}, false, []domain.UniverseState{other}, 50012, "http://192.168.1.5:50012"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := New(Deps{System: tc.sys, Store: forwardStore{others: tc.others}, Runtime: forwardRuntime{up: tc.up}},
				"/ws", &domain.Manifest{Name: "acme", Expose: domain.ExposeLAN}, "test")
			rules := rule()
			if err := svc.forwardPorts(context.Background(), tc.st, rules); err != nil {
				t.Fatal(err)
			}
			if got := rules["api"]; got.HostPort != tc.want || got.ForwardURL != tc.wantURL {
				t.Fatalf("api = port %d, url %q; want %d, %q", got.HostPort, got.ForwardURL, tc.want, tc.wantURL)
			}
			if ui := rules["ui"]; ui.HostPort != 0 || ui.ForwardURL != "" {
				t.Fatalf("a host without portForward was given a port: %+v", ui)
			}
		})
	}
}

func TestForwardPortsLoopbackWhenLocalOnly(t *testing.T) {
	svc := New(Deps{System: &forwardSystem{free: []int{50020}}, Store: forwardStore{}, Runtime: forwardRuntime{}},
		"/ws", &domain.Manifest{Name: "acme", Expose: domain.ExposeLocal}, "test")
	rules := map[string]domain.RouteRule{"api": {PortForward: domain.PortForwardAuto}}
	if err := svc.forwardPorts(context.Background(), domain.UniverseState{ID: "1"}, rules); err != nil {
		t.Fatal(err)
	}
	if got := rules["api"].ForwardURL; !strings.HasPrefix(got, "http://127.0.0.1:") {
		t.Fatalf("ForwardURL = %q, want the loopback address", got)
	}
}
