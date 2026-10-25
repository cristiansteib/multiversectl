package app_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/adapters/catalog"
	"github.com/cristiansteib/multiversectl/internal/app"
	"github.com/cristiansteib/multiversectl/internal/domain"
)

// An index of mine that links acme's, which is where a project of acme's goes.
func placementRig(t *testing.T) (*rig, *fakeRepos) {
	t.Helper()
	e := catalog.Editor{}
	acme := t.TempDir()
	write(t, acme, "catalog.yaml", string(e.Scaffold("acme")))
	billing := manifestRepo(t, "billing")
	git := &fakeRepos{repos: map[string]string{
		"https://github.com/acme/index.git":   acme,
		"https://github.com/acme/billing.git": billing,
	}}
	r := newRig(t, git)
	data, err := e.AddCatalogRef(e.Scaffold("me"), domain.CatalogRef{Name: "acme", Source: domain.Source{Repo: "https://github.com/acme/index.git"}})
	if err != nil {
		t.Fatal(err)
	}
	data, _ = e.AddCatalogRef(data, domain.CatalogRef{Name: "secret", Source: domain.Source{Repo: "https://github.com/acme/private.git"}})
	write(t, r.root, "catalog.yaml", string(data))
	return r, git
}

func TestThePickerDefaultsToTheCatalogOwnedBySameOwnerAsTheSource(t *testing.T) {
	r, git := placementRig(t)
	r.prompt.takeDefault = true
	node, err := r.Add(context.Background(), app.AddInput{Repo: "acme/billing"})
	if err != nil {
		t.Fatal(err)
	}
	if node != "acme/billing" {
		t.Errorf("node = %q; acme/billing belongs in acme's catalog, not the root", node)
	}
	if len(git.pushed) != 1 || git.pushed[0] != "main" {
		t.Errorf("pushed %v: the entry is written to acme's repository", git.pushed)
	}
	// What the picker showed: where each choice writes to, and what it cannot offer.
	var notes, disabled []string
	for _, o := range r.prompt.lastOptions {
		notes = append(notes, o.Note)
		if o.Disabled {
			disabled = append(disabled, o.Label+" "+o.Note)
		}
	}
	if !strings.Contains(strings.Join(notes, "|"), "writes to acme/index · another repository") {
		t.Errorf("notes %v should say where writing goes, and that it is another repository", notes)
	}
	if len(disabled) != 1 || !strings.Contains(disabled[0], "secret") || !strings.Contains(disabled[0], "no access or not found: https://github.com/acme/private.git") {
		t.Errorf("an unreachable layer is listed, not hidden: %v", disabled)
	}
}

func TestYesPutsAnEntryInTheRootNotInSomeoneElsesRepository(t *testing.T) {
	r, git := placementRig(t)
	r.Yes = true
	node, err := r.Add(context.Background(), app.AddInput{Repo: "acme/billing"})
	if err != nil || node != "billing" {
		t.Fatalf("node=%q err=%v", node, err)
	}
	if len(git.pushed) != 0 {
		t.Errorf("a script must not push to a team's repository nobody named: %v", git.pushed)
	}
}

func TestANameThatIsTakenIsAskedAboutOnlyThen(t *testing.T) {
	r, _ := placementRig(t)
	r.Yes = false
	e := catalog.Editor{}
	data, _ := os.ReadFile(filepath.Join(r.root, "catalog.yaml"))
	out, _ := e.AddWorkspaceRef(data, domain.WorkspaceRef{Name: "billing", Source: domain.Source{Repo: "git@example.test:other/billing-v1.git"}})
	write(t, r.root, "catalog.yaml", string(out))

	r.prompt.selects = []int{0}
	r.prompt.inputs = nil // accept the suggestion
	node, err := r.Add(context.Background(), app.AddInput{Repo: "acme/billing", At: "/", AtGiven: true})
	if err != nil || node != "billing-2" {
		t.Fatalf("node=%q err=%v", node, err)
	}
	asked := strings.Join(r.prompt.asked, "|")
	if !strings.Contains(asked, `"billing" exists here (git@example.test:other/billing-v1). Name`) {
		t.Errorf("asked %q", asked)
	}
	if strings.Contains(asked, "Name in the catalog") {
		t.Errorf("the name is no longer asked for up front: %q", asked)
	}

	if _, err := r.Add(context.Background(), app.AddInput{Repo: "acme/billing", Name: "billing", At: "/", AtGiven: true}); err == nil || !strings.Contains(err.Error(), "pick another --name") {
		t.Errorf("an explicit --name that collides is an error: %v", err)
	}
}

