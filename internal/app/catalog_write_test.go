package app_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/adapters/catalog"
	"github.com/cristiansteib/multiversectl/internal/adapters/fsx"
	"github.com/cristiansteib/multiversectl/internal/app"
	"github.com/cristiansteib/multiversectl/internal/domain"
)

type scripted struct {
	takeDefault bool // answer Select with its default
	lastOptions []app.Option
	multi       []int // nil: no terminal
	selects     []int
	inputs      []string
	confirms    []bool
	asked       []string
}

func (p *scripted) Select(label string, opts []app.Option, def int) (int, error) {
	p.asked = append(p.asked, label)
	p.lastOptions = opts
	if p.takeDefault {
		return def, nil
	}
	if len(p.selects) == 0 {
		return 0, app.ErrNotInteractive
	}
	n := p.selects[0]
	p.selects = p.selects[1:]
	return n, nil
}
func (p *scripted) MultiSelect(label string, _ []app.Option) ([]int, error) {
	p.asked = append(p.asked, label)
	if p.multi == nil {
		return nil, app.ErrNotInteractive
	}
	return p.multi, nil
}
func (p *scripted) Confirm(label string, def bool) (bool, error) {
	p.asked = append(p.asked, label)
	if len(p.confirms) == 0 {
		return def, nil
	}
	v := p.confirms[0]
	p.confirms = p.confirms[1:]
	return v, nil
}
func (p *scripted) Input(label, def string) (string, error) {
	p.asked = append(p.asked, label)
	if len(p.inputs) == 0 {
		return def, nil
	}
	v := p.inputs[0]
	p.inputs = p.inputs[1:]
	return v, nil
}

type fakeHost struct {
	err      error
	created  []string
	url      string
	accounts []app.Account
	repos    []app.RemoteRepo

	roots  []app.RemoteRepo // what a search by the root topic finds
	topics []string         // "<repo> <topic>" for each AddTopic

	signedOut bool // Available says to sign in, until Login is called
	logins    int
}

func (h *fakeHost) Name() string { return "GitHub" }
func (h *fakeHost) Accounts(context.Context) ([]app.Account, error) {
	return h.accounts, nil
}
func (h *fakeHost) Repos(context.Context, string) ([]app.RemoteRepo, error) {
	return h.repos, nil
}

func (h *fakeHost) Available(context.Context) error {
	if h.signedOut {
		return &app.HostUnavailable{Reason: "not signed in", CanSignIn: true}
	}
	return h.err
}
func (h *fakeHost) ReposWithTopic(context.Context, string) ([]app.RemoteRepo, error) {
	return h.roots, nil
}
func (h *fakeHost) AddTopic(_ context.Context, repoURL, topic string) error {
	h.topics = append(h.topics, repoURL+" "+topic)
	return nil
}
func (h *fakeHost) Owns(repoURL string) bool { return strings.Contains(repoURL, "example.test") }
func (h *fakeHost) OpenReview(context.Context, string, string) (*app.Review, error) {
	return nil, nil
}
func (h *fakeHost) Login(context.Context) error { h.logins++; h.signedOut = false; return nil }
func (h *fakeHost) Create(_ context.Context, name string, private bool) (string, error) {
	if !private {
		name += " (public)"
	}
	h.created = append(h.created, name)
	return h.url, nil
}

type memConfig struct{ c app.Config }

func (m *memConfig) Load() (app.Config, error) { return m.c, nil }
func (m *memConfig) Save(c app.Config) error   { m.c = c; return nil }
func (m *memConfig) Path() string              { return "/cfg/config.yaml" }

type quiet struct{ warns, infos []string }

func (q *quiet) Info(f string, a ...any)  { q.infos = append(q.infos, fmt.Sprintf(f, a...)) }
func (*quiet) OK(string, ...any)          {}
func (q *quiet) Warn(f string, a ...any)  { q.warns = append(q.warns, fmt.Sprintf(f, a...)) }
func (*quiet) Plain(string, ...any)       {}
func (*quiet) Table([]string, [][]string) {}

type rig struct {
	*app.Catalogs
	git    *fakeRepos
	host   *fakeHost
	prompt *scripted
	cfg    *memConfig
	out    *quiet
	loader fakeLoader
	root   string // local root catalog directory
	cache  string // where remote repositories are cloned
}

