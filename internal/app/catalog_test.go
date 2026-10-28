package app_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cristiansteib/multiversectl/internal/adapters/catalog"
	"github.com/cristiansteib/multiversectl/internal/adapters/fsx"
	"github.com/cristiansteib/multiversectl/internal/app"
	"github.com/cristiansteib/multiversectl/internal/domain"
)

// fakeRepos stands in for git: a clone copies a prepared directory, so the
// resolver runs against real files without a network.
type fakeRepos struct {
	app.Git
	mu      sync.Mutex        // catalogs are read concurrently
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
	behind    int       // commits origin is ahead by
	ahead     int       // commits here that origin lacks
	dirty     bool      // the working tree has uncommitted changes
	lastFetch time.Time // when dir last fetched; zero is never
	ffed      []string  // branches fast-forwarded

	hang      map[string]bool // url -> a clone of it never answers (a VPN that is down)
	hangFetch bool            // every fetch never answers
	tried     []string        // urls a clone was started for
}

// block is a network call that answers only when it is cancelled.
func block(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }

func (f *fakeRepos) Clone(ctx context.Context, url, dest string) error {
	f.mu.Lock()
	f.tried = append(f.tried, url)
	src, ok := f.repos[url]
	hang := f.hang[url]
	if ok && !hang {
		f.cloned = append(f.cloned, url)
	}
	f.mu.Unlock()
	if hang {
		return block(ctx)
	}
	if !ok {
		return errors.New("repository not found")
	}
	return os.CopyFS(dest, os.DirFS(src))
}
func (f *fakeRepos) Fetch(ctx context.Context, dir string) error {
	f.mu.Lock()
	f.fetched = append(f.fetched, dir)
	f.mu.Unlock()
	if f.hangFetch {
		return block(ctx)
	}
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
func (f *fakeRepos) IsClean(context.Context, string) (bool, error)      { return !f.dirty, nil }
func (f *fakeRepos) Ahead(context.Context, string, string) (int, error) { return f.ahead, nil }
func (f *fakeRepos) LastFetch(context.Context, string) time.Time        { return f.lastFetch }
func (f *fakeRepos) HasRemote(context.Context, string) bool             { return false }
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
func (f *fakeRepos) AddRemote(_ context.Context, dir, _, url string) error {
	if f.remotes == nil {
		f.remotes = map[string]string{}
	}
	f.remotes[dir] = url
	return nil
}
func (f *fakeRepos) CurrentBranch(context.Context, string) (string, error) { return "main", nil }
func (f *fakeRepos) Commit(_ context.Context, _, msg string, paths ...string) error {
	f.commits = append(f.commits, msg+" "+strings.Join(paths, ","))
	return nil
}
func (f *fakeRepos) Push(_ context.Context, _, branch string) error {
	f.pushed = append(f.pushed, branch)
	return f.pushErr
}
func (f *fakeRepos) Checkout(_ context.Context, dir, ref string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
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
	write(t, local, "catalogs/personal.yaml", cat("personal", `  multiverses:
    - name: billing
      source: {repo: git@example.test:me/billing.git}
`))
	write(t, remote, "catalog.yaml", cat("acme", `  catalogs:
    - name: backend
      source: {path: teams/backend.yaml}
`))
	write(t, remote, "teams/backend.yaml", cat("backend", `  multiverses:
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
	if !ok || got.Kind != domain.MultiverseNode {
		t.Fatalf("acme/backend/payments not found: %+v", root)
	}
	if got.Source.Repo != "git@example.test:acme/index.git" || got.Source.Ref != "v1" || got.Source.Path != "payments/multiverse.yaml" {
		t.Errorf("a same-repo project must inherit the repository and ref: %+v", got.Source)
	}
	if _, ok := root.Find("personal/billing"); !ok {
		t.Error("personal/billing not found")
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
  multiverses:
    - name: billing
      source: {repo: git@example.test:me/billing.git}
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
	if _, ok := root.Find("billing"); !ok {
		t.Error("the reachable project disappeared")
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

func (f *fakeRepos) Behind(context.Context, string, string) (int, error) { return f.behind, nil }
func (f *fakeRepos) FastForward(_ context.Context, _, branch string) error {
	f.ffed = append(f.ffed, branch)
	return nil
}

// A catalog behind a VPN that is down is shown as unavailable, and the
// catalogs that answer are still usable.
func TestTreeTimesOutAnUnreachableHostAndKeepsTheRest(t *testing.T) {
	local, remote := t.TempDir(), t.TempDir()
	write(t, local, "catalog.yaml", cat("me", `  catalogs:
    - name: vpn1
      source: {repo: git@vpn.example.test:acme/one.git}
    - name: vpn2
      source: {repo: git@vpn.example.test:acme/two.git}
    - name: open
      source: {repo: git@example.test:acme/open.git}
`))
	write(t, remote, "catalog.yaml", cat("open", `  multiverses:
    - name: billing
      source: {repo: git@example.test:me/billing.git}
`))
	git := &fakeRepos{
		repos: map[string]string{"git@example.test:acme/open.git": remote},
		hang:  map[string]bool{"git@vpn.example.test:acme/one.git": true, "git@vpn.example.test:acme/two.git": true},
	}
	cache := t.TempDir()
	c := app.NewCatalogs(app.CatalogDeps{Git: git, Files: fsx.New(), Decoder: catalog.Decoder{}, CacheDir: cache, Timeout: 50 * time.Millisecond, Out: &quiet{}})
	start := time.Now()
	root, err := c.Tree(context.Background(), filepath.Join(local, "catalog.yaml"), false)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 2*time.Second {
		t.Errorf("took %s: the timeout did not bound it", time.Since(start))
	}
	for _, name := range []string{"vpn1", "vpn2"} {
		n, _ := root.Find(name)
		if !strings.Contains(n.Unavailable, "timed out") {
			t.Errorf("%s: unavailable = %q, want a timeout", name, n.Unavailable)
		}
	}
	if _, ok := root.Find("open/billing"); !ok {
		t.Error("the reachable catalog is not usable")
	}
}

// A fetch that times out keeps the copy fetched before.
func TestRefreshFallsBackToTheCopyFetchedEarlier(t *testing.T) {
	local, remote := t.TempDir(), t.TempDir()
	write(t, local, "catalog.yaml", cat("me", `  catalogs:
    - name: acme
      source: {repo: git@example.test:acme/index.git}
`))
	write(t, remote, "catalog.yaml", cat("acme", `  multiverses:
    - name: api
      source: {path: api.yaml}
`))
	git := &fakeRepos{repos: map[string]string{"git@example.test:acme/index.git": remote}}
	cache := t.TempDir()
	c := app.NewCatalogs(app.CatalogDeps{Git: git, Files: fsx.New(), Decoder: catalog.Decoder{}, CacheDir: cache, Timeout: 50 * time.Millisecond, Out: &quiet{}})
	source := filepath.Join(local, "catalog.yaml")
	if _, err := c.Tree(context.Background(), source, false); err != nil {
		t.Fatal(err)
	}
	git.hangFetch = true
	root, err := c.Tree(context.Background(), source, true)
	if err != nil {
		t.Fatal(err)
	}
	if n, ok := root.Find("acme/api"); !ok || n.Unavailable != "" {
		t.Errorf("the cached copy was not used: %+v", root)
	}
}

// Catalogs that cannot be reached are waited for together: six repositories
// that never answer cost one timeout, not six.
func TestTreeReadsSiblingCatalogsAtTheSameTime(t *testing.T) {
	local := t.TempDir()
	var body strings.Builder
	body.WriteString("  catalogs:\n")
	hang := map[string]bool{}
	for i := range 6 {
		repo := fmt.Sprintf("git@host%d.example.test:acme/c.git", i)
		hang[repo] = true
		fmt.Fprintf(&body, "    - name: c%d\n      source: {repo: %s}\n", i, repo)
	}
	write(t, local, "catalog.yaml", cat("me", body.String()))
	timeout := 150 * time.Millisecond
	c := app.NewCatalogs(app.CatalogDeps{Git: &fakeRepos{hang: hang}, Files: fsx.New(), Decoder: catalog.Decoder{}, CacheDir: t.TempDir(), Timeout: timeout, Out: &quiet{}})
	start := time.Now()
	root, err := c.Tree(context.Background(), filepath.Join(local, "catalog.yaml"), false)
	if err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > 4*timeout {
		t.Errorf("took %s for six repositories that time out: they were read one after another", took)
	}
	if len(root.Children) != 6 || root.Children[0].Name != "c0" || root.Children[5].Name != "c5" {
		t.Errorf("children %+v: the order of the catalog must be kept", root.Children)
	}
}

func TestTreeTellsWhichRepositoryIsBeingReadAndHowItWent(t *testing.T) {
	local, remote := t.TempDir(), t.TempDir()
	write(t, local, "catalog.yaml", cat("me", `  catalogs:
    - name: acme
      source: {repo: git@example.test:acme/index.git}
    - name: gone
      source: {repo: git@example.test:acme/gone.git}
`))
	write(t, remote, "catalog.yaml", cat("acme", "  multiverses: []\n"))
	git := &fakeRepos{repos: map[string]string{"git@example.test:acme/index.git": remote}}
	c := app.NewCatalogs(app.CatalogDeps{Git: git, Files: fsx.New(), Decoder: catalog.Decoder{}, CacheDir: t.TempDir(), Out: &quiet{}})
	var mu sync.Mutex
	phases := map[string][]string{}
	_, err := c.TreeObserved(context.Background(), filepath.Join(local, "catalog.yaml"), false, func(e app.CatalogEvent) {
		mu.Lock()
		phases[e.Repo] = append(phases[e.Repo], e.Phase)
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(phases["git@example.test:acme/index.git"], ","); got != "fetching,done" {
		t.Errorf("index: %s", got)
	}
	if got := strings.Join(phases["git@example.test:acme/gone.git"], ","); got != "fetching,unavailable" {
		t.Errorf("gone: %s", got)
	}
}

func TestAFallbackToTheOldCopyMarksTheNodeStale(t *testing.T) {
	local, remote := t.TempDir(), t.TempDir()
	write(t, local, "catalog.yaml", cat("me", `  catalogs:
    - name: acme
      source: {repo: git@example.test:acme/index.git}
`))
	write(t, remote, "catalog.yaml", cat("acme", "  multiverses: []\n"))
	git := &fakeRepos{repos: map[string]string{"git@example.test:acme/index.git": remote}}
	c := app.NewCatalogs(app.CatalogDeps{Git: git, Files: fsx.New(), Decoder: catalog.Decoder{}, CacheDir: t.TempDir(), Timeout: 50 * time.Millisecond, Out: &quiet{}})
	source := filepath.Join(local, "catalog.yaml")
	if _, err := c.Tree(context.Background(), source, false); err != nil {
		t.Fatal(err)
	}
	git.hangFetch = true
	root, err := c.Tree(context.Background(), source, true)
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := root.Find("acme"); !strings.Contains(n.Stale, "timed out") || n.Unavailable != "" {
		t.Errorf("acme: stale %q, unavailable %q; want a stale timeout", n.Stale, n.Unavailable)
	}
}
