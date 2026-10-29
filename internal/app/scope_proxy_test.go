package app

import (
	"testing"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

type branchLoader struct {
	ManifestLoader
	m *domain.Manifest
}

func (b branchLoader) LoadMerged(string, func(string) []string) (*domain.Manifest, error) {
	return b.m, nil
}

// The proxy serves every universe, so a branch's file cannot change what it
// listens on: the next start of another universe would change it back.
func TestABranchCannotReconfigureTheSharedProxy(t *testing.T) {
	shared := domain.Proxy{Entrypoints: map[string]int{"web": 80}}
	m := &domain.Manifest{Name: "acme", Path: "/ws/multiverse.yaml", Proxy: shared}
	branch := &domain.Manifest{Name: "acme", Path: m.Path,
		Proxy: domain.Proxy{Entrypoints: map[string]int{"web": 80, "grpc": 50051}}}
	svc := New(Deps{Manifests: branchLoader{m: branch}, Out: discardOut{}}, "/ws", m, "test")

	scoped, err := svc.forUniverse("2")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := scoped.Manifest.Proxy.Entrypoints["grpc"]; ok {
		t.Errorf("proxy = %+v: want the shared clones' entrypoints", scoped.Manifest.Proxy)
	}
}
