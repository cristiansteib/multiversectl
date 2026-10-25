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

type scripted struct {
	selects  []int
	inputs   []string
	confirms []bool
	asked    []string
}

func (p *scripted) Select(label string, _ []string, def int) (int, error) {
	p.asked = append(p.asked, label)
	if len(p.selects) == 0 {
		return 0, app.ErrNotInteractive
	}
	n := p.selects[0]
	p.selects = p.selects[1:]
	return n, nil
}
func (p *scripted) MultiSelect(label string, _ []string) ([]int, error) {
	p.asked = append(p.asked, label)
	return nil, app.ErrNotInteractive
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
	err     error
	created []string
	url     string
}

func (h *fakeHost) Available(context.Context) error { return h.err }
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

type quiet struct{ warns []string }

func (*quiet) Info(string, ...any)        {}
func (*quiet) OK(string, ...any)          {}
func (q *quiet) Warn(f string, a ...any)  { q.warns = append(q.warns, f) }
func (*quiet) Plain(string, ...any)       {}
func (*quiet) Table([]string, [][]string) {}

type rig struct {
	*app.Catalogs
	git    *fakeRepos
	host   *fakeHost
	prompt *scripted
	cfg    *memConfig
	root   string // local root catalog directory
}

// newRig starts with a local root catalog and this machine pointing at it.
func newRig(t *testing.T, git *fakeRepos) *rig {
	t.Helper()
	root := t.TempDir()
	write(t, root, "catalog.yaml", string(catalog.Editor{}.Scaffold("me")))
	r := &rig{git: git, host: &fakeHost{url: "git@example.test:me/new.git"}, prompt: &scripted{},
		cfg: &memConfig{c: app.Config{Catalog: filepath.Join(root, "catalog.yaml")}}, root: root}
	r.Catalogs = app.NewCatalogs(app.CatalogDeps{
		Git: git, Files: fsx.New(), Decoder: catalog.Decoder{}, Editor: catalog.Editor{},
		Config: r.cfg, Out: &quiet{}, Prompt: r.prompt, Host: r.host, CacheDir: t.TempDir(),
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
	err := r.CreateCatalog(context.Background(), app.CreateCatalogInput{Name: "personal", Same: true})
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
	err := r.CreateCatalog(context.Background(), app.CreateCatalogInput{Name: "acme", Where: app.RepoChoice{Create: "acme-index"}})
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
	err := r.CreateCatalog(context.Background(), app.CreateCatalogInput{Name: "acme", Where: app.RepoChoice{URL: "git@example.test:acme/index.git"}})
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
	if err := r.CreateCatalog(context.Background(), app.CreateCatalogInput{Name: "personal"}); err != nil {
		t.Fatal(err)
	}
	if len(r.prompt.asked) == 0 || !strings.Contains(r.prompt.asked[0], "Where should the new catalog live") {
		t.Errorf("asked %v", r.prompt.asked)
	}
}

func TestDecliningLeavesNothingBehind(t *testing.T) {
	git := &fakeRepos{inRepo: true}
	r := newRig(t, git)
	r.prompt.confirms = []bool{false}
	err := r.CreateCatalog(context.Background(), app.CreateCatalogInput{Name: "personal", Same: true})
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
	if err := r.CreateCatalog(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if err := r.CreateCatalog(context.Background(), in); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("err = %v", err)
	}
}

func TestAddWorkspaceTakesTheNameFromItsManifest(t *testing.T) {
	repo := t.TempDir()
	write(t, repo, "multiverse.yaml", "apiVersion: multiverse/v1alpha1\nkind: Workspace\nmetadata:\n  name: payments\n")
	git := &fakeRepos{repos: map[string]string{"git@example.test:acme/pay.git": repo}}
	r := newRig(t, git)
	r.Yes = true
	_, err := r.AddWorkspace(context.Background(), app.AddWorkspaceInput{Repo: "git@example.test:acme/pay.git", At: "/", AtGiven: true})
	if err != nil {
		t.Fatal(err)
	}
	n, ok := r.tree(t).Find("payments")
	if !ok || n.Source.Repo != "git@example.test:acme/pay.git" {
		t.Fatalf("not listed: %+v", n)
	}
}

func TestAddWorkspaceNeedsAManifest(t *testing.T) {
	git := &fakeRepos{repos: map[string]string{"git@example.test:acme/none.git": t.TempDir()}}
	r := newRig(t, git)
	r.Yes = true
	_, err := r.AddWorkspace(context.Background(), app.AddWorkspaceInput{Repo: "git@example.test:acme/none.git", At: "/", AtGiven: true})
	if err == nil || !strings.Contains(err.Error(), "multiverse.yaml") {
		t.Errorf("err = %v", err)
	}
}

func TestAddCatalogRefusesAFileThatIsNotACatalog(t *testing.T) {
	repo := t.TempDir()
	write(t, repo, "catalog.yaml", "apiVersion: multiverse/v1alpha1\nkind: Workspace\nmetadata: {name: x}\n")
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
	if err := r.CreateCatalog(context.Background(), app.CreateCatalogInput{Name: "personal", Same: true}); err != nil {
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
	write(t, repo, "multiverse.yaml", "apiVersion: multiverse/v1alpha1\nkind: Workspace\nmetadata:\n  name: pay\n")
	git.repos["git@example.test:acme/pay.git"] = repo
	if _, err := r.AddWorkspace(context.Background(), app.AddWorkspaceInput{Repo: "git@example.test:acme/pay.git", At: "/", AtGiven: true}); err != nil {
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
	write(t, repo, "multiverse.yaml", "apiVersion: multiverse/v1alpha1\nkind: Workspace\nmetadata:\n  name: pay\n")
	git.repos["git@example.test:acme/pay.git"] = repo

	_, err := r.AddWorkspace(context.Background(), app.AddWorkspaceInput{Repo: "git@example.test:acme/pay.git", At: "/", AtGiven: true})
	if err == nil || !strings.Contains(err.Error(), "refused") {
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

func TestLeavesTakeASubtreeOrOneWorkspace(t *testing.T) {
	r := newRig(t, &fakeRepos{})
	tree := domain.Node{Name: "me", Kind: domain.CatalogNode, Children: []domain.Node{
		{Name: "acme", Path: "acme", Kind: domain.CatalogNode, Children: []domain.Node{
			{Name: "a", Path: "acme/a", Kind: domain.WorkspaceNode},
			{Name: "b", Path: "acme/b", Kind: domain.WorkspaceNode},
		}},
		{Name: "gone", Path: "gone", Kind: domain.CatalogNode, Unavailable: "no access"},
	}}
	got, err := r.Leaves(tree, []string{"acme", "acme/a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("got %d leaves, want 2 with the duplicate dropped", len(got))
	}
	if _, err := r.Leaves(tree, []string{"gone"}); err == nil {
		t.Error("an unavailable catalog cannot be installed")
	}
	if _, err := r.Leaves(tree, []string{"nope"}); err == nil {
		t.Error("an unknown path must fail")
	}
	if _, err := r.Leaves(tree, nil); !errors.Is(err, app.ErrNotInteractive) {
		t.Errorf("no paths and no terminal: err = %v", err)
	}
	if app.WorkspaceSlug("acme/backend/pay") != "acme-backend-pay" {
		t.Error("slug")
	}
	if app.WorkspaceSlug("fichero/fichero") != "fichero" || app.WorkspacePath("fichero/fichero") != "fichero" {
		t.Error("a repeated segment should collapse")
	}
	if app.WorkspaceSlug("a/b/a") != "a-b-a" {
		t.Error("only adjacent repeats collapse")
	}
}

func manifestRepo(t *testing.T, name string) string {
	t.Helper()
	d := t.TempDir()
	write(t, d, "multiverse.yaml", "apiVersion: multiverse/v1alpha1\nkind: Workspace\nmetadata:\n  name: "+name+"\n")
	return d
}

func TestAddLinksARepositoryWithACatalogAsACatalog(t *testing.T) {
	proj := manifestRepo(t, "fichero")
	// The project's own index lists its own workspace, in the same repository.
	out, _ := catalog.Editor{}.AddWorkspaceRef([]byte(catalog.Editor{}.Scaffold("fichero")), domain.WorkspaceRef{Name: "app", Source: domain.Source{Path: "multiverse.yaml"}})
	write(t, proj, "catalog.yaml", string(out))
	git := &fakeRepos{repos: map[string]string{"https://github.com/fichero-app/multiverse.git": proj}}
	r := newRig(t, git)
	r.Yes = true

	node, err := r.Add(context.Background(), app.AddInput{Repo: "fichero-app/multiverse", At: "/", AtGiven: true})
	if err != nil {
		t.Fatal(err)
	}
	if node != "fichero" {
		t.Errorf("node = %q", node)
	}
	leaf, ok := r.tree(t).Find("fichero/app")
	if !ok || leaf.Source.Repo != "https://github.com/fichero-app/multiverse.git" || leaf.Source.Path != "multiverse.yaml" {
		t.Errorf("the project's own workspace should resolve to its repository: %+v", leaf)
	}
}

func TestAddListsABareManifestRepositoryAsAWorkspace(t *testing.T) {
	git := &fakeRepos{repos: map[string]string{"https://github.com/acme/solo.git": manifestRepo(t, "solo")}}
	r := newRig(t, git)
	r.Yes = true
	node, err := r.Add(context.Background(), app.AddInput{Repo: "acme/solo", At: "/", AtGiven: true})
	if err != nil || node != "solo" {
		t.Fatalf("node=%q err=%v", node, err)
	}
	if n, ok := r.tree(t).Find("solo"); !ok || n.Kind != domain.WorkspaceNode {
		t.Errorf("not a workspace: %+v", n)
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

func TestAddSetsUpARootFirstWhenThereIsNone(t *testing.T) {
	git := &fakeRepos{repos: map[string]string{"https://github.com/acme/solo.git": manifestRepo(t, "solo")}}
	r := newRig(t, git)
	r.cfg.c = app.Config{}
	_, err := r.Add(context.Background(), app.AddInput{Repo: "acme/solo"})
	if !errors.Is(err, app.ErrNotInteractive) {
		t.Fatalf("err = %v; without a terminal init cannot ask where the root lives", err)
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
