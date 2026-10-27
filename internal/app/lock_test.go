package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

type fakeLocks struct {
	taken []string // the lock names, in the order they were asked for
	busy  string   // a name that is reported as held by somebody else
	held  map[string]bool
}

func (f *fakeLocks) Acquire(_ context.Context, path string, _ time.Duration) (Unlock, error) {
	name := strings.TrimSuffix(filepath.Base(path), ".lock")
	f.taken = append(f.taken, name)
	if name == f.busy {
		return nil, &Busy{Holder: "pid 42: multiversectl universe up 3"}
	}
	if f.held == nil {
		f.held = map[string]bool{}
	}
	if f.held[name] {
		return nil, errors.New("taken twice by the same process: " + name)
	}
	f.held[name] = true
	return func() { f.held[name] = false }, nil
}

func lockedService(locks *fakeLocks) *Service {
	store := &pruneStore{states: []domain.UniverseState{
		{ID: "3", Name: "3", Slug: "u3", Project: "acme-u3", Branches: map[string]string{"api": "u3"}},
	}}
	m := &domain.Manifest{Name: "acme", Repos: map[string]domain.Repo{"api": {}}}
	return New(Deps{Store: store, Runtime: removeRuntime{}, Git: &removeGit{}, Files: everywhere{},
		Out: discardOut{}, Locks: locks}, "/ws", m, "test")
}

// The command line and the web view are different programs: what keeps them
// off each other is a lock in the project, and being told who has it.
func TestWorkingOnAUniverseAnotherProcessHasIsRefused(t *testing.T) {
	s := lockedService(&fakeLocks{busy: "universe-3"})
	err := s.Stop(context.Background(), StopInput{Universe: "3"})
	var busy *Busy
	if !errors.As(err, &busy) {
		t.Fatalf("want it refused, got %v", err)
	}
	if !strings.Contains(err.Error(), "universe 3") || !strings.Contains(err.Error(), "pid 42") {
		t.Errorf("want what is busy and who has it, got %q", err)
	}
}

func TestRemovingTakesTheProjectThenTheUniverse(t *testing.T) {
	locks := &fakeLocks{}
	s := lockedService(locks)
	if err := s.Remove(context.Background(), RemoveInput{Universe: "3"}); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	want := []string{"multiverse", "universe-3"}
	if len(locks.taken) != len(want) {
		t.Fatalf("want %v, got %v", want, locks.taken)
	}
	for i, name := range want {
		if locks.taken[i] != name {
			// Order matters: two callers taking them the other way round would
			// each hold what the other waits for.
			t.Errorf("lock %d = %s, want %s (always multiverse first)", i, locks.taken[i], name)
		}
	}
}

// Prune removes one universe after another, and Remove takes the locks for
// each. If Prune held the project lock too, it would be this same process
// waiting for a lock it already has — a wait with no end.
func TestPruningDoesNotWaitForItself(t *testing.T) {
	locks := &fakeLocks{}
	s := lockedService(locks)
	done := make(chan error, 1)
	go func() {
		_, err := s.Prune(context.Background(), []PruneItem{{Name: "3"}})
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Prune: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Prune is waiting for a lock it holds itself")
	}
}

func TestWithoutALockPortNothingLocks(t *testing.T) {
	store := &pruneStore{states: []domain.UniverseState{{ID: "3", Name: "3", Project: "acme-u3"}}}
	m := &domain.Manifest{Name: "acme", Repos: map[string]domain.Repo{"api": {}}}
	s := New(Deps{Store: store, Runtime: removeRuntime{}, Git: &removeGit{}, Files: everywhere{},
		Out: discardOut{}}, "/ws", m, "test")
	if err := s.Remove(context.Background(), RemoveInput{Universe: "3"}); err != nil {
		t.Fatalf("want it to run unlocked, got %v", err)
	}
}
