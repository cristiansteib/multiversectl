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
	if !strings.Contains(written, "kind: CatalogRoot") {
		t.Errorf("a root made by init must be a CatalogRoot:\n%s", written)
	}
	if len(r.host.topics) != 1 || !strings.HasSuffix(r.host.topics[0], app.RootTopic) {
		t.Errorf("the repository should be tagged so a new machine finds it: %v", r.host.topics)
	}
}

func TestInitUpgradesARootThatIsAPlainCatalog(t *testing.T) {
	git := &fakeRepos{inRepo: true}
	r := rootRig(t, catalog.Editor{}.Scaffold, git)
	r.Yes = true
	if err := r.Init(context.Background(), app.InitInput{}); err != nil {
		t.Fatal(err)
	}
	if got := read1(t, r.cfg.c.Catalog); !strings.Contains(got, "kind: CatalogRoot") {
		t.Errorf("a legacy root should be marked:\n%s", got)
	}
	if len(git.commits) == 0 {
		t.Error("the upgrade should be committed")
	}
}

func TestInitLeavesARootThatIsAlreadyMarked(t *testing.T) {
	git := &fakeRepos{inRepo: true}
	r := rootRig(t, catalog.Editor{}.ScaffoldRoot, git)
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
	write(t, dir, "catalog.yaml", string(catalog.Editor{}.ScaffoldRoot("me")))
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

func TestInitRefusesToSwitchRootsWhileWorkspacesDependOnIt(t *testing.T) {
	git := &fakeRepos{inRepo: true}
	r := rootRig(t, catalog.Editor{}.ScaffoldRoot, git)
	r.cfg.c.Workspaces = map[string]app.WorkspaceEntry{"acme": {Root: "/w/acme", From: "personal/acme"}}
	other := t.TempDir()
	write(t, other, "catalog.yaml", string(catalog.Editor{}.ScaffoldRoot("other")))
	r.Yes = true

	err := r.Init(context.Background(), app.InitInput{Source: other})
	if err == nil || !strings.Contains(err.Error(), "multiversectl reset") || !strings.Contains(err.Error(), "1 installed projects") {
		t.Fatalf("err = %v, want a refusal that names reset", err)
	}
	if !strings.Contains(r.cfg.c.Catalog, r.root) {
		t.Errorf("the root must not change: %q", r.cfg.c.Catalog)
	}
}

func TestInitSwitchesRootsOnceNothingDependsOnTheOldOne(t *testing.T) {
	git := &fakeRepos{inRepo: true}
	r := rootRig(t, catalog.Editor{}.ScaffoldRoot, git)
	other := t.TempDir()
	write(t, other, "catalog.yaml", string(catalog.Editor{}.ScaffoldRoot("other")))
	r.prompt.confirms = []bool{true}
	if err := r.Init(context.Background(), app.InitInput{Source: other}); err != nil {
		t.Fatal(err)
	}
	if r.cfg.c.Catalog != filepath.Join(other, "catalog.yaml") {
		t.Errorf("root = %q", r.cfg.c.Catalog)
	}
}

func TestResetForgetsTheRootAndTheWorkspacesButNoFiles(t *testing.T) {
	r := rootRig(t, catalog.Editor{}.ScaffoldRoot, &fakeRepos{inRepo: true})
	checkout := t.TempDir()
	r.cfg.c.Workspaces = map[string]app.WorkspaceEntry{"acme": {Root: checkout, From: "personal/acme"}}
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
	if r.cfg.c.Catalog != "" || len(r.cfg.c.Workspaces) != 0 {
		t.Errorf("config = %+v, want it empty", r.cfg.c)
	}
	if _, err := os.Stat(checkout); err != nil {
		t.Error("reset must not delete anything on disk")
	}
}
