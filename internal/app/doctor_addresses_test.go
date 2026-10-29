package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

type probeSystem struct {
	System
	answers map[string]int
	// bodies are what an address says besides its status.
	bodies map[string]string
}

func (p probeSystem) Probe(_ context.Context, url string) (Answer, error) {
	if status, ok := p.answers[url]; ok {
		return Answer{Status: status, Body: p.bodies[url]}, nil
	}
	return Answer{}, errors.New("connection refused")
}

// An address that answers with anything, even a 401, proves the route and the
// service are there; a refused connection or the proxy's own "no backend"
// statuses are what doctor warns about.
func TestDoctorProbesEachAddressOfARunningApp(t *testing.T) {
	manifest := &domain.Manifest{
		Name:  "acme",
		Proxy: domain.Proxy{Entrypoints: map[string]int{"web": 80, "devapi": 8001}},
	}
	st := domain.UniverseState{
		Name: "3", Slug: "3",
		Rules: map[string]domain.RouteRule{
			"api":    {App: "api", Hostname: "api-3.example.test", Service: "nginx", Entrypoint: "web"},
			"devapi": {App: "api", Hostname: "devapi-3.example.test", Service: "web", Entrypoint: "devapi"},
			"ui":     {App: "api", Hostname: "ui-3.example.test", Service: "ui", Entrypoint: "web"},
			"mail":   {App: "api", Hostname: "mail-3.example.test", Service: "mail", Entrypoint: "web"},
		},
	}
	svc := New(Deps{System: probeSystem{answers: map[string]int{
		"http://api-3.example.test":         401,
		"http://devapi-3.example.test:8001": 200,
		"http://ui-3.example.test":          503,
	}}}, "/ws", manifest, "test")

	var checks []Check
	svc.checkAddresses(context.Background(), func(name string, status Status, format string, a ...any) {
		checks = append(checks, Check{Name: name, Status: status, Detail: strings.TrimSpace(format)})
	}, st, "api")

	got := map[string]Status{}
	for _, c := range checks {
		got[c.Name] = c.Status
	}
	want := map[string]Status{
		"url api/api":    StatusOK,
		"url api/devapi": StatusOK,
		"url api/ui":     StatusWarn,
		"url api/mail":   StatusWarn,
	}
	for name, status := range want {
		if got[name] != status {
			t.Errorf("%s = %q, want %q (all: %v)", name, got[name], status, got)
		}
	}
}

// A 404 from the app proves the route is there; the proxy's own 404 says no
// route matches. A service that fails on its front page is a finding too, and a
// gRPC one is the usual reason.
func TestDoctorTellsTheProxysAnswersFromTheServices(t *testing.T) {
	manifest := &domain.Manifest{Name: "acme", Proxy: domain.Proxy{Entrypoints: map[string]int{"web": 80}}}
	rule := func(name string) domain.RouteRule {
		return domain.RouteRule{App: "api", Hostname: name + "-3.example.test", Service: name, Entrypoint: "web"}
	}
	st := domain.UniverseState{Name: "3", Slug: "3", Rules: map[string]domain.RouteRule{
		"own404": rule("own404"), "lost": rule("lost"), "rpc": rule("rpc"), "fine": rule("fine"),
	}}
	svc := New(Deps{System: probeSystem{
		answers: map[string]int{
			"http://own404-3.example.test": 404, "http://lost-3.example.test": 404,
			"http://rpc-3.example.test": 500, "http://fine-3.example.test": 200,
		},
		bodies: map[string]string{
			"http://own404-3.example.test": `{"detail":"Not found."}`,
			"http://lost-3.example.test":   "404 page not found\n",
		},
	}}, "/ws", manifest, "test")
	got := map[string]Check{}
	svc.checkAddresses(context.Background(), func(name string, status Status, format string, a ...any) {
		got[name] = Check{Name: name, Status: status, Detail: fmt.Sprintf(format, a...)}
	}, st, "api")
	for name, want := range map[string]Status{"own404": StatusOK, "lost": StatusWarn, "rpc": StatusWarn, "fine": StatusOK} {
		if got["url api/"+name].Status != want {
			t.Errorf("%s = %v, want %v", name, got["url api/"+name], want)
		}
	}
	if !strings.Contains(got["url api/lost"].Detail, "no route matches") || !strings.Contains(got["url api/rpc"].Detail, "h2c") {
		t.Errorf("the findings do not say what to do: %v / %v", got["url api/lost"].Detail, got["url api/rpc"].Detail)
	}
}

