package app

import (
	"context"
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

type pruneStore struct {
	Store
	states  []domain.UniverseState
	deleted []string
}

func (p *pruneStore) List() ([]domain.UniverseState, error) { return p.states, nil }
func (p *pruneStore) Load(id string) (domain.UniverseState, error) {
	for _, st := range p.states {
		if st.ID == id {
			return st, nil
		}
	}
	return domain.UniverseState{}, domain.ErrNotFound
}
func (p *pruneStore) Delete(id string) error               { p.deleted = append(p.deleted, id); return nil }
func (p *pruneStore) SaveConfig(string, []byte) error      { return nil }
func (p *pruneStore) LoadConfig(string) ([]byte, error)    { return nil, domain.ErrNotFound }
func (p *pruneStore) Notes(string) ([]domain.Note, error)  { return nil, nil }
func (p *pruneStore) CreateNote(string, domain.Note) error { return nil }
func (p *pruneStore) SaveNote(string, domain.Note) error   { return nil }

type pruneRuntime struct {
	Runtime
	running map[string]int
}

func (p pruneRuntime) Containers(_ context.Context, project string) ([]Container, error) {
	out := make([]Container, p.running[project])
	for i := range out {
		out[i] = Container{Status: "Up 2 minutes"}
	}
	return out, nil
}

type pruneGit struct {
	Git
	dirty   map[string]bool // by worktree directory suffix
	removed []string
}

func (g *pruneGit) IsClean(_ context.Context, dir string) (bool, error) {
	for suffix, d := range g.dirty {
		if strings.HasSuffix(dir, suffix) {
			return !d, nil
		}
	}
	return true, nil
}
func (g *pruneGit) PruneWorktrees(context.Context, string) error { return nil }

func (g *pruneGit) RemoveWorktree(_ context.Context, _, dest string, _ bool) error {
	g.removed = append(g.removed, dest)
	return nil
}

type everywhere struct{ Files }

func (everywhere) Exists(string) bool     { return true }
func (everywhere) RemoveAll(string) error { return nil }

func pruneService(store *pruneStore, rt pruneRuntime, git *pruneGit) *Service {
	m := &domain.Manifest{Name: "acme", Repos: map[string]domain.Repo{"api": {URL: "git@example.test:acme/api.git"}}}
	return New(Deps{Store: store, Runtime: rt, Git: git, Files: everywhere{}, Out: discardOut{}}, "/ws", m, "test")
}

func TestPruneKeepsWhatIsRunningOrUnsaved(t *testing.T) {
	store := &pruneStore{states: []domain.UniverseState{
		{ID: "alpha", Name: "alpha", Project: "acme-alpha", Branches: map[string]string{"api": "alpha"}},
		{ID: "beta", Name: "beta", Project: "acme-beta", Branches: map[string]string{"api": "beta"}},
		{ID: "gamma", Name: "gamma", Project: "acme-gamma", Branches: map[string]string{"api": "gamma"}},
	}}
	rt := pruneRuntime{running: map[string]int{"acme-beta": 2}}
	git := &pruneGit{dirty: map[string]bool{"gamma/api": true}}
	s := pruneService(store, rt, git)

	skips := func(o PruneOptions) map[string]string {
		items, err := s.PrunePlan(context.Background(), o)
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]string{}
		for _, it := range items {
			m[it.Name] = it.Skip
		}
		return m
	}

	if got := skips(PruneOptions{}); got["alpha"] != "" || !strings.Contains(got["beta"], "running") || !strings.Contains(got["gamma"], "uncommitted work in api") {
		t.Errorf("default plan = %v", got)
	}
	if got := skips(PruneOptions{All: true}); got["beta"] != "" || got["gamma"] == "" {
		t.Errorf("--all must include running, not uncommitted work: %v", got)
	}
	if got := skips(PruneOptions{All: true, Force: true}); got["alpha"]+got["beta"]+got["gamma"] != "" {
		t.Errorf("--all --force keeps nothing: %v", got)
	}

	items, _ := s.PrunePlan(context.Background(), PruneOptions{})
	n, err := s.Prune(context.Background(), items)
	if err != nil || n != 1 {
		t.Fatalf("removed %d, %v; want only alpha", n, err)
	}
	if len(store.deleted) != 1 || store.deleted[0] != "alpha" || len(git.removed) != 1 {
		t.Errorf("deleted %v, worktrees %v", store.deleted, git.removed)
	}
}

type discardOut struct{}

func (discardOut) Info(string, ...any)        {}
func (discardOut) OK(string, ...any)          {}
func (discardOut) Warn(string, ...any)        {}
func (discardOut) Plain(string, ...any)       {}
func (discardOut) Table([]string, [][]string) {}
