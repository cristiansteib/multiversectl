package app_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/adapters/catalog"
	"github.com/cristiansteib/multiversectl/internal/adapters/fsx"
	"github.com/cristiansteib/multiversectl/internal/adapters/manifest"
	"github.com/cristiansteib/multiversectl/internal/app"
	"github.com/cristiansteib/multiversectl/internal/domain"
)

type realLoader struct{}

func (realLoader) Load(p string) (*domain.Manifest, error) { return manifest.Load(p) }

// projectRig is the catalog rig with the real manifest writer and loader, so
// what a project init writes is checked by the code that will read it.
func projectRig(t *testing.T, git *fakeRepos) *rig {
	t.Helper()
	r := newRig(t, git)
	r.Catalogs = app.NewCatalogs(app.CatalogDeps{
		Git: git, Files: fsx.New(), Decoder: catalog.Decoder{}, Editor: catalog.Editor{},
		Config: r.cfg, Manifests: realLoader{}, Writer: manifest.Writer{}, Out: r.out, Prompt: r.prompt, Host: r.host,
		CacheDir: t.TempDir(),
	})
	return r
}

func read1(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestProjectInitCreatesTheRepositoryPushesAndLinksIt(t *testing.T) {
	git := &fakeRepos{inRepo: true, repos: map[string]string{}}
	r := projectRig(t, git)
	r.Yes = true
	dir := filepath.Join(t.TempDir(), "multiverse")
	git.repos[r.host.url] = dir // what the host serves once the project is pushed

	res, err := r.InitProject(context.Background(), app.ProjectInput{
		Dir: dir, Name: "acme", Create: "acme-org/multiverse",
		Repos: []string{"https://example.test/acme/acme-api.git", "acme/acme-web"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.host.created) != 1 || r.host.created[0] != "acme-org/multiverse" {
		t.Errorf("created %v, want a private acme-org/multiverse", r.host.created)
	}
	m, err := manifest.Load(filepath.Join(dir, "multiverse.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Repos["api"].URL != "https://example.test/acme/acme-api.git" || m.Repos["web"].URL != "https://github.com/acme/acme-web.git" {
		t.Errorf("the project's name should be dropped from the keys, and owner/repo means GitHub: %+v", m.Repos)
	}
	if _, err := os.Stat(filepath.Join(dir, "catalog.yaml")); err == nil {
		t.Error("a multiverse's repository is not a catalog: nothing lists itself")
	}
	agents := read1(t, filepath.Join(dir, "AGENTS.md"))
	for _, want := range []string{"**acme**", "multiverse.yaml", "start " + r.host.url, "universe up", "acme-api"} {
		if !strings.Contains(agents, want) {
			t.Errorf("AGENTS.md lacks %q:\n%s", want, agents)
		}
	}
	if len(git.commits) < 1 || !strings.Contains(git.commits[0], "AGENTS.md") {
		t.Errorf("AGENTS.md must be committed with the rest: %v", git.commits)
	}
	if git.remotes[dir] != r.host.url || len(git.pushed) != 1 || len(git.commits) < 1 {
		t.Errorf("origin %q, pushed %v, commits %v", git.remotes[dir], git.pushed, git.commits)
	}
	if res.URL != r.host.url || res.Node == "" {
		t.Errorf("result = %+v, want the new repository listed in the root catalog", res)
	}
	if _, ok := r.tree(t).Find(res.Node); !ok {
		t.Errorf("%s is not in the root catalog", res.Node)
	}
}

func TestProjectInitUsesTheCheckoutsFoundBesideIt(t *testing.T) {
	ws := t.TempDir()
	git := &fakeRepos{inRepo: true, remotes: map[string]string{
		filepath.Join(ws, "acme-api"):  "git@example.test:acme/acme-api.git",
		filepath.Join(ws, "acme-docs"): "git@example.test:acme/acme-docs.git",
	}}
	r := projectRig(t, git)
	r.Yes = true
	write(t, ws, "acme-api/.git/HEAD", "")
	write(t, ws, "acme-api/docker-compose.yml", "services: {}\n")
	write(t, ws, "acme-docs/.git/HEAD", "")
	write(t, ws, "stray/.git/HEAD", "") // no origin: nobody else could clone it

	dir := filepath.Join(ws, "multiverse")
	// No --name: acme-api and acme-docs share their first word.
	res, err := r.InitProject(context.Background(), app.ProjectInput{Dir: dir, Scan: true, Local: true})
	if err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Load(filepath.Join(dir, "multiverse.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Repos) != 2 || m.Repos["api"].Dir != "acme-api" || m.Repos["docs"].Dir != "acme-docs" {
		t.Errorf("repos = %+v: want api and docs; stray has no origin", m.Repos)
	}
	if res.Root != ws {
		t.Errorf("root = %q, want %q: the checkouts already sit there", res.Root, ws)
	}
	if res.URL != "" || res.Node != "" || len(git.pushed) != 0 {
		t.Errorf("a local-only project must not be pushed or listed: %+v, pushed %v", res, git.pushed)
	}
}

func TestProjectInitGuideSearchesAnAccountAndPastesURLs(t *testing.T) {
	git := &fakeRepos{inRepo: true}
	r := projectRig(t, git)
	r.host.accounts = []app.Account{{Name: "me"}, {Name: "acme-org", Org: true}}
	r.host.repos = []app.RemoteRepo{
		{Name: "acme-org/acme-api", URL: "https://github.com/acme-org/acme-api.git", Private: true},
		{Name: "acme-org/other", URL: "https://github.com/acme-org/other.git"},
	}
	// Search (account 1, first match), paste, done, then keep it local.
	r.prompt.selects = []int{0, 1, 1, 2, 2}
	r.prompt.inputs = []string{"https://example.test/acme/acme-web.git"}
	r.prompt.multi = []int{0}
	dir := filepath.Join(t.TempDir(), "multiverse")

	if _, err := r.InitProject(context.Background(), app.ProjectInput{Dir: dir, Name: "acme"}); err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Load(filepath.Join(dir, "multiverse.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Repos) != 2 || m.Repos["api"].URL != "https://github.com/acme-org/acme-api.git" || m.Repos["web"].URL == "" {
		t.Errorf("repos = %+v, want the searched api and the pasted web", m.Repos)
	}
}

func TestProjectInitRefusesADirectoryThatIsAlreadyAProject(t *testing.T) {
	r := projectRig(t, &fakeRepos{inRepo: true})
	dir := t.TempDir()
	write(t, dir, "multiverse.yaml", "x")
	_, err := r.InitProject(context.Background(), app.ProjectInput{Dir: dir, Name: "acme", Repos: []string{"a/b"}, Local: true})
	if err == nil || !strings.Contains(err.Error(), "already defines a multiverse") {
		t.Fatalf("err = %v", err)
	}
}

func TestProjectInitInAScriptSaysWhatItNeeds(t *testing.T) {
	r := projectRig(t, &fakeRepos{inRepo: true})
	dir := filepath.Join(t.TempDir(), "multiverse")
	_, err := r.InitProject(context.Background(), app.ProjectInput{Dir: dir, Name: "acme"})
	if err == nil || !strings.Contains(err.Error(), "--repo") {
		t.Fatalf("no repositories: err = %v", err)
	}
	_, err = r.InitProject(context.Background(), app.ProjectInput{Dir: dir, Name: "acme", Repos: []string{"a/b"}})
	if err == nil || !strings.Contains(err.Error(), "--create") {
		t.Fatalf("no home for the repository: err = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "multiverse.yaml")); statErr == nil {
		t.Error("wrote files before it knew where they go")
	}
}

func TestProjectInitDeclinedWritesNothing(t *testing.T) {
	r := projectRig(t, &fakeRepos{inRepo: true})
	r.prompt.confirms = []bool{false}
	dir := filepath.Join(t.TempDir(), "multiverse")
	_, err := r.InitProject(context.Background(), app.ProjectInput{Dir: dir, Name: "acme", Repos: []string{"a/b"}, Create: "o/multiverse"})
	if err == nil || len(r.host.created) != 0 {
		t.Fatalf("err = %v, created %v: saying no must create nothing", err, r.host.created)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "multiverse.yaml")); statErr == nil {
		t.Error("wrote files after a no")
	}
}

func TestProjectInitNamesAndKeysSurviveAStrayRepository(t *testing.T) {
	git := &fakeRepos{inRepo: true}
	r := projectRig(t, git)
	r.Yes = true
	dir := filepath.Join(t.TempDir(), "multiverse")
	_, err := r.InitProject(context.Background(), app.ProjectInput{Dir: dir, Local: true, Repos: []string{
		"https://github.com/acme/acme-api", "https://github.com/acme/acme-ui", "https://github.com/acme/acme-admin",
		"https://github.com/acme/.skills",
	}})
	if err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Load(filepath.Join(dir, "multiverse.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "acme" || m.Repos["api"].URL == "" || m.Repos["ui"].URL == "" || m.Repos["skills"].Dir != ".skills" {
		t.Errorf("name %q, repos %+v: want acme, api, ui, and .skills kept as the directory of skills", m.Name, m.Repos)
	}
}

// A service that only needs the person to sign in is offered, and signs them in
// when chosen, instead of being greyed out with an instruction to go elsewhere.
func TestProjectInitGuideSignsInWhenTheServiceNeedsIt(t *testing.T) {
	git := &fakeRepos{inRepo: true}
	r := projectRig(t, git)
	r.host.signedOut = true
	r.host.accounts = []app.Account{{Name: "me"}}
	r.host.repos = []app.RemoteRepo{{Name: "me/api", URL: "https://github.com/me/api.git"}}
	r.prompt.selects = []int{0, 0, 2, 2} // search GitHub, account me, Done, then keep it local
	r.prompt.multi = []int{0}
	dir := filepath.Join(t.TempDir(), "multiverse")

	if _, err := r.InitProject(context.Background(), app.ProjectInput{Dir: dir, Name: "me"}); err != nil {
		t.Fatal(err)
	}
	if r.host.logins != 1 {
		t.Errorf("signed in %d times, want once", r.host.logins)
	}
	if _, err := manifest.Load(filepath.Join(dir, "multiverse.yaml")); err != nil {
		t.Error(err)
	}
}

func TestProjectInitKeepsAnAGENTSFileThatIsAlreadyThere(t *testing.T) {
	git := &fakeRepos{inRepo: true}
	r := projectRig(t, git)
	r.Yes = true
	dir := filepath.Join(t.TempDir(), "multiverse")
	write(t, dir, "AGENTS.md", "mine\n")
	if _, err := r.InitProject(context.Background(), app.ProjectInput{Dir: dir, Name: "acme", Local: true, Repos: []string{"acme/api"}}); err != nil {
		t.Fatal(err)
	}
	if got := read1(t, filepath.Join(dir, "AGENTS.md")); got != "mine\n" {
		t.Errorf("AGENTS.md was overwritten: %q", got)
	}
	if strings.Contains(git.commits[0], "AGENTS.md") {
		t.Errorf("a file that was not written must not be swept into the commit: %v", git.commits)
	}
}

// A name given as name=url is the manifest key as it stands, instead of the
// repository's name with the multiverse's prefix taken off.
func TestProjectInitRepoNameCanBeSetWithNameEqualsURL(t *testing.T) {
	git := &fakeRepos{inRepo: true}
	r := projectRig(t, git)
	r.Yes = true
	dir := filepath.Join(t.TempDir(), "multiverse")
	res, err := r.InitProject(context.Background(), app.ProjectInput{Dir: dir, Local: true, Name: "nnnn", Repos: []string{
		"git@gitlab.example.test:acme/nnnn-cli.git", "backend=git@gitlab.example.test:acme/nnnn-api.git",
	}})
	if err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Load(filepath.Join(dir, "multiverse.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Repos["cli"].URL == "" || m.Repos["backend"].URL == "" || m.Repos["api"].URL != "" {
		t.Errorf("repos %+v: want cli (derived) and backend (given)", m.Repos)
	}
	if len(res.Files) != 2 || len(res.Repos) != 2 {
		t.Errorf("result %+v: want the files and repositories reported", res)
	}
}

func TestProjectInitRefusesTwoRepositoriesWithTheSameGivenName(t *testing.T) {
	r := projectRig(t, &fakeRepos{inRepo: true})
	r.Yes = true
	_, err := r.InitProject(context.Background(), app.ProjectInput{Dir: filepath.Join(t.TempDir(), "m"), Local: true, Repos: []string{
		"x=https://github.com/acme/a", "x=https://github.com/acme/b",
	}})
	if err == nil || !strings.Contains(err.Error(), "name=url") {
		t.Errorf("err = %v, want a clash that points at name=url", err)
	}
}
