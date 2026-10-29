package app_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/app"
	"github.com/cristiansteib/multiversectl/internal/domain"
)

// onDemandRig is a universe with the app "api" and, in the same repository, the
// app "tool" that does not start unless it is named.
func onDemandRig(t *testing.T) (*renderRig, *startRuntime) {
	t.Helper()
	r, rt := startRig(t, "{}")
	r.svc.Manifest.Apps["tool"] = domain.App{Repo: "api", OnDemand: true,
		Hosts: map[string]domain.HostRule{"tool": {Service: "web", Port: 80, Entrypoint: "web"}}}
	return r, rt
}

type savedStore struct {
	app.Store
	last domain.UniverseState
}

func (s *savedStore) Save(st domain.UniverseState) error { s.last = st; return nil }

func projectsWith(projects []string, app string) bool {
	for _, p := range projects {
		if strings.HasSuffix(p, "-"+app) {
			return true
		}
	}
	return false
}

func TestAStartThatNamesNoAppLeavesOutTheOnesOnDemandAndSaysSo(t *testing.T) {
	r, rt := onDemandRig(t)
	saved := &savedStore{Store: r.svc.Store}
	r.svc.Store = saved
	if err := r.svc.Start(context.Background(), app.StartInput{Universe: "1"}); err != nil {
		t.Fatal(err)
	}
	if len(rt.ups) != 1 || !projectsWith(rt.ups, "api") {
		t.Errorf("started %v: want only api", rt.ups)
	}
	if !strings.Contains(r.out.text(), "tool: on demand, not started (start it with: multiversectl universe up 1 tool)") {
		t.Errorf("no note about tool:\n%s", r.out.text())
	}
	// Its address is allocated all the same, so starting it later and other apps
	// that refer to it need nothing more.
	if rule, ok := saved.last.Rules["tool"]; !ok || rule.App != "tool" || rule.Hostname == "" {
		t.Errorf("rules = %+v: want tool's address", saved.last.Rules)
	}
}

func TestAnAppOnDemandStartsWhenItIsNamed(t *testing.T) {
	r, rt := onDemandRig(t)
	if err := r.svc.Start(context.Background(), app.StartInput{Universe: "1", Apps: []string{"tool"}}); err != nil {
		t.Fatal(err)
	}
	if len(rt.ups) != 1 || !projectsWith(rt.ups, "tool") {
		t.Errorf("started %v: want only tool", rt.ups)
	}
	if strings.Contains(r.out.text(), "on demand") {
		t.Errorf("said it left tool out though it was named:\n%s", r.out.text())
	}
}

func TestDownStillStopsAnAppOnDemandThatIsRunningAndLeavesAnIdleOneAlone(t *testing.T) {
	r, rt := onDemandRig(t)
	ctx := context.Background()
	if err := r.svc.Stop(ctx, app.StopInput{Universe: "1"}); err != nil {
		t.Fatal(err)
	}
	if !projectsWith(rt.downs, "api") || projectsWith(rt.downs, "tool") {
		t.Errorf("idle: brought down %v, want api only", rt.downs)
	}

	rt.downs = nil
	if err := r.svc.Stop(ctx, app.StopInput{Universe: "1", Apps: []string{"tool"}}); err != nil {
		t.Fatal(err)
	}
	if !projectsWith(rt.downs, "tool") {
		t.Errorf("named: brought down %v, want tool", rt.downs)
	}

	rt.downs = nil
	u := r.svc.Universe("1")
	rt.running = []app.Container{{Name: u.ProjectFor("tool") + "-web-1", Project: u.ProjectFor("tool"), Service: "web", Status: "Up 1 minute"}}
	if err := r.svc.Stop(ctx, app.StopInput{Universe: "1"}); err != nil {
		t.Fatal(err)
	}
	if !projectsWith(rt.downs, "tool") || !projectsWith(rt.downs, "api") {
		t.Errorf("running: brought down %v, want both", rt.downs)
	}
}
