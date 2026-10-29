package app

import (
	"context"
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// partialRig is a universe of four apps: api (always started), tool (on demand),
// docs (a repository with nothing to run) and ui (which runs only its web
// service, and has a worker it leaves out).
func partialRig(t *testing.T) (*Service, domain.UniverseState) {
	t.Helper()
	m := &domain.Manifest{Name: "acme", Repos: map[string]domain.Repo{"api": {}, "tool": {}, "docs": {}, "ui": {}}, Apps: map[string]domain.App{
		"api":  {Repo: "api"},
		"tool": {Repo: "tool", OnDemand: true},
		"docs": {Repo: "docs", Derived: true},
		"ui":   {Repo: "ui", Services: []string{"web"}, Optional: map[string][]string{"extras": {"mailer"}}},
	}}
	svc := New(Deps{Runtime: partialRuntime{}, Files: noFilesExceptCompose{}, System: fakeSystem{}, Out: discardOut{}}, "/ws", m, "test")
	return svc, domain.UniverseState{Name: "3", Slug: "u3", Project: "acme-u3"}
}

type partialRuntime struct{ Runtime }

func (partialRuntime) Inspect(_ context.Context, r RuntimeRequest) (ProjectConfig, error) {
	if strings.HasSuffix(r.Project, "-ui") {
		return ProjectConfig{Services: map[string]ServiceConfig{"web": {}, "worker": {}, "mailer": {}}}, nil
	}
	return ProjectConfig{Services: map[string]ServiceConfig{"web": {}, "worker": {}}}, nil
}

// noFilesExceptCompose has the compose file of every repository but docs.
type noFilesExceptCompose struct{ Files }

func (noFilesExceptCompose) Exists(p string) bool { return !strings.Contains(p, "/docs") }

func containerOf(svc *Service, app, service, status string) Container {
	project := svc.Universe("3").ProjectFor(app)
	return Container{Name: project + "-" + service + "-1", Project: project, Service: service, Status: status}
}

func summaryOf(t *testing.T, svc *Service, st domain.UniverseState, containers ...Container) Summary {
	t.Helper()
	apps := svc.appSummaries(context.Background(), st, containers, nil, ListOptions{Services: true})
	return Summary{Name: "3", Apps: apps}
}

func TestAServiceThatFailedAndOneThatWasStoppedAreToldApart(t *testing.T) {
	svc, st := partialRig(t)
	sum := summaryOf(t, svc, st,
		containerOf(svc, "api", "web", "Up 5 minutes"),
		containerOf(svc, "api", "worker", "Exited (1) 2 minutes ago"),
		containerOf(svc, "ui", "web", "Up 5 minutes"),
	)
	got := sum.Shortfalls()
	if len(got) != 1 || got[0].App != "api" || got[0].Service != "worker" || got[0].Why != "failed" || !strings.Contains(got[0].Detail, "Exited (1)") {
		t.Fatalf("shortfalls = %+v", got)
	}

	sum = summaryOf(t, svc, st,
		containerOf(svc, "api", "web", "Up 5 minutes"),
		containerOf(svc, "api", "worker", "Exited (0) 2 minutes ago"),
		containerOf(svc, "ui", "web", "Up 5 minutes"),
	)
	if got := sum.Shortfalls(); len(got) != 1 || got[0].Why != "stopped" {
		t.Fatalf("a clean exit is a stop: %+v", got)
	}
}

func TestAServiceNeverStartedIsNamedAsSuch(t *testing.T) {
	svc, st := partialRig(t)
	sum := summaryOf(t, svc, st, containerOf(svc, "api", "web", "Up 1 minute"), containerOf(svc, "ui", "web", "Up 1 minute"))
	got := sum.Shortfalls()
	if len(got) != 1 || got[0].Service != "worker" || got[0].Why != "not started" {
		t.Fatalf("shortfalls = %+v", got)
	}
}

// What is off on purpose is not expected, so it is not why a universe is partial.
func TestAppsOnDemandWithNoRuntimeAndServicesOffByChoiceAreNotExpected(t *testing.T) {
	svc, st := partialRig(t)
	sum := summaryOf(t, svc, st,
		containerOf(svc, "api", "web", "Up 5 minutes"),
		containerOf(svc, "api", "worker", "Up 5 minutes"),
		containerOf(svc, "ui", "web", "Up 5 minutes"),
		// An earlier start of the tool left its container behind, stopped.
		containerOf(svc, "tool", "web", "Exited (0) 3 days ago"),
	)
	if got := sum.Shortfalls(); len(got) != 0 {
		t.Fatalf("nothing is missing, but: %+v", got)
	}
	expected := 0
	for _, a := range sum.Apps {
		expected += a.Expected
	}
	if expected != 3 { // api web+worker, ui web; not tool, not docs, not ui's worker or mailer
		t.Errorf("expected = %d", expected)
	}
	skipped := sum.Skipped()
	if skipped["tool"] != "on demand" || !strings.HasPrefix(skipped["docs"], "no runtime") || len(skipped) != 2 {
		t.Errorf("skipped = %v", skipped)
	}
}

func TestAnAppOnDemandThatRunsIsExpectedToBeWhole(t *testing.T) {
	svc, st := partialRig(t)
	sum := summaryOf(t, svc, st,
		containerOf(svc, "api", "web", "Up 5 minutes"), containerOf(svc, "api", "worker", "Up 5 minutes"),
		containerOf(svc, "ui", "web", "Up 5 minutes"),
		containerOf(svc, "tool", "web", "Up 1 minute"),
		containerOf(svc, "tool", "worker", "Exited (2) 10 seconds ago"),
	)
	got := sum.Shortfalls()
	if len(got) != 1 || got[0].App != "tool" || got[0].Service != "worker" || got[0].Why != "failed" {
		t.Fatalf("shortfalls = %+v", got)
	}
	if _, listed := sum.Skipped()["tool"]; listed {
		t.Error("a running tool is not skipped")
	}
}
