package app_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/adapters/catalog"
	"github.com/cristiansteib/multiversectl/internal/app"
	"github.com/cristiansteib/multiversectl/internal/domain"
)

func wsRef(t *testing.T, e catalog.Editor, data []byte, name, repo, path string) []byte {
	t.Helper()
	out, err := e.AddWorkspaceRef(data, domain.WorkspaceRef{Name: name, Source: domain.Source{Repo: repo, Path: path}})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func plan(t *testing.T, r *rig, args []string, o app.InstallOptions) []app.InstallTarget {
	t.Helper()
	if o.Base == "" {
		o.Base = "/ws"
	}
	got, err := r.PlanInstall(context.Background(), args, o)
	if err != nil {
		t.Fatalf("PlanInstall(%v): %v", args, err)
	}
	return got
}

func TestInstallFromACatalogNodeTakesEverythingUnderIt(t *testing.T) {
	r := newRig(t, &fakeRepos{})
	e := catalog.Editor{}
	data := e.Scaffold("me")
	data, _ = e.AddCatalogRef(data, domain.CatalogRef{Name: "acme", Source: domain.Source{Path: "catalogs/acme.yaml"}})
	write(t, r.root, "catalog.yaml", string(data))
	sub := wsRef(t, e, wsRef(t, e, e.Scaffold("acme"), "api", "git@example.test:acme/api.git", ""), "web", "git@example.test:acme/web.git", "")
	write(t, r.root, "catalogs/acme.yaml", string(sub))

	got := plan(t, r, []string{"acme"}, app.InstallOptions{})
	if len(got) != 2 || got[0].Name != "api" || got[1].Name != "web" {
		t.Fatalf("targets = %+v", got)
	}
	if got[0].Root != "/ws/api" || got[0].From != "acme/api" || got[0].ManifestSource != "git@example.test:acme/api.git" {
		t.Errorf("target = %+v", got[0])
	}
	if !strings.Contains(got[0].How, "in your catalog") {
		t.Errorf("how = %q", got[0].How)
	}
}

func TestInstallNamesAreLeafNamesAndCollisionsGetTheParentInFront(t *testing.T) {
	r := newRig(t, &fakeRepos{})
	e := catalog.Editor{}
	root := e.Scaffold("me")
	root, _ = e.AddCatalogRef(root, domain.CatalogRef{Name: "payments", Source: domain.Source{Path: "p.yaml"}})
	root, _ = e.AddCatalogRef(root, domain.CatalogRef{Name: "backend", Source: domain.Source{Path: "b.yaml"}})
	write(t, r.root, "catalog.yaml", string(root))
	write(t, r.root, "p.yaml", string(wsRef(t, e, e.Scaffold("payments"), "billing", "git@example.test:acme/pay-billing.git", "")))
	write(t, r.root, "b.yaml", string(wsRef(t, e, e.Scaffold("backend"), "billing", "git@example.test:acme/be-billing.git", "")))

	got := plan(t, r, []string{"payments", "backend"}, app.InstallOptions{})
	if len(got) != 2 || got[0].Name != "billing" || got[1].Name != "backend-billing" {
		t.Fatalf("two different billings must not become one name, and must not be skipped: %+v", got)
	}
}

func TestInstallSkipsWhatIsAlreadyHereEvenIfTheNodeWasRenamed(t *testing.T) {
	r := newRig(t, &fakeRepos{})
	e := catalog.Editor{}
	write(t, r.root, "catalog.yaml", string(wsRef(t, e, e.Scaffold("me"), "billing", "git@example.test:acme/billing.git", "")))
	r.cfg.c.Workspaces = map[string]app.WorkspaceEntry{
		"old-name": {Root: "/x", From: "was/renamed", Source: app.Origin{Repo: "git@example.test:acme/billing.git", Path: "multiverse.yaml"}},
	}
	got := plan(t, r, []string{"billing"}, app.InstallOptions{})
	if len(got) != 1 || !strings.Contains(got[0].Skip, "already installed as old-name") {
		t.Fatalf("got %+v", got)
	}
}

func TestInstallAProjectNeedsNoRootCatalog(t *testing.T) {
	proj := manifestRepo(t, "acme")
	e := catalog.Editor{}
	write(t, proj, "catalog.yaml", string(wsRef(t, e, e.Scaffold("acme"), "acme", "", "multiverse.yaml")))
	git := &fakeRepos{repos: map[string]string{"https://github.com/acme/multiverse.git": proj}}
	r := newRig(t, git)
	r.cfg.c = app.Config{}

	got := plan(t, r, []string{"acme/multiverse"}, app.InstallOptions{})
	if len(got) != 1 || got[0].Name != "acme" || got[0].ManifestSource != "https://github.com/acme/multiverse.git" || got[0].ManifestPath != "multiverse.yaml" {
		t.Fatalf("got %+v", got)
	}
	if got[0].From != "" {
		t.Errorf("not from your catalog, so no catalog path: %q", got[0].From)
	}
	if !strings.Contains(got[0].How, "with a catalog") {
		t.Errorf("how = %q", got[0].How)
	}
}

func TestInstallARepositoryWithSeveralWorkspacesNeedsAllOrAChoice(t *testing.T) {
	proj := t.TempDir()
	e := catalog.Editor{}
	data := wsRef(t, e, wsRef(t, e, e.Scaffold("acme"), "api", "git@example.test:acme/api.git", ""), "web", "git@example.test:acme/web.git", "")
	write(t, proj, "catalog.yaml", string(data))
	git := &fakeRepos{repos: map[string]string{"https://github.com/acme/multiverse.git": proj}}
	r := newRig(t, git)
	r.cfg.c = app.Config{}

	if _, err := r.PlanInstall(context.Background(), []string{"acme/multiverse"}, app.InstallOptions{Base: "/ws"}); err == nil || !strings.Contains(err.Error(), "--all") {
		t.Errorf("without a terminal the error should name --all: %v", err)
	}
	if got := plan(t, r, []string{"acme/multiverse"}, app.InstallOptions{All: true}); len(got) != 2 {
		t.Errorf("--all: %+v", got)
	}
	r.prompt.multi = []int{1}
	if got := plan(t, r, []string{"acme/multiverse"}, app.InstallOptions{}); len(got) != 1 || got[0].Name != "web" {
		t.Errorf("a choice: %+v", got)
	}
	r.prompt.multi = []int{}
	if _, err := r.PlanInstall(context.Background(), []string{"acme/multiverse"}, app.InstallOptions{Base: "/ws"}); err != app.ErrCancelled {
		t.Errorf("Enter alone backs out: %v", err)
	}
}

func TestInstallAManifestFileOrDirectoryFromThisMachine(t *testing.T) {
	dir := manifestRepo(t, "solo")
	r := newRig(t, &fakeRepos{})
	r.cfg.c = app.Config{}
	for _, arg := range []string{dir, filepath.Join(dir, "multiverse.yaml")} {
		got := plan(t, r, []string{arg}, app.InstallOptions{Root: "/home/me/solo"})
		if len(got) != 1 || got[0].Name != "solo" || got[0].Root != "/home/me/solo" ||
			got[0].ManifestSource != filepath.Join(dir, "multiverse.yaml") || got[0].Origin.Repo != "" {
			t.Errorf("%s: %+v", arg, got)
		}
	}
}

func TestInstallReadsItsArgumentInAFixedOrder(t *testing.T) {
	r := newRig(t, &fakeRepos{})
	e := catalog.Editor{}
	write(t, r.root, "catalog.yaml", string(wsRef(t, e, e.Scaffold("me"), "billing", "git@example.test:acme/billing.git", "")))

	// A catalog node wins over a directory of the same name.
	wd, _ := os.Getwd()
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "billing"), 0o755); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	os.Chdir(tmp)
	got := plan(t, r, []string{"billing"}, app.InstallOptions{})
	if len(got) != 1 || !strings.Contains(got[0].How, "in your catalog") {
		t.Errorf("catalog first: %+v", got)
	}

	// Nothing matches, and the repository cannot be cloned: the error says so.
	_, err := r.PlanInstall(context.Background(), []string{"acme/ghost"}, app.InstallOptions{Base: "/ws"})
	if err == nil || !strings.Contains(err.Error(), "not in your catalog") || !strings.Contains(err.Error(), "no access or not found") {
		t.Errorf("err = %v", err)
	}
}

