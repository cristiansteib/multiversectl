package app_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/adapters/catalog"
	"github.com/cristiansteib/multiversectl/internal/adapters/fsx"
	"github.com/cristiansteib/multiversectl/internal/app"
	"github.com/cristiansteib/multiversectl/internal/domain"
)

// fakeRepos stands in for git: a clone copies a prepared directory, so the
// resolver runs against real files without a network.
type fakeRepos struct {
	app.Git
	repos   map[string]string // url -> directory to copy
	cloned  []string
	fetched []string
	checked map[string]string

	remotes   map[string]string // checkout dir -> origin URL
	inRepo    bool              // Toplevel succeeds: local catalogs are in a work tree
	emptyRepo bool              // DefaultBranch fails, as in a repository with no commits
	commits   []string
	pushed    []string
	pushErr   error
}

func (f *fakeRepos) Clone(_ context.Context, url, dest string) error {
	src, ok := f.repos[url]
	if !ok {
		return errors.New("repository not found")
	}
	f.cloned = append(f.cloned, url)
	return os.CopyFS(dest, os.DirFS(src))
}
func (f *fakeRepos) Fetch(_ context.Context, dir string) error {
	f.fetched = append(f.fetched, dir)
	return nil
}
func (f *fakeRepos) DefaultBranch(context.Context, string) (string, error) {
	if f.emptyRepo {
		return "", errors.New("cannot determine the default branch of origin")
	}
	return "main", nil
}
func (f *fakeRepos) Toplevel(_ context.Context, dir string) (string, error) {
	if !f.inRepo {
		return "", errors.New("not a git repository")
	}
	return dir, nil
}
func (f *fakeRepos) IsClean(context.Context, string) (bool, error) { return true, nil }
func (f *fakeRepos) HasRemote(context.Context, string) bool        { return false }
func (f *fakeRepos) RemoteURL(_ context.Context, dir string) (string, error) {
	if u, ok := f.remotes[dir]; ok {
		return u, nil
	}
	return "", errors.New("no such remote")
}
func (f *fakeRepos) RefExists(context.Context, string, string) (app.RefKind, error) {
	return app.RefRemote, nil
}
func (f *fakeRepos) Init(context.Context, string) error { return nil }
func (f *fakeRepos) Commit(_ context.Context, _, msg string, paths ...string) error {
	f.commits = append(f.commits, msg+" "+strings.Join(paths, ","))
	return nil
}
func (f *fakeRepos) Push(_ context.Context, _, branch string) error {
	f.pushed = append(f.pushed, branch)
	return f.pushErr
}
func (f *fakeRepos) Checkout(_ context.Context, dir, ref string) error {
	if f.checked == nil {
		f.checked = map[string]string{}
	}
	f.checked[dir] = ref
	return nil
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func cat(name, body string) string {
	return "apiVersion: multiverse/v1alpha1\nkind: Catalog\nmetadata: {name: " + name + "}\nspec:\n" + body
}

func newCatalogs(t *testing.T, git *fakeRepos) (*app.Catalogs, string) {
	t.Helper()
	cache := t.TempDir()
	return app.NewCatalogs(app.CatalogDeps{Git: git, Files: fsx.New(), Decoder: catalog.Decoder{}, CacheDir: cache}), cache
}

func TestTreeComposesLocalAndRemoteCatalogs(t *testing.T) {
	local, remote := t.TempDir(), t.TempDir()
	write(t, local, "catalog.yaml", cat("me", `  catalogs:
    - name: personal
      source: {path: catalogs/personal.yaml}
    - name: acme
      source: {repo: git@example.test:acme/index.git, ref: v1}
`))
	write(t, local, "catalogs/personal.yaml", cat("personal", `  workspaces:
    - name: fichero
      source: {repo: git@example.test:me/fichero.git}
`))
	write(t, remote, "catalog.yaml", cat("acme", `  catalogs:
    - name: backend
      source: {path: teams/backend.yaml}
`))
	write(t, remote, "teams/backend.yaml", cat("backend", `  workspaces:
    - name: payments
      source: {path: payments/multiverse.yaml}
`))

	git := &fakeRepos{repos: map[string]string{"git@example.test:acme/index.git": remote}}
	c, _ := newCatalogs(t, git)
	root, err := c.Tree(context.Background(), filepath.Join(local, "catalog.yaml"), false)
	if err != nil {
		t.Fatal(err)
	}

	got, ok := root.Find("acme/backend/payments")
	if !ok || got.Kind != domain.WorkspaceNode {
		t.Fatalf("acme/backend/payments not found: %+v", root)
	}
	if got.Source.Repo != "git@example.test:acme/index.git" || got.Source.Ref != "v1" || got.Source.Path != "payments/multiverse.yaml" {
		t.Errorf("a same-repo workspace must inherit the repository and ref: %+v", got.Source)
	}
	if _, ok := root.Find("personal/fichero"); !ok {
		t.Error("personal/fichero not found")
	}
	if n := len(root.Leaves()); n != 2 {
		t.Errorf("leaves = %d, want 2", n)
	}
	if len(git.cloned) != 1 {
		t.Errorf("cloned %v; a same-repo reference must reuse the checkout", git.cloned)
	}
	for _, ref := range git.checked {
		if ref != "v1" {
			t.Errorf("checked out %q, want v1", ref)
		}
	}
}

func TestTreeKeepsGoingPastAnUnreachableLayer(t *testing.T) {
	local := t.TempDir()
	write(t, local, "catalog.yaml", cat("me", `  catalogs:
    - name: acme
      source: {repo: git@example.test:acme/private.git}
    - name: missing
      source: {path: nope.yaml}
  workspaces:
    - name: fichero
      source: {repo: git@example.test:me/fichero.git}
`))
	c, _ := newCatalogs(t, &fakeRepos{})
	root, err := c.Tree(context.Background(), filepath.Join(local, "catalog.yaml"), false)
	if err != nil {
		t.Fatalf("one unreachable layer must not fail the tree: %v", err)
	}
	for _, name := range []string{"acme", "missing"} {
		n, _ := root.Find(name)
		if n.Unavailable == "" {
			t.Errorf("%s should be unavailable", name)
		}
	}
	if _, ok := root.Find("fichero"); !ok {
		t.Error("the reachable workspace disappeared")
	}
}

func TestTreeRejectsCycles(t *testing.T) {
	local := t.TempDir()
	write(t, local, "catalog.yaml", cat("me", "  catalogs:\n    - name: again\n      source: {path: catalog.yaml}\n"))
	c, _ := newCatalogs(t, &fakeRepos{})
	root, err := c.Tree(context.Background(), filepath.Join(local, "catalog.yaml"), false)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := root.Find("again")
	if !strings.Contains(n.Unavailable, "refers back to itself") {
		t.Errorf("cycle not reported: %q", n.Unavailable)
	}
}

func TestRefreshFetchesEachRepositoryOnce(t *testing.T) {
	remote, local := t.TempDir(), t.TempDir()
	write(t, remote, "catalog.yaml", cat("acme", ""))
	write(t, local, "catalog.yaml", cat("me", `  catalogs:
    - name: a
      source: {repo: git@example.test:acme/index.git}
    - name: b
      source: {repo: git@example.test:acme/index.git}
`))
	git := &fakeRepos{repos: map[string]string{"git@example.test:acme/index.git": remote}}
	c, _ := newCatalogs(t, git)
	for i := 0; i < 2; i++ {
		if _, err := c.Tree(context.Background(), filepath.Join(local, "catalog.yaml"), true); err != nil {
			t.Fatal(err)
		}
	}
	if len(git.cloned) != 1 {
		t.Errorf("cloned %d times, want 1", len(git.cloned))
	}
	// The first run clones, so it has nothing to fetch; the second fetches the
	// shared repository once even though two references point at it.
	if len(git.fetched) != 1 {
		t.Errorf("fetched %d times, want 1", len(git.fetched))
	}
}
