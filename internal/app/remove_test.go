package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

type removeGit struct {
	Git
	dirty   map[string]bool // by worktree directory suffix
	removed []string
	// refuse is returned by RemoveWorktree for a destination ending in it.
	refuse string
}

func (g *removeGit) IsClean(_ context.Context, dir string) (bool, error) {
	for suffix, d := range g.dirty {
		if strings.HasSuffix(dir, suffix) {
			return !d, nil
		}
	}
	return true, nil
}

func (g *removeGit) RemoveWorktree(_ context.Context, _, dest string, _ bool) error {
	if g.refuse != "" && strings.HasSuffix(dest, g.refuse) {
		return errors.New("worktree is locked")
	}
	g.removed = append(g.removed, dest)
	return nil
}

type removeRuntime struct{ Runtime }

func (removeRuntime) Down(context.Context, RuntimeRequest, bool) error { return nil }

func removeService(git *removeGit) (*Service, *pruneStore) {
	store := &pruneStore{states: []domain.UniverseState{
		{ID: "3", Name: "3", Slug: "u3", Project: "acme-u3", Branches: map[string]string{"api": "u3", "ui": "u3"}},
	}}
	m := &domain.Manifest{Name: "acme", Repos: map[string]domain.Repo{"api": {}, "ui": {}}}
	return New(Deps{Store: store, Runtime: removeRuntime{}, Git: git, Files: everywhere{}, Out: discardOut{}}, "/ws", m, "test"), store
}

// Typing a universe's name back says "yes, this universe". It does not say
// "and the afternoon's work in it", which is a question of its own.
func TestRemovingAUniverseWithWorkOnlyInItIsRefused(t *testing.T) {
	git := &removeGit{dirty: map[string]bool{"/ui": true}}
	s, store := removeService(git)

	err := s.Remove(context.Background(), RemoveInput{Universe: "3"})
	var unsaved *UnsavedWork
	if !errors.As(err, &unsaved) {
		t.Fatalf("want it refused, got %v", err)
	}
	if len(unsaved.Repos) != 1 || unsaved.Repos[0] != "ui" {
		t.Errorf("want the repository named, got %v", unsaved.Repos)
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("want the way past it, got %q", err)
	}
	// And nothing of the universe is gone: a refusal after half the work is
	// worse than either answer.
	if len(git.removed) != 0 || len(store.deleted) != 0 {
		t.Errorf("it took something anyway: worktrees %v, state %v", git.removed, store.deleted)
	}
}

func TestForceRemovesItAnyway(t *testing.T) {
	git := &removeGit{dirty: map[string]bool{"/ui": true}}
	s, store := removeService(git)

	if err := s.Remove(context.Background(), RemoveInput{Universe: "3", Force: true}); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(git.removed) != 2 {
		t.Errorf("want both worktrees removed, got %v", git.removed)
	}
	if len(store.deleted) != 1 || store.deleted[0] != "3" {
		t.Errorf("want the state file gone, got %v", store.deleted)
	}
}

func TestAUniverseWithNothingUnsavedNeedsNoForce(t *testing.T) {
	git := &removeGit{}
	s, _ := removeService(git)
	if err := s.Remove(context.Background(), RemoveInput{Universe: "3"}); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(git.removed) != 2 {
		t.Errorf("want both worktrees removed, got %v", git.removed)
	}
}

func TestUnsavedNamesOnlyTheRepositoriesThatHaveWork(t *testing.T) {
	s, _ := removeService(&removeGit{dirty: map[string]bool{"/api": true}})
	dirty, err := s.Unsaved(context.Background(), "3")
	if err != nil {
		t.Fatalf("Unsaved: %v", err)
	}
	if len(dirty) != 1 || dirty[0] != "api" {
		t.Errorf("want api alone, got %v", dirty)
	}
}

// The usual reason Create fails is Ctrl-C, and a cleanup on the context that
// was just cancelled does nothing at all — silently, leaving the half-made
// universe on disk.
func TestTakingBackAHalfMadeUniverseSurvivesTheCancellationThatCausedIt(t *testing.T) {
	git := &removeGit{}
	s, _ := removeService(git)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cause := errors.New("the second repository failed")
	err := s.rollback(ctx, []repoPlan{{key: "api", clone: "/ws/api", dest: "/ws/universes/3/api"}},
		s.Universe("3"), false, cause)

	if !errors.Is(err, cause) {
		t.Errorf("want the cause kept, got %v", err)
	}
	if len(git.removed) != 1 {
		t.Fatalf("want the worktree taken back, got %v", git.removed)
	}
}

// What it cannot take back has to be said: a worktree left behind is a
// directory somebody has to deal with.
func TestWhatCouldNotBeTakenBackIsNamed(t *testing.T) {
	git := &removeGit{refuse: "/api"}
	s, _ := removeService(git)
	cause := errors.New("the second repository failed")

	err := s.rollback(context.Background(), []repoPlan{{key: "api", clone: "/ws/api", dest: "/ws/universes/3/api"}},
		s.Universe("3"), false, cause)

	if !errors.Is(err, cause) {
		t.Errorf("want the cause kept, got %v", err)
	}
	for _, want := range []string{"still there", "/ws/universes/3/api", "worktree is locked"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("want %q in %q", want, err)
		}
	}
}
