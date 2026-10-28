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
	out, err := e.AddMultiverseRef(data, domain.MultiverseRef{Name: name, Source: domain.Source{Repo: repo, Path: path}})
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

func TestInstallNamesAreLeafNamesAndACollisionIsRefused(t *testing.T) {
	r := newRig(t, &fakeRepos{})
	e := catalog.Editor{}
	root := e.Scaffold("me")
	root, _ = e.AddCatalogRef(root, domain.CatalogRef{Name: "payments", Source: domain.Source{Path: "p.yaml"}})
	root, _ = e.AddCatalogRef(root, domain.CatalogRef{Name: "backend", Source: domain.Source{Path: "b.yaml"}})
	write(t, r.root, "catalog.yaml", string(root))
	write(t, r.root, "p.yaml", string(wsRef(t, e, e.Scaffold("payments"), "billing", "git@example.test:acme/pay-billing.git", "")))
	write(t, r.root, "b.yaml", string(wsRef(t, e, e.Scaffold("backend"), "billing", "git@example.test:acme/be-billing.git", "")))

	_, err := r.PlanInstall(context.Background(), []string{"payments", "backend"}, app.InstallOptions{})
	if err == nil || !strings.Contains(err.Error(), `"billing" is already taken`) || !strings.Contains(err.Error(), "names are unique") {
		t.Fatalf("two different billings must not share a name, and the refusal says why: %v", err)
	}
}

