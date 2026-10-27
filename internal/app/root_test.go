package app_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/adapters/catalog"
	"github.com/cristiansteib/multiversectl/internal/app"
)

// rootRig is a machine whose root catalog is a file of the given kind.
func rootRig(t *testing.T, scaffold func(string) []byte, git *fakeRepos) *rig {
	t.Helper()
	r := newRig(t, git)
	write(t, r.root, "catalog.yaml", string(scaffold("me")))
	return r
}

func TestInitCreatesTheRootAsACatalogRoot(t *testing.T) {
	git := &fakeRepos{inRepo: true, emptyRepo: true, repos: map[string]string{}}
	r := newRig(t, git)
	r.cfg.c = app.Config{}
	r.Yes = true
	dir := t.TempDir()
	git.repos[r.host.url] = dir
	if err := r.Init(context.Background(), app.InitInput{Where: app.RepoChoice{Create: "me/multiverse-index"}}); err != nil {
		t.Fatal(err)
	}
	var written string
	_ = filepath.WalkDir(r.cache, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() == "catalog.yaml" {
			written = read1(t, p)
		}
		return nil
	})
	if !strings.Contains(written, "kind: Catalog") {
		t.Errorf("a root made by init must be a Catalog:\n%s", written)
	}
	if len(r.host.topics) != 1 || !strings.HasSuffix(r.host.topics[0], app.CatalogTopic) {
		t.Errorf("the repository should be tagged so a new machine finds it: %v", r.host.topics)
	}
}

func TestInitLeavesACatalogThatIsAlreadyOne(t *testing.T) {
	git := &fakeRepos{inRepo: true}
	r := rootRig(t, catalog.Editor{}.Scaffold, git)
	r.Yes = true
	if err := r.Init(context.Background(), app.InitInput{}); err != nil {
		t.Fatal(err)
	}
	if len(git.commits) != 0 {
		t.Errorf("nothing to change, nothing to commit: %v", git.commits)
	}
}

func TestInitFindsTheRootByItsTopicOnAFreshMachine(t *testing.T) {
	git := &fakeRepos{inRepo: true, repos: map[string]string{}}
	r := newRig(t, git)
	r.cfg.c = app.Config{}
	r.Yes = true
	dir := t.TempDir()
	write(t, dir, "catalog.yaml", string(catalog.Editor{}.Scaffold("me")))
	git.repos["https://example.test/me/multiverse-index.git"] = dir
	r.host.roots = []app.RemoteRepo{{Name: "me/multiverse-index", URL: "https://example.test/me/multiverse-index.git", Private: true}}

	if err := r.Init(context.Background(), app.InitInput{}); err != nil {
		t.Fatal(err)
	}
	if r.cfg.c.Catalog != "https://example.test/me/multiverse-index.git" {
		t.Errorf("the root found by topic should be used, got %q", r.cfg.c.Catalog)
	}
}

func TestInitWithNothingFoundSaysWhatToGive(t *testing.T) {
	r := newRig(t, &fakeRepos{inRepo: true})
	r.cfg.c = app.Config{}
	r.Yes = true
	err := r.Init(context.Background(), app.InitInput{})
	if err == nil || !strings.Contains(err.Error(), "give a source") {
		t.Fatalf("err = %v", err)
	}
}

func TestInitRefusesToSwitchRootsWhileMultiversesDependOnIt(t *testing.T) {
	git := &fakeRepos{inRepo: true}
	r := rootRig(t, catalog.Editor{}.Scaffold, git)
	r.cfg.c.Multiverses = map[string]app.MultiverseEntry{"acme": {Root: "/w/acme", From: "personal/acme"}}
	other := t.TempDir()
	write(t, other, "catalog.yaml", string(catalog.Editor{}.Scaffold("other")))
	r.Yes = true

	err := r.Init(context.Background(), app.InitInput{Source: other})
	if err == nil || !strings.Contains(err.Error(), "multiversectl system reset") || !strings.Contains(err.Error(), "1 installed multiverses") {
		t.Fatalf("err = %v, want a refusal that names reset", err)
	}
	if !strings.Contains(r.cfg.c.Catalog, r.root) {
		t.Errorf("the root must not change: %q", r.cfg.c.Catalog)
	}
}

func TestInitSwitchesRootsOnceNothingDependsOnTheOldOne(t *testing.T) {
	git := &fakeRepos{inRepo: true}
	r := rootRig(t, catalog.Editor{}.Scaffold, git)
	other := t.TempDir()
	write(t, other, "catalog.yaml", string(catalog.Editor{}.Scaffold("other")))
	r.prompt.confirms = []bool{true}
	if err := r.Init(context.Background(), app.InitInput{Source: other}); err != nil {
		t.Fatal(err)
	}
	if r.cfg.c.Catalog != filepath.Join(other, "catalog.yaml") {
		t.Errorf("root = %q", r.cfg.c.Catalog)
	}
}

func TestResetForgetsTheRootAndTheMultiversesButNoFiles(t *testing.T) {
	r := rootRig(t, catalog.Editor{}.Scaffold, &fakeRepos{inRepo: true})
	checkout := t.TempDir()
	r.cfg.c.Multiverses = map[string]app.MultiverseEntry{"acme": {Root: checkout, From: "personal/acme"}}
	r.prompt.confirms = []bool{false}
	if _, err := r.Reset(); err == nil {
		t.Fatal("declining must not reset")
	}
	if r.cfg.c.Catalog == "" {
		t.Fatal("declining changed the configuration")
	}

	r.prompt.confirms = []bool{true}
	if forgot, err := r.Reset(); err != nil || !forgot {
		t.Fatalf("forgot %v, %v", forgot, err)
	}
	if r.cfg.c.Catalog != "" || len(r.cfg.c.Multiverses) != 0 {
		t.Errorf("config = %+v, want it empty", r.cfg.c)
	}
	if _, err := os.Stat(checkout); err != nil {
		t.Error("reset must not delete anything on disk")
	}
}
