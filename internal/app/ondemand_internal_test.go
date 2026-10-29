package app

import (
	"context"
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

type idleRuntime struct {
	Runtime
	containers []Container
}

func (r idleRuntime) Containers(context.Context, string) ([]Container, error) {
	return r.containers, nil
}

func (idleRuntime) Inspect(context.Context, RuntimeRequest) (ProjectConfig, error) {
	return ProjectConfig{Services: map[string]ServiceConfig{"web": {}, "worker": {}}}, nil
}

type noFiles struct{ Files }

func (noFiles) Exists(string) bool { return false }

func onDemandService(rt Runtime) *Service {
	m := &domain.Manifest{Name: "acme", Repos: map[string]domain.Repo{"tool": {}}, Apps: map[string]domain.App{
		"tool": {Repo: "tool", OnDemand: true, Secrets: []string{".env"}},
	}}
	return New(Deps{Runtime: rt, Files: noFiles{}, System: fakeSystem{}, Out: discardOut{}}, "/ws", m, "test")
}

func TestDoctorShowsAnIdleAppOnDemandAsOffNotAsAFailure(t *testing.T) {
	svc := onDemandService(idleRuntime{})
	var got []Check
	svc.checkUniverse(context.Background(), func(name string, status Status, format string, a ...any) {
		got = append(got, Check{Name: name, Status: status, Detail: strings.TrimSpace(format)})
	}, domain.UniverseState{Name: "3", Slug: "3", Project: "acme-3"})
	if len(got) != 2 {
		t.Fatalf("checks = %+v", got)
	}
	for _, c := range got {
		if c.Status != StatusOK {
			t.Errorf("%s = %s (%s): an app that was never asked for is not a finding", c.Name, c.Status, c.Detail)
		}
	}
	if !strings.HasPrefix(got[1].Detail, "off, on demand") {
		t.Errorf("app line = %q", got[1].Detail)
	}
}

func TestDoctorStillCountsAnAppOnDemandThatIsRunning(t *testing.T) {
	probe := onDemandService(nil)
	project := probe.Universe("3").ProjectFor("tool")
	up := Container{Name: project + "-web-1", Project: project, Service: "web", Status: "Up 2 minutes"}
	svc := onDemandService(idleRuntime{containers: []Container{up}})
	var got []Check
	svc.checkUniverse(context.Background(), func(name string, status Status, format string, a ...any) {
		got = append(got, Check{Name: name, Status: status, Detail: strings.TrimSpace(format)})
	}, domain.UniverseState{Name: "3", Slug: "3", Project: "acme-3"})
	if got[0].Status != StatusFail {
		t.Errorf("a missing secret of a running app must fail: %+v", got[0])
	}
	if got[1].Detail != "%d running" {
		t.Errorf("app line = %q", got[1].Detail)
	}
}

func TestAnIdleAppOnDemandExpectsNoServices(t *testing.T) {
	svc := onDemandService(idleRuntime{})
	svc.Manifest.Apps["tool"] = domain.App{Repo: "tool", OnDemand: true}
	apps := svc.appSummaries(context.Background(), domain.UniverseState{Name: "3", Slug: "3", Project: "acme-3"}, nil, nil, ListOptions{Services: true})
	if len(apps) != 1 || !apps[0].OnDemand || apps[0].Expected != 0 {
		t.Fatalf("summary = %+v", apps)
	}
	if len(apps[0].Services) != 2 {
		t.Fatalf("services = %+v", apps[0].Services)
	}
	for _, sv := range apps[0].Services {
		if sv.State != StateOff {
			t.Errorf("%s is %s, want off", sv.Name, sv.State)
		}
	}
}

// profileRuntime defines celery's worker only while the celery profile is on.
type profileRuntime struct{ Runtime }

func (profileRuntime) Containers(context.Context, string) ([]Container, error) { return nil, nil }
func (profileRuntime) Inspect(_ context.Context, r RuntimeRequest) (ProjectConfig, error) {
	services := map[string]ServiceConfig{"web": {}}
	for _, p := range r.Profiles {
		if p == "celery" {
			services["worker"] = ServiceConfig{}
		}
	}
	return ProjectConfig{Services: services}, nil
}

// A group that is only a profile still says which flag starts its services.
func TestAProfileOnlyGroupsServicesAreListedAsOptional(t *testing.T) {
	m := &domain.Manifest{Name: "acme", Repos: map[string]domain.Repo{"api": {}}, Apps: map[string]domain.App{
		"api": {Repo: "api", Optional: map[string][]string{"celery": nil}},
	}}
	svc := New(Deps{Runtime: profileRuntime{}, Files: noFiles{}, System: fakeSystem{}, Out: discardOut{}}, "/ws", m, "test")
	u := svc.Universe("3")
	got := map[string]ServiceStatus{}
	for _, st := range svc.serviceStates(context.Background(), u, domain.UniverseState{Name: "3"}, "api", nil) {
		got[st.Name] = st
	}
	if w := got["worker"]; w.State != StateOptional || w.Detail != "--with celery" || got["web"].State != StateNotUp {
		t.Errorf("states = %+v", got)
	}
}