func TestInstallNameAndRootNameOneWorkspace(t *testing.T) {
	r := newRig(t, &fakeRepos{})
	e := catalog.Editor{}
	data := wsRef(t, e, wsRef(t, e, e.Scaffold("me"), "a", "git@example.test:acme/a.git", ""), "b", "git@example.test:acme/b.git", "")
	write(t, r.root, "catalog.yaml", string(data))
	if _, err := r.PlanInstall(context.Background(), []string{"a", "b"}, app.InstallOptions{Base: "/ws", Name: "x"}); err == nil {
		t.Error("--name with two workspaces must be refused")
	}
	if got := plan(t, r, []string{"a"}, app.InstallOptions{Name: "mine"}); got[0].Name != "mine" || got[0].Root != "/ws/mine" {
		t.Errorf("%+v", got)
	}
}

func TestInstallPickerListsTheTreeAndMarksInstalled(t *testing.T) {
	r := newRig(t, &fakeRepos{})
	e := catalog.Editor{}
	write(t, r.root, "catalog.yaml", string(wsRef(t, e, wsRef(t, e, e.Scaffold("me"), "a", "git@example.test:acme/a.git", ""), "b", "git@example.test:acme/b.git", "")))
	r.cfg.c.Workspaces = map[string]app.WorkspaceEntry{"a": {From: "a", Source: app.Origin{Repo: "git@example.test:acme/a.git", Path: "multiverse.yaml"}}}
	r.prompt.multi = []int{2}
	got := plan(t, r, nil, app.InstallOptions{})
	if len(got) != 1 || got[0].Name != "b" {
		t.Fatalf("%+v", got)
	}
}
