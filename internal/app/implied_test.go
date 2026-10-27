package app_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/adapters/manifest"
	"github.com/cristiansteib/multiversectl/internal/app"
)

// A repository with compose files and no multiverse.yaml is a multiverse of one:
// itself, named after the repository, with nothing written into it.
func TestARepositoryWithOnlyComposeRunsAsItself(t *testing.T) {
	repo := t.TempDir()
	write(t, repo, "compose.yaml", "services: {}\n")
	url := "https://github.com/acme/api.git"
	r := projectRig(t, &fakeRepos{repos: map[string]string{url: repo}})
	r.cfg.c = app.Config{}

	got := plan(t, r, []string{"acme/api"}, app.InstallOptions{})
	if len(got) != 1 || got[0].Name != "api" || got[0].Renamed != "" {
		t.Fatalf("got %+v", got)
	}
	m, err := manifest.Decode(got[0].Generated, "/ws/api/.multiverse")
	if err != nil {
		t.Fatalf("the generated manifest must be one the tool reads: %v\n%s", err, got[0].Generated)
	}
	if m.Name != "api" || len(m.Repos) != 1 || m.Repos["api"].URL != url {
		t.Errorf("name %q, repos %+v: want api, running the repository itself", m.Name, m.Repos)
	}
	if !strings.Contains(got[0].How, "runs as itself") {
		t.Errorf("the way it was read should say so: %q", got[0].How)
	}
}

func TestARepositoryWithNothingToRunSaysWhatItLookedFor(t *testing.T) {
	repo := t.TempDir()
	write(t, repo, "README.md", "hi")
	r := projectRig(t, &fakeRepos{repos: map[string]string{"https://github.com/acme/lib.git": repo}})
	r.cfg.c = app.Config{}
	_, err := r.PlanInstall(context.Background(), []string{"acme/lib"}, app.InstallOptions{Base: "/ws"})
	if err == nil || !strings.Contains(err.Error(), "no compose file") {
		t.Fatalf("err = %v", err)
	}
}

// The newcomer takes the next level of its address, and the one already
// installed keeps its name.
func TestAClashingNameTakesTheNextLevelOfTheAddress(t *testing.T) {
	repo := t.TempDir()
	write(t, repo, "compose.yaml", "services: {}\n")
	url := "https://git.example.test/acme/api.git"
	r := projectRig(t, &fakeRepos{repos: map[string]string{url: repo}})
	r.cfg.c = app.Config{Multiverses: map[string]app.MultiverseEntry{
		"api": {Root: "/ws/api", Source: app.Origin{Repo: "https://github.com/other/api.git"}},
	}}

	got := plan(t, r, []string{url}, app.InstallOptions{})
	if got[0].Name != "acme-api" || !strings.Contains(got[0].Renamed, "`api` is taken by github:other/api") {
		t.Fatalf("got name %q, renamed %q", got[0].Name, got[0].Renamed)
	}
	if got[0].Root != "/ws/acme-api" {
		t.Errorf("root = %q: the folder follows the name", got[0].Root)
	}
	m, err := manifest.Decode(got[0].Generated, "/ws")
	if err != nil || m.Name != "acme-api" || m.Repos["api"].URL != url {
		t.Errorf("the manifest takes the long name and keeps the short key: %+v err=%v", m, err)
	}

	r.cfg.c.Multiverses["acme-api"] = app.MultiverseEntry{Root: "/ws/acme-api", Source: app.Origin{Repo: "https://elsewhere.test/acme/api.git"}}
	got = plan(t, r, []string{url}, app.InstallOptions{})
	if got[0].Name != "git-example-test-acme-api" {
		t.Errorf("name = %q, want the host added once the owner is not enough", got[0].Name)
	}
}

func TestAClashWithNoLevelLeftNamesTheWayOut(t *testing.T) {
	repo := t.TempDir()
	write(t, repo, "compose.yaml", "services: {}\n")
	r := projectRig(t, &fakeRepos{repos: map[string]string{"https://github.com/acme/api.git": repo}})
	r.cfg.c = app.Config{Multiverses: map[string]app.MultiverseEntry{
		"api": {Root: "/a", Source: app.Origin{Repo: "https://github.com/x/api.git"}}, "acme-api": {Root: "/b", Source: app.Origin{Repo: "https://gitlab.com/acme/api.git"}},
	}}
	_, err := r.PlanInstall(context.Background(), []string{"acme/api"}, app.InstallOptions{Base: "/ws"})
	if err == nil || !strings.Contains(err.Error(), "--as") {
		t.Fatalf("err = %v", err)
	}
}

// A name someone gave is a claim: --as is refused when it is taken, not
// stretched into another one.
func TestAChosenNameIsNeverStretched(t *testing.T) {
	repo := t.TempDir()
	write(t, repo, "compose.yaml", "services: {}\n")
	r := projectRig(t, &fakeRepos{repos: map[string]string{"https://github.com/acme/api.git": repo}})
	r.cfg.c = app.Config{Multiverses: map[string]app.MultiverseEntry{
		"web": {Root: "/a", Source: app.Origin{Repo: "https://github.com/x/web.git"}},
	}}
	_, err := r.PlanInstall(context.Background(), []string{"acme/api"}, app.InstallOptions{Base: "/ws", Name: "web"})
	if err == nil || !strings.Contains(err.Error(), `"web" is already taken`) {
		t.Fatalf("err = %v", err)
	}
}

func TestTheSameRepositoryIsNotInstalledTwiceUnderALongerName(t *testing.T) {
	repo := t.TempDir()
	write(t, repo, "compose.yaml", "services: {}\n")
	url := "https://github.com/acme/api.git"
	r := projectRig(t, &fakeRepos{repos: map[string]string{url: repo}})
	r.cfg.c = app.Config{Multiverses: map[string]app.MultiverseEntry{
		"api": {Root: "/ws/api", Source: app.Origin{Repo: url}},
	}}
	got := plan(t, r, []string{"acme/api"}, app.InstallOptions{})
	if len(got) != 1 || got[0].Name != "api" || !strings.Contains(got[0].Skip, "already installed as api") {
		t.Fatalf("got %+v", got)
	}
}

// A checkout on this machine runs as the repository it came from.
func TestACheckoutWithOnlyComposeRunsAsItsOrigin(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "docker-compose.yml", "services: {}\n")
	git := &fakeRepos{remotes: map[string]string{dir: "git@example.test:acme/api.git"}}
	r := projectRig(t, git)
	r.cfg.c = app.Config{}
	got := plan(t, r, []string{dir}, app.InstallOptions{})
	if len(got) != 1 || got[0].Name != "api" || got[0].ManifestSource != "git@example.test:acme/api.git" {
		t.Fatalf("got %+v", got)
	}

	bare := t.TempDir()
	write(t, bare, "docker-compose.yml", "services: {}\n")
	_, err := r.PlanInstall(context.Background(), []string{bare}, app.InstallOptions{Base: "/ws"})
	if err == nil || !strings.Contains(err.Error(), "no remote") {
		t.Errorf("a checkout nobody can clone from cannot be installed: %v", err)
	}
}
