package server

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/adapters/fsx"
	"github.com/cristiansteib/multiversectl/internal/app"
	"github.com/cristiansteib/multiversectl/internal/domain"
)

// lockGit watches, from inside the fetch, whether the catalog lock was held.
// A fetch in the manifest clone is what Status does under that lock, and two
// git processes in one clone contend.
type lockGit struct {
	app.Git
	mu      *sync.Mutex
	fetches int
	loose   int // fetches that ran with the lock free
}

func (g *lockGit) Fetch(context.Context, string) error {
	g.fetches++
	if g.mu.TryLock() {
		g.mu.Unlock()
		g.loose++
	}
	return nil
}

func (g *lockGit) IsClean(context.Context, string) (bool, error)       { return true, nil }
func (g *lockGit) Behind(context.Context, string, string) (int, error) { return 0, nil }

type memConfig struct{ c app.Config }

func (m *memConfig) Load() (app.Config, error) { return m.c, nil }
func (m *memConfig) Save(c app.Config) error   { m.c = c; return nil }
func (m *memConfig) Path() string              { return "/cfg/config.yaml" }

type quietSink struct{}

func (quietSink) Info(string, ...any)        {}
func (quietSink) OK(string, ...any)          {}
func (quietSink) Warn(string, ...any)        {}
func (quietSink) Plain(string, ...any)       {}
func (quietSink) Table([]string, [][]string) {}
func (quietSink) Stdout() io.Writer          { return io.Discard }
func (quietSink) Stderr() io.Writer          { return io.Discard }

func TestUpdateFetchesTheCatalogCloneUnderTheSameLockAsStatus(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".multiverse", "workspace"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := &memConfig{c: app.Config{Current: "proj", Workspaces: map[string]app.WorkspaceEntry{
		"proj": {Root: root, Manifest: filepath.Join(root, "multiverse.yaml"),
			Source: app.Origin{Repo: "git@example.test:me/proj.git"}},
	}}}
	git := &lockGit{}
	files := fsx.New()
	cats := app.NewCatalogs(app.CatalogDeps{Git: git, Files: files, Config: cfg, Out: quietSink{}})
	machine := app.New(app.Deps{Git: git, Files: files, Config: cfg, Out: quietSink{}}, "", nil, "test")
	svcs := NewServices(cats, machine, func(out app.Output, _, _ io.Writer, root, _ string) (*app.Service, error) {
		return app.New(app.Deps{Git: git, Files: files, Config: cfg, Out: out}, root,
			&domain.Manifest{Name: "proj"}, "test"), nil
	})
	git.mu = svcs.mu

	if err := svcs.Do(context.Background(), opRequest{Action: "update", Workspace: "proj"}, quietSink{}); err != nil {
		t.Fatal(err)
	}
	if git.fetches != 1 {
		t.Fatalf("fetches = %d, want the one update makes", git.fetches)
	}
	if git.loose != 0 {
		t.Errorf("update fetched the manifest clone with the lock free; Status fetches the same clone under it")
	}
}
