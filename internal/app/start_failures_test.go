package app_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/app"
	"github.com/cristiansteib/multiversectl/internal/domain"
)

// failingRuntime refuses to start the projects whose name ends in the given app.
type failingRuntime struct {
	*startRuntime
	failApp string
}

func (r failingRuntime) Up(ctx context.Context, req app.RuntimeRequest) error {
	if strings.HasSuffix(req.Project, "-"+r.failApp) {
		return errors.New("docker compose up: port is already allocated")
	}
	return r.startRuntime.Up(ctx, req)
}

type recordedCommands struct{ ran []string }

func (c *recordedCommands) RunHost(_ context.Context, h app.HostCommand) (string, error) {
	c.ran = append(c.ran, strings.Join(h.Args, " "))
	return "", nil
}

// twoAppRig has api (which fails to start) and worker, each with a postUp hook
// on the machine, and a hook of the multiverse's own.
func twoAppRig(t *testing.T) (*renderRig, *startRuntime, *recordedCommands) {
	t.Helper()
	r, rt := startRig(t, "{}")
	hook := func(name string) map[domain.HookEvent][]domain.Hook {
		return map[domain.HookEvent][]domain.Hook{domain.PostUp: {{Host: true, Command: []string{"echo", name}}}}
	}
	a := r.svc.Manifest.Apps["api"]
	a.Hooks = hook("api")
	r.svc.Manifest.Apps["api"] = a
	r.svc.Manifest.Apps["worker"] = domain.App{Repo: "api", Hooks: hook("worker")}
	r.svc.Manifest.Hooks = hook("multiverse")
	cmds := &recordedCommands{}
	r.svc.Commands = cmds
	r.svc.Runtime = failingRuntime{rt, "api"}
	return r, rt, cmds
}

func TestAnAppThatFailsDoesNotKeepTheOthersFromStarting(t *testing.T) {
	r, rt, cmds := twoAppRig(t)
	err := r.svc.Start(context.Background(), app.StartInput{Universe: "1"})
	var failed *app.StartFailed
	if !errors.As(err, &failed) || len(failed.Failed) != 1 || failed.Failed[0].App != "api" {
		t.Fatalf("err = %v", err)
	}
	if len(failed.Up) != 1 || failed.Up[0] != "worker" || !projectsWith(rt.ups, "worker") {
		t.Errorf("up = %v, started %v", failed.Up, rt.ups)
	}
	// The hooks of the app that started run; those of the one that did not, and
	// the multiverse's own (the universe is not whole), do not.
	if got := strings.Join(cmds.ran, ";"); got != "echo worker" {
		t.Errorf("hooks ran: %q", got)
	}
	out := r.out.text()
	for _, want := range []string{"up: worker", "failed: api: ", "port is already allocated"} {
		if !strings.Contains(out, want) {
			t.Errorf("summary lacks %q:\n%s", want, out)
		}
	}
}

func TestFailFastStopsAtTheFirstAppThatFails(t *testing.T) {
	r, rt, cmds := twoAppRig(t)
	err := r.svc.Start(context.Background(), app.StartInput{Universe: "1", FailFast: true})
	var failed *app.StartFailed
	if err == nil || errors.As(err, &failed) || !strings.Contains(err.Error(), "port is already allocated") {
		t.Fatalf("err = %v", err)
	}
	if projectsWith(rt.ups, "worker") || len(cmds.ran) != 0 {
		t.Errorf("went on: started %v, hooks %v", rt.ups, cmds.ran)
	}
}

func TestEveryAppUpRunsTheMultiverseHooksAsBefore(t *testing.T) {
	r, _, cmds := twoAppRig(t)
	r.svc.Runtime = r.svc.Runtime.(failingRuntime).startRuntime
	if err := r.svc.Start(context.Background(), app.StartInput{Universe: "1"}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(cmds.ran, ";"); got != "echo api;echo worker;echo multiverse" {
		t.Errorf("hooks ran: %q", got)
	}
}
