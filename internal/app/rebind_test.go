package app_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/app"
	"github.com/cristiansteib/multiversectl/internal/domain"
)

// rebindRig is a start whose multiverse is published on the loopback address
// while the running shared proxy is on every interface, and another multiverse
// is installed beside it.
func rebindRig(t *testing.T, otherRunning bool) (*renderRig, *startRuntime) {
	t.Helper()
	r, rt := startRig(t, "{}")
	other := &domain.Manifest{Name: "beta", Path: "/other/multiverse.yaml"}
	r.svc.Manifests = fakeLoader{other.Path: other}
	r.svc.Config = installConfig{app.Config{Multiverses: map[string]app.MultiverseEntry{
		"acme": {Root: r.svc.Multiverse, Manifest: r.svc.Manifest.Path},
		"beta": {Root: "/other", Manifest: other.Path},
	}}}
	if otherRunning {
		rt.running = []app.Container{{Name: "beta-u1-api-web-1", Project: "beta-u1-api", Service: "web", Status: "Up 2 minutes",
			WorkingDir: "/other/universes/1/api", ConfigFile: "/other/universes/1/api/compose.yaml"}}
	}
	return r, rt
}

func TestStartingDoesNotRebindTheSharedProxyUnderAnotherMultiversesUniverses(t *testing.T) {
	r, rt := rebindRig(t, true)
	err := r.svc.Start(context.Background(), app.StartInput{Universe: "1"})
	if err == nil || !strings.Contains(err.Error(), "published on 127.0.0.1 instead of 0.0.0.0") ||
		!strings.Contains(err.Error(), "beta") || !strings.Contains(err.Error(), "--rebind-proxy") {
		t.Fatalf("err = %v", err)
	}
	if len(rt.ups) != 0 {
		t.Errorf("started %v", rt.ups)
	}

	if err := r.svc.Start(context.Background(), app.StartInput{Universe: "1", RebindProxy: true}); err != nil {
		t.Fatalf("with --rebind-proxy: %v", err)
	}
	if !strings.Contains(r.out.text(), "the shared proxy is being reconfigured") {
		t.Errorf("no warning:\n%s", r.out.text())
	}
}

func TestWithNothingElseRunningTheProxyIsRebound(t *testing.T) {
	r, rt := rebindRig(t, false)
	if err := r.svc.Start(context.Background(), app.StartInput{Universe: "1"}); err != nil {
		t.Fatal(err)
	}
	if len(rt.ups) != 1 || !strings.Contains(r.out.text(), "warn: the shared proxy is being reconfigured") {
		t.Errorf("started %v:\n%s", rt.ups, r.out.text())
	}
}
