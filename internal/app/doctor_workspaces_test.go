package app

import (
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

type fakeLoader map[string]*domain.Manifest

func (f fakeLoader) Load(path string) (*domain.Manifest, error) {
	if m, ok := f[path]; ok {
		return m, nil
	}
	return nil, domain.ErrNoManifest
}

type fakeConfig struct{ c Config }

func (f fakeConfig) Load() (Config, error) { return f.c, nil }
func (fakeConfig) Save(Config) error       { return nil }
func (fakeConfig) Path() string            { return "" }

func TestDoctorWarnsAboutAnotherWorkspaceOnTheSamePort(t *testing.T) {
	mine := &domain.Manifest{Name: "acme", Path: "/a/multiverse.yaml", Proxy: domain.Proxy{Entrypoints: map[string]int{"web": 80}}}
	other := &domain.Manifest{Name: "beta", Path: "/b/multiverse.yaml", Proxy: domain.Proxy{Entrypoints: map[string]int{"web": 80}}}
	quiet := &domain.Manifest{Name: "gamma", Path: "/c/multiverse.yaml", Proxy: domain.Proxy{Entrypoints: map[string]int{"web": 8080}}}
	s := &Service{Deps: Deps{
		Config: fakeConfig{Config{Workspaces: map[string]WorkspaceEntry{
			"acme": {Manifest: mine.Path}, "beta": {Manifest: other.Path}, "gamma": {Manifest: quiet.Path}, "broken": {Manifest: "/nope"},
		}}},
		Manifests: fakeLoader{other.Path: other, quiet.Path: quiet},
	}, Manifest: mine}

	var got []Check
	s.checkWorkspaces(func(name string, st Status, f string, a ...any) {
		got = append(got, Check{Name: name, Status: st, Detail: f})
	})
	if len(got) != 1 || got[0].Status != StatusWarn || !strings.Contains(got[0].Detail, "%s") {
		t.Fatalf("want one warning about the shared port, got %+v", got)
	}
}

func TestDoctorSaysSoWhenWorkspacesDoNotClash(t *testing.T) {
	mine := &domain.Manifest{Name: "acme", Path: "/a/multiverse.yaml", Proxy: domain.Proxy{Entrypoints: map[string]int{"web": 80}}}
	other := &domain.Manifest{Name: "beta", Path: "/b/multiverse.yaml", Proxy: domain.Proxy{Entrypoints: map[string]int{"web": 8080}}}
	s := &Service{Deps: Deps{
		Config:    fakeConfig{Config{Workspaces: map[string]WorkspaceEntry{"beta": {Manifest: other.Path}}}},
		Manifests: fakeLoader{other.Path: other},
	}, Manifest: mine}
	var got []Status
	s.checkWorkspaces(func(_ string, st Status, _ string, _ ...any) { got = append(got, st) })
	if len(got) != 1 || got[0] != StatusOK {
		t.Fatalf("got %v", got)
	}
	// A machine with a single workspace has nothing to compare, so says nothing.
	s.Config = fakeConfig{}
	got = nil
	s.checkWorkspaces(func(_ string, st Status, _ string, _ ...any) { got = append(got, st) })
	if len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}
