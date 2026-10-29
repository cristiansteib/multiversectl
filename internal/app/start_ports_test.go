package app

import (
	"context"
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

type portSystem struct {
	System
	taken map[int]bool
}

func (p portSystem) PortFree(_ context.Context, port int) (bool, error) { return !p.taken[port], nil }

type quietOut struct{ Output }

func (quietOut) Warn(string, ...any) {}

type portProxy struct {
	Proxy
	running bool
	started bool
}

func (p *portProxy) EnsureNetwork(context.Context, string) error { return nil }
func (p *portProxy) Status(context.Context) (ProxyStatus, error) {
	return ProxyStatus{Running: p.running}, nil
}
func (p *portProxy) EnsureUp(context.Context, ProxyRequest) error { p.started = true; return nil }

// A port held by another process fails the start by name, before docker
// creates a container it cannot start; a proxy that is already up holds its
// own ports, so that is not a conflict.
func TestEnsureProxyChecksPortsOnlyWhenDown(t *testing.T) {
	manifest := &domain.Manifest{
		Name:  "acme",
		Proxy: domain.Proxy{Entrypoints: map[string]int{"web": 80, "devapi": 8001}},
	}
	for _, tc := range []struct {
		name    string
		running bool
		wantErr string
	}{
		{"down and taken", false, `entrypoint "devapi" needs port 8001`},
		{"up and holding it", true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			px := &portProxy{running: tc.running}
			svc := New(Deps{System: portSystem{taken: map[int]bool{8001: true}}, Proxy: px, Out: quietOut{}}, "/ws", manifest, "test")
			err := svc.EnsureProxy(context.Background(), "example.test", false)
			if tc.wantErr == "" {
				if err != nil || !px.started {
					t.Fatalf("err = %v, started = %v", err, px.started)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) || px.started {
				t.Fatalf("err = %v, started = %v", err, px.started)
			}
		})
	}
}
