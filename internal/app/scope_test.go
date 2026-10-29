package app_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/app"
	"github.com/cristiansteib/multiversectl/internal/domain"
)

// partLoader answers a universe's reading of the repositories' own files, and
// remembers where it was told to look.
type partLoader struct {
	app.ManifestLoader
	asked [][]string
	with  func() *domain.Manifest
}

func (p *partLoader) LoadMerged(_ string, dirs func(string) []string) (*domain.Manifest, error) {
	p.asked = append(p.asked, dirs("api"))
	return p.with(), nil
}

// What a repository says about itself is read from the universe's own checkout
// first, so a change on a branch is seen by that universe and by no other.
func TestAUniverseReadsTheRepositorysOwnFileFromItsCheckout(t *testing.T) {
	r, _ := startRig(t, "{}")
	m := r.svc.Manifest
	scoped := &domain.Manifest{Name: m.Name, Path: m.Path, Dir: m.Dir, Repos: m.Repos, Proxy: m.Proxy,
		Apps: map[string]domain.App{"api": {Repo: "api", Hooks: map[domain.HookEvent][]domain.Hook{
			domain.PostUp: {{Host: true, Name: "from-the-branch", Command: []string{"true"}}},
		}}}}
	loader := &partLoader{with: func() *domain.Manifest { return scoped }}
	r.svc.Manifests = loader

	hooks, err := r.svc.Hooks("1")
	if err != nil {
		t.Fatal(err)
	}
	if len(hooks) != 1 || hooks[0].Name != "from-the-branch" {
		t.Errorf("hooks = %+v: want the one the branch's file declares", hooks)
	}
	if len(loader.asked) != 1 || len(loader.asked[0]) != 2 ||
		!strings.HasSuffix(loader.asked[0][0], "/universes/1/api") || !strings.HasSuffix(loader.asked[0][1], "/api") ||
		strings.Contains(loader.asked[0][1], "universes") {
		t.Errorf("looked in %v: want the worktree, then the clone", loader.asked)
	}
	if len(r.svc.Manifest.Apps["api"].Hooks) != 0 {
		t.Error("the service's own manifest must not be changed by one universe's file")
	}
}

func TestStartingWithAFileThatDoesNotLoadSaysWhichAndStartsNothing(t *testing.T) {
	r, rt := startRig(t, "{}")
	r.svc.Manifests = badLoader{}
	err := r.svc.Start(context.Background(), app.StartInput{Universe: "1"})
	if err == nil || !strings.Contains(err.Error(), "universe 1") || !strings.Contains(err.Error(), "bad part") || len(rt.ups) != 0 {
		t.Fatalf("err = %v, started %v", err, rt.ups)
	}
}

type badLoader struct{ app.ManifestLoader }

func (badLoader) LoadMerged(string, func(string) []string) (*domain.Manifest, error) {
	return nil, errBadPart
}

var errBadPart = errorString("repo api: .multiverse.yaml: bad part")

type errorString string

func (e errorString) Error() string { return string(e) }
