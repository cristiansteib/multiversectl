package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

type probeSystem struct {
	System
	answers map[string]int
}

func (p probeSystem) Probe(_ context.Context, url string) (int, error) {
	if status, ok := p.answers[url]; ok {
		return status, nil
	}
	return 0, errors.New("connection refused")
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