// newRig starts with a local root catalog and this machine pointing at it.
func newRig(t *testing.T, git *fakeRepos) *rig {
	t.Helper()
	root := t.TempDir()
	cache := t.TempDir()
	write(t, root, "catalog.yaml", string(catalog.Editor{}.Scaffold("me")))
	r := &rig{git: git, host: &fakeHost{url: "git@example.test:me/new.git"}, prompt: &scripted{}, out: &quiet{}, loader: fakeLoader{},
		cfg: &memConfig{c: app.Config{Catalog: filepath.Join(root, "catalog.yaml")}}, root: root, cache: cache}
	r.Catalogs = app.NewCatalogs(app.CatalogDeps{
		Git: git, Files: fsx.New(), Decoder: catalog.Decoder{}, Editor: catalog.Editor{},
		Config: r.cfg, Manifests: r.loader, Out: r.out, Prompt: r.prompt, Host: r.host, CacheDir: cache,
	})
	return r
}

func (r *rig) tree(t *testing.T) domain.Node {
	t.Helper()
	n, err := r.Tree(context.Background(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCreateCatalogInTheParentsRepository(t *testing.T) {
	git := &fakeRepos{inRepo: true}
	r := newRig(t, git)
	r.Yes = true
	_, err := r.CreateCatalog(context.Background(), app.CreateCatalogInput{Name: "personal", Same: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := r.tree(t).Find("personal"); !ok {
		t.Fatal("the new catalog is not reachable from the root")
	}
	if _, err := os.Stat(filepath.Join(r.root, "catalogs", "personal.yaml")); err != nil {
		t.Fatalf("child file missing: %v", err)
	}
	if len(git.commits) != 1 || !strings.Contains(git.commits[0], "catalog.yaml") || !strings.Contains(git.commits[0], "personal.yaml") {
		t.Errorf("both files belong in one commit, got %v", git.commits)
	}
	if len(git.pushed) != 0 {
		t.Errorf("a local root is never pushed for the user: %v", git.pushed)
	}
}

func TestCreateCatalogInANewPrivateRepository(t *testing.T) {
	empty := t.TempDir()
	git := &fakeRepos{repos: map[string]string{"git@example.test:me/new.git": empty}, emptyRepo: true}
	r := newRig(t, git)
	r.Yes = true
	_, err := r.CreateCatalog(context.Background(), app.CreateCatalogInput{Name: "acme", Where: app.RepoChoice{Create: "acme-index"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.host.created) != 1 || r.host.created[0] != "acme-index" {
		t.Errorf("created %v; repositories are private unless --public", r.host.created)
	}
	if len(git.pushed) != 1 || git.pushed[0] != "main" {
		t.Errorf("an empty repository should get its first push on main, got %v", git.pushed)
	}
	data, _ := os.ReadFile(filepath.Join(r.root, "catalog.yaml"))
	if !strings.Contains(string(data), "git@example.test:me/new.git") {
		t.Errorf("parent does not link the new repository:\n%s", data)
	}
}

func TestCreateCatalogReusesARepositoryThatAlreadyHasOne(t *testing.T) {
	existing := t.TempDir()
	write(t, existing, "catalog.yaml", string(catalog.Editor{}.Scaffold("acme")))
	git := &fakeRepos{repos: map[string]string{"git@example.test:acme/index.git": existing}}
	r := newRig(t, git)
	r.Yes = true
	_, err := r.CreateCatalog(context.Background(), app.CreateCatalogInput{Name: "acme", Where: app.RepoChoice{URL: "git@example.test:acme/index.git"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(git.pushed) != 0 || len(r.host.created) != 0 {
		t.Errorf("an existing catalog must be left alone: pushed=%v created=%v", git.pushed, r.host.created)
	}
	if _, ok := r.tree(t).Find("acme"); !ok {
		t.Error("acme not linked")
	}
}

func TestCreateCatalogAsksWhereWhenNothingSaysSo(t *testing.T) {
	git := &fakeRepos{inRepo: true}
	r := newRig(t, git)
	r.prompt.selects = []int{0} // same repository
	if _, err := r.CreateCatalog(context.Background(), app.CreateCatalogInput{Name: "personal"}); err != nil {
		t.Fatal(err)
	}
	if len(r.prompt.asked) == 0 || !strings.Contains(r.prompt.asked[0], "Where should the catalog live") {
		t.Errorf("asked %v", r.prompt.asked)
	}
}

func TestDecliningLeavesNothingBehind(t *testing.T) {
	git := &fakeRepos{inRepo: true}
	r := newRig(t, git)
	r.prompt.confirms = []bool{false}
	_, err := r.CreateCatalog(context.Background(), app.CreateCatalogInput{Name: "personal", Same: true})
	if !errors.Is(err, app.ErrCancelled) {
		t.Fatalf("err = %v, want ErrCancelled", err)
	}
	if _, serr := os.Stat(filepath.Join(r.root, "catalogs")); serr == nil {
		t.Error("a declined edit wrote files")
	}
	if len(git.commits) != 0 {
		t.Errorf("committed anyway: %v", git.commits)
	}
}

func TestCreateCatalogRefusesADuplicateName(t *testing.T) {
	r := newRig(t, &fakeRepos{})
	r.Yes = true
	in := app.CreateCatalogInput{Name: "personal", Same: true, At: "/", AtGiven: true}
	if _, err := r.CreateCatalog(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if _, err := r.CreateCatalog(context.Background(), in); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("err = %v", err)
	}
}

func TestAddMultiverseTakesTheNameFromItsManifest(t *testing.T) {
	repo := t.TempDir()
	write(t, repo, "multiverse.yaml", "apiVersion: multiverse/v1alpha1\nkind: Multiverse\nmetadata:\n  name: payments\n")
	git := &fakeRepos{repos: map[string]string{"git@example.test:acme/pay.git": repo}}
	r := newRig(t, git)
	r.Yes = true
	_, err := r.AddMultiverse(context.Background(), app.AddMultiverseInput{Repo: "git@example.test:acme/pay.git", At: "/", AtGiven: true})
	if err != nil {
		t.Fatal(err)
	}
	n, ok := r.tree(t).Find("payments")
	if !ok || n.Source.Repo != "git@example.test:acme/pay.git" {
		t.Fatalf("not listed: %+v", n)
	}
}

func TestAddMultiverseNeedsAManifest(t *testing.T) {
	git := &fakeRepos{repos: map[string]string{"git@example.test:acme/none.git": t.TempDir()}}
	r := newRig(t, git)
	r.Yes = true
	_, err := r.AddMultiverse(context.Background(), app.AddMultiverseInput{Repo: "git@example.test:acme/none.git", At: "/", AtGiven: true})
	if err == nil || !strings.Contains(err.Error(), "multiverse.yaml") {
		t.Errorf("err = %v", err)
	}
}

func TestAddCatalogRefusesAFileThatIsNotACatalog(t *testing.T) {
	repo := t.TempDir()
	write(t, repo, "catalog.yaml", "apiVersion: multiverse/v1alpha1\nkind: Multiverse\nmetadata: {name: x}\n")
	git := &fakeRepos{repos: map[string]string{"git@example.test:acme/bad.git": repo}}
	r := newRig(t, git)
	r.Yes = true
	_, err := r.AddCatalog(context.Background(), app.AddCatalogInput{Repo: "git@example.test:acme/bad.git", Name: "bad", At: "/", AtGiven: true})
	if err == nil || !strings.Contains(err.Error(), "unsupported kind") {
		t.Errorf("err = %v", err)
	}
}

func TestRemoveDropsOnlyTheReference(t *testing.T) {
	r := newRig(t, &fakeRepos{})
	r.Yes = true
	if _, err := r.CreateCatalog(context.Background(), app.CreateCatalogInput{Name: "personal", Same: true}); err != nil {
		t.Fatal(err)
	}
	if err := r.Remove(context.Background(), "personal"); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.tree(t).Find("personal"); ok {
		t.Error("still listed")
	}
	if _, err := os.Stat(filepath.Join(r.root, "catalogs", "personal.yaml")); err != nil {
		t.Error("the catalog file it pointed at must stay")
	}
	if err := r.Remove(context.Background(), ""); err == nil {
		t.Error("the root cannot be removed")
	}
}

func TestWritingToARemoteCatalogPushesToItsBranch(t *testing.T) {
	remote := t.TempDir()
	write(t, remote, "catalog.yaml", string(catalog.Editor{}.Scaffold("acme")))
	git := &fakeRepos{repos: map[string]string{"git@example.test:acme/index.git": remote}}
	r := newRig(t, git)
	r.Yes = true
	r.cfg.c.Catalog = "git@example.test:acme/index.git"

	repo := t.TempDir()
	write(t, repo, "multiverse.yaml", "apiVersion: multiverse/v1alpha1\nkind: Multiverse\nmetadata:\n  name: pay\n")
	git.repos["git@example.test:acme/pay.git"] = repo
	if _, err := r.AddMultiverse(context.Background(), app.AddMultiverseInput{Repo: "git@example.test:acme/pay.git", At: "/", AtGiven: true}); err != nil {
		t.Fatal(err)
	}
	if len(git.pushed) != 1 || git.pushed[0] != "main" {
		t.Errorf("pushed %v, want main", git.pushed)
	}
}

func TestARefusedPushIsReportedAndTheCloneReset(t *testing.T) {
	remote := t.TempDir()
	write(t, remote, "catalog.yaml", string(catalog.Editor{}.Scaffold("acme")))
	git := &fakeRepos{repos: map[string]string{"git@example.test:acme/index.git": remote}, pushErr: errors.New("rejected")}
	r := newRig(t, git)
	r.Yes = true
	r.cfg.c.Catalog = "git@example.test:acme/index.git"
	repo := t.TempDir()
	write(t, repo, "multiverse.yaml", "apiVersion: multiverse/v1alpha1\nkind: Multiverse\nmetadata:\n  name: pay\n")
	git.repos["git@example.test:acme/pay.git"] = repo

	_, err := r.AddMultiverse(context.Background(), app.AddMultiverseInput{Repo: "git@example.test:acme/pay.git", At: "/", AtGiven: true})
	if err == nil || !strings.Contains(err.Error(), "rejected") {
		t.Fatalf("err = %v", err)
	}
	if len(git.fetched) == 0 {
		t.Error("the clone should be refreshed after a refused push")
	}
}

func TestInitCreatesAPrivateRepositoryAndRecordsIt(t *testing.T) {
	empty := t.TempDir()
	git := &fakeRepos{repos: map[string]string{"git@example.test:me/new.git": empty}, emptyRepo: true}
	r := newRig(t, git)
	r.cfg.c = app.Config{}
	r.Yes = true
	if err := r.Init(context.Background(), app.InitInput{Where: app.RepoChoice{Create: "multiverse-index"}}); err != nil {
		t.Fatal(err)
	}
	if r.cfg.c.Catalog != "git@example.test:me/new.git" {
		t.Errorf("root catalog = %q", r.cfg.c.Catalog)
	}
	if len(git.pushed) != 1 {
		t.Errorf("pushed %v", git.pushed)
	}
}

func TestInitWithoutGhExplainsTheWayOut(t *testing.T) {
	r := newRig(t, &fakeRepos{})
	r.cfg.c = app.Config{}
	r.Yes = true
	r.host.err = errors.New("the GitHub CLI (gh) is not installed")
	err := r.Init(context.Background(), app.InitInput{Where: app.RepoChoice{Create: "x"}})
	if err == nil || !strings.Contains(err.Error(), "--repo") {
		t.Errorf("err = %v; it should point at --repo", err)
	}
	if r.cfg.c.Catalog != "" {
		t.Error("nothing should be recorded")
	}
}

func TestInitLocalStartsAGitRepositoryNobodyPushedYet(t *testing.T) {
	git := &fakeRepos{}
	r := newRig(t, git)
	r.cfg.c = app.Config{}
	r.Yes = true
	dir := filepath.Join(t.TempDir(), "index")
	if err := r.Init(context.Background(), app.InitInput{Local: dir}); err != nil {
		t.Fatal(err)
	}
	if r.cfg.c.Catalog != filepath.Join(dir, "catalog.yaml") {
		t.Errorf("root catalog = %q", r.cfg.c.Catalog)
	}
	if len(git.pushed) != 0 {
		t.Error("a local-only catalog has nowhere to push")
	}
}

func TestInitAsksBeforeReplacingTheRoot(t *testing.T) {
	r := newRig(t, &fakeRepos{})
	r.prompt.confirms = []bool{false}
	err := r.Init(context.Background(), app.InitInput{Local: t.TempDir()})
	if !errors.Is(err, app.ErrCancelled) {
		t.Errorf("err = %v", err)
	}
}

func manifestRepo(t *testing.T, name string) string {
	t.Helper()
	d := t.TempDir()
	write(t, d, "multiverse.yaml", "apiVersion: multiverse/v1alpha1\nkind: Multiverse\nmetadata:\n  name: "+name+"\n")
	return d
}

func TestAddLinksARepositoryWithACatalogAsACatalog(t *testing.T) {
	proj := manifestRepo(t, "billing")
	// The project's own index lists its own project, in the same repository.
	out, _ := catalog.Editor{}.AddMultiverseRef([]byte(catalog.Editor{}.Scaffold("billing")), domain.MultiverseRef{Name: "app", Source: domain.Source{Path: "multiverse.yaml"}})
	write(t, proj, "catalog.yaml", string(out))
	git := &fakeRepos{repos: map[string]string{"https://github.com/acme-org/multiverse.git": proj}}
	r := newRig(t, git)
	r.Yes = true

	node, err := r.Add(context.Background(), app.AddInput{Repo: "acme-org/multiverse", At: "/", AtGiven: true})
	if err != nil {
		t.Fatal(err)
	}
	if node != "billing" {
		t.Errorf("node = %q", node)
	}
	leaf, ok := r.tree(t).Find("billing/app")
	if !ok || leaf.Source.Repo != "https://github.com/acme-org/multiverse.git" || leaf.Source.Path != "multiverse.yaml" {
		t.Errorf("the project's own project should resolve to its repository: %+v", leaf)
	}
}

func TestAddListsABareManifestRepositoryAsAMultiverse(t *testing.T) {
	git := &fakeRepos{repos: map[string]string{"https://github.com/acme/solo.git": manifestRepo(t, "solo")}}
	r := newRig(t, git)
	r.Yes = true
	node, err := r.Add(context.Background(), app.AddInput{Repo: "acme/solo", At: "/", AtGiven: true})
	if err != nil || node != "solo" {
		t.Fatalf("node=%q err=%v", node, err)
	}
	if n, ok := r.tree(t).Find("solo"); !ok || n.Kind != domain.MultiverseNode {
		t.Errorf("not a project: %+v", n)
	}
}

func TestAddTwiceIsANoOp(t *testing.T) {
	git := &fakeRepos{repos: map[string]string{"https://github.com/acme/solo.git": manifestRepo(t, "solo")}}
	r := newRig(t, git)
	r.Yes = true
	in := app.AddInput{Repo: "acme/solo", At: "/", AtGiven: true}
	if _, err := r.Add(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	before := len(git.commits)
	if node, err := r.Add(context.Background(), in); err != nil || node != "solo" {
		t.Fatalf("second add: node=%q err=%v", node, err)
	}
	if len(git.commits) != before {
		t.Error("adding the same repository again must not commit")
	}
}

func TestAddNeedsARootToWriteTo(t *testing.T) {
	git := &fakeRepos{repos: map[string]string{"https://github.com/acme/solo.git": manifestRepo(t, "solo")}}
	r := newRig(t, git)
	r.cfg.c = app.Config{}
	_, err := r.Add(context.Background(), app.AddInput{Repo: "acme/solo"})
	if err == nil || !strings.Contains(err.Error(), "multiversectl init") {
		t.Fatalf("err = %v; writing to an index needs one, and the error should say how to make it", err)
	}
}

func TestAddNeedsSomethingToAdd(t *testing.T) {
	git := &fakeRepos{repos: map[string]string{"https://github.com/acme/empty.git": t.TempDir()}}
	r := newRig(t, git)
	r.Yes = true
	if _, err := r.Add(context.Background(), app.AddInput{Repo: "acme/empty", At: "/", AtGiven: true}); err == nil || !strings.Contains(err.Error(), "multiverse.yaml") {
		t.Errorf("err = %v", err)
	}
}

const wsYAML = "apiVersion: multiverse/v1alpha1\nkind: Multiverse\nmetadata:\n  name: billing\nspec:\n  root: /home/me/billing\n"

func TestCreateCatalogWithAManifestMakesTheProjectItsOwnMultiverse(t *testing.T) {
	src := t.TempDir()
	write(t, src, "billing.yaml", wsYAML)
	empty := t.TempDir()
	git := &fakeRepos{repos: map[string]string{"git@example.test:me/new.git": empty}, emptyRepo: true}
	r := newRig(t, git)
	r.Yes = true
	_, err := r.CreateCatalog(context.Background(), app.CreateCatalogInput{
		Name: "billing", Manifest: filepath.Join(src, "billing.yaml"), Where: app.RepoChoice{Create: "acme-org/multiverse"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(git.commits) != 1 || !strings.Contains(git.commits[0], "catalog.yaml") || !strings.Contains(git.commits[0], "multiverse.yaml") {
		t.Errorf("the catalog and the manifest belong in one commit: %v", git.commits)
	}
	leaf, ok := r.tree(t).Find("billing/billing")
	if !ok || leaf.Source.Path != "multiverse.yaml" || leaf.Source.Repo != "git@example.test:me/new.git" {
		t.Errorf("the project's project should live in its own repository: %+v", leaf)
	}
}

func TestCreateCatalogWithAManifestInTheParentsRepository(t *testing.T) {
	src := t.TempDir()
	write(t, src, "billing.yaml", wsYAML)
	r := newRig(t, &fakeRepos{})
	r.Yes = true
	if _, err := r.CreateCatalog(context.Background(), app.CreateCatalogInput{Name: "billing", Same: true, Manifest: filepath.Join(src, "billing.yaml")}); err != nil {
		t.Fatal(err)
	}
	leaf, ok := r.tree(t).Find("billing/billing")
	if !ok || !strings.HasSuffix(leaf.Source.Path, "/catalogs/billing/multiverse.yaml") {
		t.Fatalf("leaf = %+v", leaf)
	}
	if _, err := os.Stat(filepath.Join(r.root, "catalogs", "billing", "multiverse.yaml")); err != nil {
		t.Errorf("manifest not copied: %v", err)
	}
}

func TestCreateCatalogWithAManifestRefusesARepositoryThatAlreadyHasACatalog(t *testing.T) {
	src := t.TempDir()
	write(t, src, "billing.yaml", wsYAML)
	existing := t.TempDir()
	write(t, existing, "catalog.yaml", string(catalog.Editor{}.Scaffold("x")))
	git := &fakeRepos{repos: map[string]string{"git@example.test:me/x.git": existing}}
	r := newRig(t, git)
	r.Yes = true
	_, err := r.CreateCatalog(context.Background(), app.CreateCatalogInput{Name: "billing", Manifest: filepath.Join(src, "billing.yaml"), Where: app.RepoChoice{URL: "git@example.test:me/x.git"}})
	if err == nil || !strings.Contains(err.Error(), "catalog add") {
		t.Errorf("err = %v", err)
	}
}

func TestAddAPathToACheckoutUsesItsOrigin(t *testing.T) {
	checkout := manifestRepo(t, "solo")
	git := &fakeRepos{repos: map[string]string{"git@example.test:acme/solo.git": checkout}, remotes: map[string]string{checkout: "git@example.test:acme/solo.git"}, inRepo: true}
	r := newRig(t, git)
	r.Yes = true
	node, err := r.Add(context.Background(), app.AddInput{Repo: checkout, At: "/", AtGiven: true})
	if err != nil || node != "solo" {
		t.Fatalf("node=%q err=%v", node, err)
	}
	if n, ok := r.tree(t).Find("solo"); !ok || n.Source.Repo != "git@example.test:acme/solo.git" {
		t.Errorf("a path must be listed by its origin, not by a path only this machine has: %+v", n)
	}
}

func TestAddAPathNeedsSomethingOthersCanFetch(t *testing.T) {
	checkout := manifestRepo(t, "solo")
	r := newRig(t, &fakeRepos{inRepo: true})
	r.Yes = true
	if _, err := r.Add(context.Background(), app.AddInput{Repo: checkout, At: "/", AtGiven: true}); err == nil || !strings.Contains(err.Error(), "no origin remote") {
		t.Errorf("no remote: err = %v", err)
	}
	r2 := newRig(t, &fakeRepos{})
	r2.Yes = true
	if _, err := r2.Add(context.Background(), app.AddInput{Repo: checkout, At: "/", AtGiven: true}); err == nil || !strings.Contains(err.Error(), "not in a git repository") {
		t.Errorf("not a repository: err = %v", err)
	}
}

func TestAddAManifestFileCopiesItIntoTheCatalogsRepository(t *testing.T) {
	src := t.TempDir()
	write(t, src, "billing.yaml", wsYAML)
	git := &fakeRepos{inRepo: true}
	r := newRig(t, git)
	r.Yes = true
	in := app.AddInput{Repo: filepath.Join(src, "billing.yaml"), At: "/", AtGiven: true}
	node, err := r.Add(context.Background(), in)
	if err != nil || node != "billing" {
		t.Fatalf("node=%q err=%v", node, err)
	}
	leaf, ok := r.tree(t).Find("billing")
	if !ok || !strings.HasSuffix(leaf.Source.Path, "/multiverse/billing.yaml") {
		t.Fatalf("leaf = %+v", leaf)
	}
	if _, err := os.Stat(filepath.Join(r.root, "multiverse", "billing.yaml")); err != nil {
		t.Errorf("manifest not copied: %v", err)
	}
	before := len(git.commits)
	if _, err := r.Add(context.Background(), in); err != nil || len(git.commits) != before {
		t.Errorf("adding it again must be a no-op: err=%v commits=%v", err, git.commits)
	}
}

func TestAddAFileThatIsNeitherIsRefused(t *testing.T) {
	src := t.TempDir()
	write(t, src, "notes.yaml", "just: notes\n")
	r := newRig(t, &fakeRepos{})
	r.Yes = true
	if _, err := r.Add(context.Background(), app.AddInput{Repo: filepath.Join(src, "notes.yaml"), At: "/", AtGiven: true}); err == nil || !strings.Contains(err.Error(), "neither a catalog nor a multiverse") {
		t.Errorf("err = %v", err)
	}
}

func TestAddReadsCatalogYamlRatherThanTrustingItsName(t *testing.T) {
	// A project whose repository also holds a catalog.yaml of someone else's
	// kind. It is a project, and reading the name alone sent it down the
	// catalog path, where it could never be added at all.
	proj := manifestRepo(t, "billing")
	write(t, proj, "catalog.yaml", "apiVersion: backstage.io/v1alpha1\nkind: Component\nmetadata:\n  name: billing\n")
	git := &fakeRepos{repos: map[string]string{"https://github.com/acme/billing.git": proj}}
	r := newRig(t, git)
	r.Yes = true

	if _, err := r.Add(context.Background(), app.AddInput{Repo: "acme/billing", At: "/", AtGiven: true}); err != nil {
		t.Fatal(err)
	}
	if n, ok := r.tree(t).Find("billing"); !ok || n.Kind != domain.MultiverseNode || n.Source.Path != "multiverse.yaml" {
		t.Errorf("it should be listed as a project: %+v", n)
	}
}

func TestAddTakesTheDirectoryTheManifestIsIn(t *testing.T) {
	repo := t.TempDir()
	write(t, repo, filepath.Join("services", "billing", "multiverse.yaml"),
		"apiVersion: multiverse/v1alpha1\nkind: Multiverse\nmetadata:\n  name: billing\n")
	git := &fakeRepos{repos: map[string]string{"https://github.com/acme/mono.git": repo}}
	r := newRig(t, git)
	r.Yes = true

	node, err := r.Add(context.Background(), app.AddInput{Repo: "acme/mono", Path: "services/billing", At: "/", AtGiven: true})
	if err != nil {
		t.Fatalf("--path may name the directory the manifest sits in: %v", err)
	}
	if node != "billing" {
		t.Errorf("node = %q", node)
	}
	if n, ok := r.tree(t).Find("billing"); !ok || n.Source.Path != "services/billing/multiverse.yaml" {
		t.Errorf("the entry should point at the manifest inside the directory: %+v", n)
	}
}

// remoteRig is a root catalog on a remote with one project listed in it.
func remoteRig(t *testing.T, at string) (*rig, *fakeRepos) {
	t.Helper()
	remote := t.TempDir()
	write(t, remote, "catalog.yaml", string(catalog.Editor{}.Scaffold("acme")))
	git := &fakeRepos{repos: map[string]string{"git@example.test:acme/index.git": remote}}
	r := newRig(t, git)
	r.Yes = true
	r.cfg.c.Catalog = "git@example.test:acme/index.git"
	repo := t.TempDir()
	write(t, repo, "multiverse.yaml", "apiVersion: multiverse/v1alpha1\nkind: Multiverse\nmetadata:\n  name: pay\n")
	git.repos["git@example.test:acme/pay.git"] = repo
	if at != "/" {
		if _, err := r.CreateCatalog(context.Background(), app.CreateCatalogInput{Name: at, Same: true}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.AddMultiverse(context.Background(), app.AddMultiverseInput{Repo: "git@example.test:acme/pay.git", At: at, AtGiven: true}); err != nil {
		t.Fatal(err)
	}
	return r, git
}

// Taking something out of a catalog other people read is a commit on a branch
// they update from. It says so, and what goes and what stays, and nothing is
// pushed on a bare Enter.
func TestRemovingFromARemoteCatalogSaysWhatWillHappenAndDefaultsToNo(t *testing.T) {
	r, git := remoteRig(t, "/")
	pushedBefore := len(git.pushed)
	r.Yes = false // an Enter, as at a terminal

	err := r.Remove(context.Background(), "pay")
	if !errors.Is(err, app.ErrCancelled) {
		t.Fatalf("want it cancelled on a bare Enter, got %v", err)
	}
	if len(git.pushed) != pushedBefore {
		t.Errorf("it pushed without being told to: %v", git.pushed)
	}
	said := strings.Join(r.out.infos, "\n")
	for _, want := range []string{
		"remove pay from the catalog",
		"removes: the entry pay (a multiverse)",
		"the repository it points at is not touched",
		"pushed straight to main of",
		"not through a pull request",
		"git revert",
	} {
		if !strings.Contains(said, want) {
			t.Errorf("want %q said before asking:\n%s", want, said)
		}
	}
}

func TestRemovingGoesAheadOnceTheyAgree(t *testing.T) {
	r, git := remoteRig(t, "/")
	pushedBefore := len(git.pushed)
	r.Yes = false
	r.prompt.confirms = []bool{true}

	if err := r.Remove(context.Background(), "pay"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(git.pushed) != pushedBefore+1 {
		t.Errorf("want one push, got %v", git.pushed)
	}
	if _, ok := r.tree(t).Find("pay"); ok {
		t.Error("still listed")
	}
}

// The name on the command line is one word, and a catalog takes everything
// under it along. That is the part that is easy to miss.
func TestRemovingACatalogNamesTheProjectsThatGoWithIt(t *testing.T) {
	r, _ := remoteRig(t, "team")
	r.Yes = false

	if err := r.Remove(context.Background(), "team"); !errors.Is(err, app.ErrCancelled) {
		t.Fatalf("want it cancelled, got %v", err)
	}
	said := strings.Join(r.out.infos, "\n")
	for _, want := range []string{
		"the entry team (a catalog)",
		"everything listed under it",
		"1 multiverse: team/pay",
		"the repositories it points at",
	} {
		if !strings.Contains(said, want) {
			t.Errorf("want %q:\n%s", want, said)
		}
	}
}

// Where nothing can undo it, the plan says that instead of promising a revert.
func TestRemovingFromAFileOutsideGitSaysNothingCanBringItBack(t *testing.T) {
	r := newRig(t, &fakeRepos{})
	r.Yes = true
	if _, err := r.CreateCatalog(context.Background(), app.CreateCatalogInput{Name: "personal", Same: true}); err != nil {
		t.Fatal(err)
	}
	r.Yes = false
	if err := r.Remove(context.Background(), "personal"); !errors.Is(err, app.ErrCancelled) {
		t.Fatalf("want it cancelled, got %v", err)
	}
	said := strings.Join(r.out.infos, "\n")
	if !strings.Contains(said, "not in a git repository") || strings.Contains(said, "git revert") {
		t.Errorf("want it said that nothing can bring it back:\n%s", said)
	}
}

// Adding is easy to take back, and keeps its one-line question and its yes.
func TestAddingStillAsksOneQuestionAndDefaultsToYes(t *testing.T) {
	remote := t.TempDir()
	write(t, remote, "catalog.yaml", string(catalog.Editor{}.Scaffold("acme")))
	git := &fakeRepos{repos: map[string]string{"git@example.test:acme/index.git": remote}}
	r := newRig(t, git)
	r.cfg.c.Catalog = "git@example.test:acme/index.git"
	repo := t.TempDir()
	write(t, repo, "multiverse.yaml", "apiVersion: multiverse/v1alpha1\nkind: Multiverse\nmetadata:\n  name: pay\n")
	git.repos["git@example.test:acme/pay.git"] = repo

	if _, err := r.AddMultiverse(context.Background(), app.AddMultiverseInput{Repo: "git@example.test:acme/pay.git", At: "/", AtGiven: true}); err != nil {
		t.Fatalf("a bare Enter should have added it: %v", err)
	}
	if strings.Contains(strings.Join(r.out.infos, "\n"), "not through a pull request") {
		t.Error("an addition should not be given the speech meant for a removal")
	}
}