func TestListingASourceTwiceAsksFirst(t *testing.T) {
	r, _ := placementRig(t)
	r.Yes = true
	if _, err := r.Add(context.Background(), app.AddInput{Repo: "acme/billing", At: "/", AtGiven: true}); err != nil {
		t.Fatal(err)
	}
	e := catalog.Editor{}
	data, _ := os.ReadFile(filepath.Join(r.root, "catalog.yaml"))
	out, _ := e.AddCatalogRef(data, domain.CatalogRef{Name: "other", Source: domain.Source{Path: "other.yaml"}})
	write(t, r.root, "catalog.yaml", string(out))
	write(t, r.root, "other.yaml", string(e.Scaffold("other")))

	// --yes adds it, and says so.
	if _, err := r.Add(context.Background(), app.AddInput{Repo: "acme/billing", Name: "again", At: "other", AtGiven: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(r.out.warns, "|"), "already listed at billing") {
		t.Errorf("warns = %v", r.out.warns)
	}
	// Interactively the default is no.
	r.Yes = false
	r.prompt.confirms = []bool{false}
	if _, err := r.Add(context.Background(), app.AddInput{Repo: "acme/billing", Name: "third", At: "other", AtGiven: true}); !errors.Is(err, app.ErrCancelled) {
		t.Errorf("err = %v", err)
	}
}

func TestWritesRefreshOnlyTheRepositoryBeingWrittenTo(t *testing.T) {
	r, git := placementRig(t)
	r.Yes = true
	if _, err := r.Tree(context.Background(), "", true); err != nil { // warm the cache
		t.Fatal(err)
	}
	git.fetched = nil
	if _, err := r.Add(context.Background(), app.AddInput{Repo: "acme/billing", At: "acme", AtGiven: true}); err != nil {
		t.Fatal(err)
	}
	// The source, and acme's index (to write to it): not every layer of the tree.
	if len(git.fetched) > 2 {
		t.Errorf("fetched %d repositories for one write: %v", len(git.fetched), git.fetched)
	}
}

func TestRemovingAnEntryTellsYouWhatIsStillInstalled(t *testing.T) {
	r := newRig(t, &fakeRepos{})
	r.Yes = true
	if _, err := r.CreateCatalog(context.Background(), app.CreateCatalogInput{Name: "personal", Same: true}); err != nil {
		t.Fatal(err)
	}
	r.cfg.c.Workspaces = map[string]app.WorkspaceEntry{"fish": {From: "personal/fish"}}
	if err := r.Remove(context.Background(), "personal"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(r.out.infos, "|"), "still installed here: multiversectl catalog uninstall fish") {
		t.Errorf("infos = %v", r.out.infos)
	}
}

func TestInitTakesAPathOrARepositoryAsItsSource(t *testing.T) {
	existing := t.TempDir()
	write(t, existing, "catalog.yaml", string(catalog.Editor{}.Scaffold("mine")))
	r := newRig(t, &fakeRepos{})
	r.cfg.c = app.Config{}
	r.Yes = true
	if err := r.Init(context.Background(), app.InitInput{Source: existing}); err != nil {
		t.Fatal(err)
	}
	if r.cfg.c.Catalog != filepath.Join(existing, "catalog.yaml") {
		t.Errorf("root = %q", r.cfg.c.Catalog)
	}

	bad := t.TempDir()
	write(t, bad, "catalog.yaml", "not: a catalog\n")
	if err := r.Init(context.Background(), app.InitInput{Source: bad}); err == nil {
		t.Error("a source that is not a catalog is refused, not recorded")
	}

	r2 := newRig(t, &fakeRepos{})
	if err := r2.Init(context.Background(), app.InitInput{}); err == nil || !strings.Contains(err.Error(), "give a source to replace it") {
		t.Errorf("without a terminal and with a root already set, the error says how to replace it: %v", err)
	}
}
