package app_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cristiansteib/multiversectl/internal/app"
	"github.com/cristiansteib/multiversectl/internal/domain"
)

// readySys answers the probes of a start as the test says.
type readySys struct {
	renderSys
	probes []string
	dials  []string
	status func(url string) int // 0 is a refused connection
	dialOK func(address string) bool
}

func (s *readySys) Probe(_ context.Context, url string) (app.Answer, error) {
	s.probes = append(s.probes, url)
	if st := s.status(url); st != 0 {
		return app.Answer{Status: st}, nil
	}
	return app.Answer{}, errors.New("connection refused")
}

func (s *readySys) Dial(_ context.Context, address string) error {
	s.dials = append(s.dials, address)
	if s.dialOK != nil && s.dialOK(address) {
		return nil
	}
	return errors.New("dial tcp " + address + ": connection refused")
}

func readyRig(t *testing.T, ready, scheme string) (*renderRig, *startRuntime, *readySys, *recordedCommands) {
	t.Helper()
	t.Cleanup(app.PollFast())
	r, rt := startRig(t, "{}")
	a := r.svc.Manifest.Apps["api"]
	a.Hosts = map[string]domain.HostRule{"api": {Service: "web", Port: 80, Entrypoint: "web", Ready: ready, Scheme: scheme}}
	a.Hooks = map[domain.HookEvent][]domain.Hook{domain.PostUp: {{Host: true, Command: []string{"after-ready"}}}}
	r.svc.Manifest.Apps["api"] = a
	sys := &readySys{renderSys: renderSys{}}
	cmds := &recordedCommands{}
	r.svc.System, r.svc.Commands = sys, cmds
	return r, rt, sys, cmds
}

func TestUpWaitsForAHealthcheckBeforeTheHooksRun(t *testing.T) {
	r, rt, _, cmds := readyRig(t, "", "")
	rt.health = [][]app.ServiceHealth{
		{{Service: "web", State: "running", Health: "starting"}},
		{{Service: "web", State: "running", Health: "starting"}},
		{{Service: "web", State: "running", Health: "healthy"}},
	}
	if err := r.svc.Start(context.Background(), app.StartInput{Universe: "1"}); err != nil {
		t.Fatal(err)
	}
	if rt.asked < 3 {
		t.Errorf("asked %d times, want it to poll until healthy", rt.asked)
	}
	if !strings.Contains(r.out.text(), "waiting for web to be healthy (starting)") || len(cmds.ran) != 1 {
		t.Errorf("output:\n%s\nhooks %v", r.out.text(), cmds.ran)
	}
}

func TestAServiceWithoutAHealthcheckIsNotWaitedFor(t *testing.T) {
	r, rt, _, _ := readyRig(t, "", "")
	rt.health = [][]app.ServiceHealth{{{Service: "web", State: "running"}}}
	if err := r.svc.Start(context.Background(), app.StartInput{Universe: "1"}); err != nil {
		t.Fatal(err)
	}
	if rt.asked != 1 || strings.Contains(r.out.text(), "waiting") {
		t.Errorf("asked %d:\n%s", rt.asked, r.out.text())
	}
}

func TestATimeoutIsAWarningNamingWhatWasWaitedForUnlessStrict(t *testing.T) {
	for _, strict := range []bool{false, true} {
		r, rt, _, cmds := readyRig(t, "", "")
		rt.health = [][]app.ServiceHealth{{{Service: "web", State: "running", Health: "unhealthy"}}}
		err := r.svc.Start(context.Background(), app.StartInput{Universe: "1",
			Wait: app.WaitOptions{Timeout: 20 * time.Millisecond, Strict: strict}})
		if strict {
			if err == nil || !strings.Contains(err.Error(), "service web is still unhealthy") || len(cmds.ran) != 0 {
				t.Errorf("strict: err = %v, hooks %v", err, cmds.ran)
			}
			continue
		}
		if err != nil {
			t.Fatalf("a timeout must not fail the start: %v", err)
		}
		if !strings.Contains(r.out.text(), "warn: api: not ready after") || !strings.Contains(r.out.text(), "service web is still unhealthy") || len(cmds.ran) != 1 {
			t.Errorf("output:\n%s\nhooks %v", r.out.text(), cmds.ran)
		}
	}
}

func TestAHostWithReadyIsPolledUntilItAnswersBelow500(t *testing.T) {
	r, _, sys, cmds := readyRig(t, "true", "")
	n := 0
	sys.status = func(url string) int {
		n++
		switch n {
		case 1:
			return 0
		case 2:
			return 503
		}
		return 401
	}
	if err := r.svc.Start(context.Background(), app.StartInput{Universe: "1"}); err != nil {
		t.Fatal(err)
	}
	if len(sys.probes) != 3 || sys.probes[0] != "http://api-u1.example.test" {
		t.Errorf("probed %v", sys.probes)
	}
	out := r.out.text()
	if !strings.Contains(out, "waiting for http://api-u1.example.test to answer (connection refused)") || !strings.Contains(out, "api: http://api-u1.example.test answers") || len(cmds.ran) != 1 {
		t.Errorf("output:\n%s", out)
	}
}

func TestAReadyPathIsAskedAndAnH2cHostIsOnlyDialled(t *testing.T) {
	r, _, sys, _ := readyRig(t, "/healthz", "")
	sys.status = func(string) int { return 200 }
	if err := r.svc.Start(context.Background(), app.StartInput{Universe: "1"}); err != nil {
		t.Fatal(err)
	}
	if len(sys.probes) == 0 || sys.probes[0] != "http://api-u1.example.test/healthz" {
		t.Errorf("probed %v", sys.probes)
	}

	r, _, sys, _ = readyRig(t, "true", "h2c")
	sys.dialOK = func(string) bool { return true }
	if err := r.svc.Start(context.Background(), app.StartInput{Universe: "1"}); err != nil {
		t.Fatal(err)
	}
	if len(sys.probes) != 0 || len(sys.dials) != 1 || sys.dials[0] != "api-u1.example.test:80" {
		t.Errorf("probed %v, dialled %v", sys.probes, sys.dials)
	}
}

func TestNoWaitAndAHostWithoutReadyAskNothing(t *testing.T) {
	r, rt, sys, _ := readyRig(t, "true", "")
	sys.status = func(string) int { return 0 }
	rt.health = [][]app.ServiceHealth{{{Service: "web", Health: "starting"}}}
	if err := r.svc.Start(context.Background(), app.StartInput{Universe: "1", Wait: app.WaitOptions{Disabled: true}}); err != nil {
		t.Fatal(err)
	}
	if rt.asked != 0 || len(sys.probes) != 0 {
		t.Errorf("--no-wait asked: health %d, probes %v", rt.asked, sys.probes)
	}

	r, rt, sys, _ = readyRig(t, "", "")
	if err := r.svc.Start(context.Background(), app.StartInput{Universe: "1"}); err != nil {
		t.Fatal(err)
	}
	if len(sys.probes) != 0 || len(sys.dials) != 0 {
		t.Errorf("a host that did not ask was probed: %v %v", sys.probes, sys.dials)
	}
}
