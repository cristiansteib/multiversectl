package app_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/app"
)

type installConfig struct{ cfg app.Config }

func (c installConfig) Load() (app.Config, error) { return c.cfg, nil }
func (installConfig) Save(app.Config) error       { return nil }
func (installConfig) Path() string                { return "" }

// The name a multiverse is installed under reaches the compose project, and
// the manifest's own name does not change it.
func TestAnInstallUnderAnotherNameHasItsOwnComposeProjects(t *testing.T) {
	r, _ := startRig(t, "{}")
	root := r.svc.Multiverse
	r.svc.Config = installConfig{app.Config{Multiverses: map[string]app.MultiverseEntry{"acme": {Root: root}}}}
	same := app.New(r.svc.Deps, root, r.svc.Manifest, "test")
	if got := same.Universe("1").ProjectFor("api"); got != "acme-u1-api" {
		t.Errorf("default install: %q", got)
	}
	r.svc.Config = installConfig{app.Config{Multiverses: map[string]app.MultiverseEntry{"acme-copy": {Root: root}}}}
	alias := app.New(r.svc.Deps, root, r.svc.Manifest, "test")
	if got := alias.Universe("1").ProjectFor("api"); got != "acme-acme-copy-u1-api" {
		t.Errorf("alias install: %q", got)
	}
}

func foreign(r *renderRig, rt *startRuntime, dir string) {
	p := r.svc.Universe("1").ProjectFor("api")
	rt.running = []app.Container{{Name: p + "-web-1", Project: p, Service: "web", Status: "Up 1 minute", WorkingDir: dir, ConfigFile: filepath.Join(dir, "compose.yaml")}}
}

func TestStartStopAndRemoveLeaveAProjectOfAnotherRootAlone(t *testing.T) {
	ctx := context.Background()
	r, rt := startRig(t, "{}")
	foreign(r, rt, "/elsewhere/other-root/universes/1/api")

	err := r.svc.Start(ctx, app.StartInput{Universe: "1"})
	var fp *app.ForeignProject
	if !errors.As(err, &fp) || !strings.Contains(err.Error(), "/elsewhere/other-root") {
		t.Fatalf("start: %v", err)
	}
	if len(rt.ups) != 0 {
		t.Errorf("started %v on a project that is not this universe's", rt.ups)
	}
	if err := r.svc.Stop(ctx, app.StopInput{Universe: "1"}); !errors.As(err, &fp) {
		t.Errorf("stop: %v", err)
	}
	if len(rt.downs) != 0 {
		t.Errorf("brought down %v", rt.downs)
	}
}

func TestAProjectStartedFromThisUniverseIsActedOn(t *testing.T) {
	ctx := context.Background()
	r, rt := startRig(t, "{}")
	foreign(r, rt, r.repoDir)
	if err := r.svc.Start(ctx, app.StartInput{Universe: "1"}); err != nil {
		t.Fatal(err)
	}
	if err := r.svc.Stop(ctx, app.StopInput{Universe: "1"}); err != nil {
		t.Fatal(err)
	}
	if len(rt.ups) != 1 || len(rt.downs) == 0 {
		t.Errorf("ups %v downs %v", rt.ups, rt.downs)
	}
}