type dialSystem struct {
	System
	http   map[string]int
	open   map[string]bool // host:port that accept connections
	probed []string
}

func (d *dialSystem) Probe(_ context.Context, url string) (Answer, error) {
	d.probed = append(d.probed, url)
	if status, ok := d.http[url]; ok {
		return Answer{Status: status}, nil
	}
	return Answer{}, errors.New("connection refused")
}

func (d *dialSystem) Dial(_ context.Context, address string) error {
	if d.open[address] {
		return nil
	}
	return errors.New("dial tcp " + address + ": connect: connection refused")
}

// A gRPC or other non-HTTP host is asked whether it accepts connections, not
// made to answer a request it would reject.
func TestDoctorChecksEachHostTheWayItSpeaks(t *testing.T) {
	manifest := &domain.Manifest{Name: "acme", Proxy: domain.Proxy{Entrypoints: map[string]int{"web": 80}}}
	st := domain.UniverseState{
		Name: "3", Slug: "3",
		Rules: map[string]domain.RouteRule{
			"api":   {App: "api", Hostname: "api-3.example.test", Service: "web", Entrypoint: "web"},
			"rpc":   {App: "api", Hostname: "rpc-3.example.test", Service: "rpc", Entrypoint: "web", Scheme: "h2c"},
			"queue": {App: "api", Hostname: "queue-3.example.test", Service: "mq", Entrypoint: "web", Probe: "tcp"},
			"db":    {App: "api", Hostname: "db-3.example.test", Service: "db", Entrypoint: "web", Probe: "none"},
			"grpc":  {App: "api", Hostname: "grpc-3.example.test", Service: "g", Entrypoint: "web", Probe: "grpc"},
			"down":  {App: "api", Hostname: "down-3.example.test", Service: "x", Entrypoint: "web", Scheme: "h2c"},
			// An h2c host that is forced back to HTTP is asked over HTTP.
			"edge": {App: "api", Hostname: "edge-3.example.test", Service: "e", Entrypoint: "web", Scheme: "h2c", Probe: "http"},
		},
	}
	sys := &dialSystem{
		http: map[string]int{"http://api-3.example.test": 200, "http://edge-3.example.test": 404},
		open: map[string]bool{"rpc-3.example.test:80": true, "queue-3.example.test:80": true, "grpc-3.example.test:80": true},
	}
	svc := New(Deps{System: sys}, "/ws", manifest, "test")
	got := map[string]Check{}
	svc.checkAddresses(context.Background(), func(name string, status Status, format string, a ...any) {
		got[name] = Check{Name: name, Status: status, Detail: fmt.Sprintf(format, a...)}
	}, st, "api")

	for name, want := range map[string]Status{
		"url api/api": StatusOK, "url api/rpc": StatusOK, "url api/queue": StatusOK, "url api/grpc": StatusOK,
		"url api/down": StatusWarn, "url api/edge": StatusOK,
	} {
		if got[name].Status != want {
			t.Errorf("%s = %+v, want %s", name, got[name], want)
		}
	}
	if _, probed := got["url api/db"]; probed {
		t.Error("a host with probe none must not be checked")
	}
	if !strings.Contains(got["url api/grpc"].Detail, "no gRPC client") || !strings.Contains(got["url api/down"].Detail, "does not accept connections") {
		t.Errorf("details: %q / %q", got["url api/grpc"].Detail, got["url api/down"].Detail)
	}
	for _, url := range sys.probed {
		if strings.Contains(url, "rpc-3") || strings.Contains(url, "queue-3") || strings.Contains(url, "grpc-3") {
			t.Errorf("sent HTTP to a host that does not speak it: %s", url)
		}
	}
}