func TestInstallSkipsWhatIsAlreadyHereEvenIfTheNodeWasRenamed(t *testing.T) {
	r := newRig(t, &fakeRepos{})
	e := catalog.Editor{}
	write(t, r.root, "catalog.yaml", string(wsRef(t, e, e.Scaffold("me"), "billing", "git@example.test:acme/billing.git", "")))
	r.cfg.c.Multiverses = map[string]app.MultiverseEntry{
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

func TestInstallARepositoryWithSeveralMultiversesNeedsANameOrAChoice(t *testing.T) {
	proj := t.TempDir()
	e := catalog.Editor{}
	data := wsRef(t, e, wsRef(t, e, e.Scaffold("acme"), "api", "git@example.test:acme/api.git", ""), "web", "git@example.test:acme/web.git", "")
	write(t, proj, "catalog.yaml", string(data))
	git := &fakeRepos{repos: map[string]string{"https://github.com/acme/multiverse.git": proj}}
	r := newRig(t, git)
	r.cfg.c = app.Config{}

	if _, err := r.PlanInstall(context.Background(), []string{"acme/multiverse"}, app.InstallOptions{Base: "/ws"}); err == nil || !strings.Contains(err.Error(), "acme/multiverse:<name>") {
		t.Errorf("without a terminal the error should say how to name one: %v", err)
	}
	r.prompt.selects = []int{1}
	if got := plan(t, r, []string{"acme/multiverse"}, app.InstallOptions{}); len(got) != 1 || got[0].Name != "web" {
		t.Errorf("a choice: %+v", got)
	}
}

// A repository serves several multiverses by holding one file each, found by
// convention, and owner/repo:name picks one without anything listing them.
func TestAnAddressNamesOneOfTheMultiversesARepositoryDefines(t *testing.T) {
	repo := t.TempDir()
	write(t, repo, "multiverse.yaml", multiverseNamed("alpha"))
	write(t, repo, "multiverse/beta.yaml", multiverseNamed("beta"))
	write(t, repo, "multiverse/notes.txt", "not a definition")
	git := &fakeRepos{repos: map[string]string{"https://github.com/acme/platform.git": repo}}
	r := newRig(t, git)
	r.cfg.c = app.Config{}

	got := plan(t, r, []string{"acme/platform:beta"}, app.InstallOptions{})
	if len(got) != 1 || got[0].Name != "beta" || got[0].ManifestPath != "multiverse/beta.yaml" || got[0].ManifestSource != "https://github.com/acme/platform.git" {
		t.Fatalf("got %+v", got)
	}
	got = plan(t, r, []string{"acme/platform:alpha"}, app.InstallOptions{})
	if len(got) != 1 || got[0].Name != "alpha" || got[0].ManifestPath != "multiverse.yaml" {
		t.Fatalf("got %+v", got)
	}
	_, err := r.PlanInstall(context.Background(), []string{"acme/platform:gamma"}, app.InstallOptions{Base: "/ws"})
	if err == nil || !strings.Contains(err.Error(), `no multiverse named "gamma"`) || !strings.Contains(err.Error(), "alpha, beta") {
		t.Errorf("an unknown name should list the ones there are: %v", err)
	}
	_, err = r.PlanInstall(context.Background(), []string{"acme/platform"}, app.InstallOptions{Base: "/ws"})
	if err == nil || !strings.Contains(err.Error(), "alpha, beta") {
		t.Errorf("no name and no terminal should list the names: %v", err)
	}
}

// Names are what -m takes, so two multiverses cannot share one.
func TestTwoMultiversesCannotShareAName(t *testing.T) {
	repo := t.TempDir()
	write(t, repo, "multiverse.yaml", multiverseNamed("alpha"))
	other := t.TempDir()
	write(t, other, "multiverse.yaml", multiverseNamed("alpha"))
	git := &fakeRepos{repos: map[string]string{
		"https://github.com/acme/one.git": repo, "https://github.com/acme/two.git": other,
	}}
	r := newRig(t, git)
	r.cfg.c = app.Config{Multiverses: map[string]app.MultiverseEntry{
		"alpha": {Root: "/ws/alpha", Source: app.Origin{Repo: "https://github.com/acme/one.git", Path: "multiverse.yaml"}},
	}}

	if got := plan(t, r, []string{"acme/one"}, app.InstallOptions{}); len(got) != 1 || got[0].Skip == "" {
		t.Errorf("the same source again is already installed, not a clash: %+v", got)
	}
	_, err := r.PlanInstall(context.Background(), []string{"acme/two"}, app.InstallOptions{Base: "/ws"})
	if err == nil || !strings.Contains(err.Error(), `"alpha" is already taken`) || !strings.Contains(err.Error(), "acme/one") {
		t.Errorf("a different source under a taken name must be refused, naming the holder: %v", err)
	}
}

func multiverseNamed(name string) string {
	return "apiVersion: multiverse/v1alpha1\nkind: Multiverse\nmetadata:\n  name: " + name + "\nspec:\n  repos:\n    - name: api\n      url: git@example.test:acme/api.git\n"
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

func TestInstallNameAndRootNameOneMultiverse(t *testing.T) {
	r := newRig(t, &fakeRepos{})
	e := catalog.Editor{}
	data := wsRef(t, e, wsRef(t, e, e.Scaffold("me"), "a", "git@example.test:acme/a.git", ""), "b", "git@example.test:acme/b.git", "")
	write(t, r.root, "catalog.yaml", string(data))
	if _, err := r.PlanInstall(context.Background(), []string{"a", "b"}, app.InstallOptions{Base: "/ws", Name: "x"}); err == nil {
		t.Error("--name with two projects must be refused")
	}
	if got := plan(t, r, []string{"a"}, app.InstallOptions{Name: "mine"}); got[0].Name != "mine" || got[0].Root != "/ws/mine" {
		t.Errorf("%+v", got)
	}
}

func TestInstallPickerListsTheTreeAndMarksInstalled(t *testing.T) {
	r := newRig(t, &fakeRepos{})
	e := catalog.Editor{}
	write(t, r.root, "catalog.yaml", string(wsRef(t, e, wsRef(t, e, e.Scaffold("me"), "a", "git@example.test:acme/a.git", ""), "b", "git@example.test:acme/b.git", "")))
	r.cfg.c.Multiverses = map[string]app.MultiverseEntry{"a": {From: "a", Source: app.Origin{Repo: "git@example.test:acme/a.git", Path: "multiverse.yaml"}}}
	r.prompt.multi = []int{2}
	got := plan(t, r, nil, app.InstallOptions{})
	if len(got) != 1 || got[0].Name != "b" {
		t.Fatalf("%+v", got)
	}
}

func TestAddTakesOneMultiverseOfARepositoryByItsAddress(t *testing.T) {
	repo := t.TempDir()
	write(t, repo, "multiverse.yaml", multiverseNamed("alpha"))
	write(t, repo, "multiverse/beta.yaml", multiverseNamed("beta"))
	git := &fakeRepos{inRepo: true, repos: map[string]string{"https://github.com/acme/platform.git": repo}}
	r := newRig(t, git)
	r.Yes = true
	node, err := r.Add(context.Background(), app.AddInput{Repo: "acme/platform:beta", At: "/", AtGiven: true})
	if err != nil || node != "beta" {
		t.Fatalf("node=%q err=%v", node, err)
	}
	leaf, ok := r.tree(t).Find("beta")
	if !ok || leaf.Source.Path != "multiverse/beta.yaml" || leaf.Source.Repo != "https://github.com/acme/platform.git" {
		t.Fatalf("leaf = %+v", leaf)
	}
}

func TestInstallFromACatalogNodeCanLeaveABranchOut(t *testing.T) {
	r := newRig(t, &fakeRepos{})
	e := catalog.Editor{}
	data := e.Scaffold("me")
	data, _ = e.AddCatalogRef(data, domain.CatalogRef{Name: "acme", Source: domain.Source{Path: "catalogs/acme.yaml"}})
	write(t, r.root, "catalog.yaml", string(data))
	acme := e.Scaffold("acme")
	acme, _ = e.AddCatalogRef(acme, domain.CatalogRef{Name: "legacy", Source: domain.Source{Path: "catalogs/legacy.yaml"}})
	acme = wsRef(t, e, acme, "api", "git@example.test:acme/api.git", "")
	write(t, r.root, "catalogs/acme.yaml", string(acme))
	write(t, r.root, "catalogs/legacy.yaml", string(wsRef(t, e, e.Scaffold("legacy"), "old", "git@example.test:acme/old.git", "")))

	if got := plan(t, r, []string{"acme"}, app.InstallOptions{}); len(got) != 2 {
		t.Fatalf("without --exclude: %+v", got)
	}
	got := plan(t, r, []string{"acme"}, app.InstallOptions{Exclude: []string{"acme/legacy"}})
	if len(got) != 1 || got[0].Name != "api" {
		t.Errorf("targets = %+v, want only api", got)
	}
	if _, err := r.PlanInstall(context.Background(), []string{"acme"}, app.InstallOptions{Base: "/ws", Exclude: []string{"acme/nope"}}); err == nil {
		t.Error("an excluded path that is not in the catalog must be refused, it is a typo")
	}
}
