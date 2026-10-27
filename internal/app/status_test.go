package app_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/app"
	"github.com/cristiansteib/multiversectl/internal/domain"
)

// installedRig has one project installed from a repository, its manifest
// cloned under the project root and listing two repositories, one cloned.
func installedRig(t *testing.T) (*rig, string) {
	t.Helper()
	git := &fakeRepos{}
	r := newRig(t, git)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".multiverse", "source"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, ".multiverse", "source", "multiverse.yaml")
	r.loader[manifest] = &domain.Manifest{Name: "acme", Repos: map[string]domain.Repo{"api": {}, "billing-api": {}}}
	r.cfg.c.Multiverses = map[string]app.MultiverseEntry{"acme": {
		Root: root, Manifest: manifest,
		Source: app.Origin{Repo: "https://github.com/acme/multiverse.git", Path: "multiverse.yaml"},
	}}
	return r, root
}

func TestStatusSaysWhereAMultiverseHasDrifted(t *testing.T) {
	r, _ := installedRig(t)
	r.git.behind = 2
	rep, err := r.Status(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Multiverses) != 1 {
		t.Fatalf("%+v", rep)
	}
	w := rep.Multiverses[0]
	notes := strings.Join(w.Notes, " | ")
	if !w.Stale || !strings.Contains(notes, "manifest behind 2 (as of the last fetch)") || !strings.Contains(notes, "billing-api not cloned") {
		t.Errorf("%+v", w)
	}
	if rep.Next != "multiversectl catalog update acme" {
		t.Errorf("next = %q", rep.Next)
	}
	if len(r.git.fetched) != 0 {
		t.Errorf("status without --fetch must not touch the network: %v", r.git.fetched)
	}

	r.git.behind = 0
	if err := os.MkdirAll(filepath.Join(w.Root, "billing-api"), 0o755); err != nil {
		t.Fatal(err)
	}
	rep, _ = r.Status(context.Background(), true)
	if rep.Multiverses[0].Stale || len(rep.Multiverses[0].Notes) != 0 || rep.Next != "" {
		t.Errorf("up to date: %+v next=%q", rep.Multiverses[0], rep.Next)
	}
	if len(r.git.fetched) == 0 {
		t.Error("--fetch fetches")
	}
}

func TestStatusNoticesWhenTheCatalogMovedOn(t *testing.T) {
	r, _ := installedRig(t)
	e := r.cfg.c.Multiverses["acme"]
	e.From = "acme"
	r.cfg.c.Multiverses["acme"] = e
	rep, _ := r.Status(context.Background(), false)
	// The root catalog is empty, so "acme" is no longer in it.
	if !strings.Contains(strings.Join(rep.Multiverses[0].Notes, "|"), "no longer in your catalog") {
		t.Errorf("notes = %v", rep.Multiverses[0].Notes)
	}
}

func TestStatusRemindsAboutALocalOnlyRoot(t *testing.T) {
	r := newRig(t, &fakeRepos{inRepo: true})
	rep, err := r.Status(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Catalog.Local || !rep.Catalog.NoRemote {
		t.Errorf("root = %+v; a root with no remote is lost with the machine", rep.Catalog)
	}
}

func TestStatusOnAFreshMachineSaysHowToBegin(t *testing.T) {
	r := newRig(t, &fakeRepos{})
	r.cfg.c = app.Config{}
	rep, _ := r.Status(context.Background(), false)
	if !strings.Contains(rep.Next, "multiversectl start <owner/repo>") || !strings.Contains(rep.Next, "multiversectl init") {
		t.Errorf("next = %q", rep.Next)
	}
}

func TestRefreshMovesTheManifestForwardAndOnlyThen(t *testing.T) {
	r, _ := installedRig(t)
	e := r.cfg.c.Multiverses["acme"]

	res, err := r.RefreshMultiverse(context.Background(), "acme", e)
	if err != nil || res.Changed || len(r.git.ffed) != 0 {
		t.Fatalf("up to date: res=%+v err=%v ffed=%v", res, err, r.git.ffed)
	}
	r.git.behind = 3
	res, err = r.RefreshMultiverse(context.Background(), "acme", e)
	if err != nil || !res.Changed || !strings.Contains(res.Detail, "3") || len(r.git.ffed) != 1 || r.git.ffed[0] != "main" {
		t.Fatalf("behind: res=%+v err=%v ffed=%v", res, err, r.git.ffed)
	}

	file := r.cfg.c.Multiverses["acme"]
	file.Source = app.Origin{}
	if res, _ := r.RefreshMultiverse(context.Background(), "acme", file); res.Skipped == "" {
		t.Error("a project installed from a file has nothing to update from")
	}
}
