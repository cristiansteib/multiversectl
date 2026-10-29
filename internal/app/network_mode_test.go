package app_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/app"
)

type inspectRuntime struct {
	*startRuntime
	cfg app.ProjectConfig
}

func (r inspectRuntime) Inspect(context.Context, app.RuntimeRequest) (app.ProjectConfig, error) {
	return r.cfg, nil
}

type captureRouter struct{ got *[]app.OverrideRequest }

func (c captureRouter) Materialize(_ context.Context, r app.OverrideRequest) ([]string, error) {
	*c.got = append(*c.got, r)
	return nil, nil
}

// A service in another network mode cannot be given a list of networks, and
// compose rejects the whole file for it: the route to it is left out, said, and
// the app still starts.
func TestAServiceWithNetworkModeIsNotRoutedAndTheAppStillStarts(t *testing.T) {
	r, rt := startRig(t, "{}")
	var seen []app.OverrideRequest
	r.svc.Router = captureRouter{&seen}
	r.svc.Runtime = inspectRuntime{rt, app.ProjectConfig{Services: map[string]app.ServiceConfig{"web": {NetworkMode: "host"}}}}
	if err := r.svc.Start(context.Background(), app.StartInput{Universe: "1"}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || len(seen[0].Routes) != 0 {
		t.Fatalf("routes handed to the router: %+v", seen)
	}
	if len(rt.ups) != 1 {
		t.Errorf("the app was not started: %v", rt.ups)
	}
	out := r.out.text()
	for _, want := range []string{`warn: api: service "web" sets network_mode "host"`, "api-u1.example.test", "cannot be reached through the proxy"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestAServiceOnTheDefaultNetworkIsStillRouted(t *testing.T) {
	r, _ := startRig(t, "{}")
	var seen []app.OverrideRequest
	r.svc.Router = captureRouter{&seen}
	if err := r.svc.Start(context.Background(), app.StartInput{Universe: "1"}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || len(seen[0].Routes) == 0 {
		t.Fatalf("routes: %+v", seen)
	}
}
