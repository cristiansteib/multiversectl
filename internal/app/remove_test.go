package app

import (
	"context"
	"errors"
	"io/fs"
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
	pruned int
}

// countingFiles is everywhere{} with a record of what was removed, and the
// chance to refuse a path the way a filesystem does.
type countingFiles struct {
	Files
	removed map[string]bool
	refuse  map[string]error
}

func (f *countingFiles) Exists(string) bool { return true }

func (f *countingFiles) RemoveAll(path string) error {
	if err, ok := f.refuse[path]; ok {
		return err
	}
	if f.removed == nil {
		f.removed = map[string]bool{}
	}
	f.removed[path] = true
	return nil
}

func removeServiceWith(git *removeGit, files Files) (*Service, *pruneStore) {
	return removeServiceRunning(git, files, removeRuntime{})
}

func removeServiceRunning(git *removeGit, files Files, rt removeRuntime) (*Service, *pruneStore) {
	store := &pruneStore{states: []domain.UniverseState{
		{ID: "3", Name: "3", Slug: "u3", Project: "acme-u3", Branches: map[string]string{"api": "u3", "ui": "u3"}},
	}}
	m := &domain.Manifest{Name: "acme", Repos: map[string]domain.Repo{"api": {}, "ui": {}}}
	return New(Deps{Store: store, Runtime: rt, Git: git, Files: files, Out: discardOut{}}, "/ws", m, "test"), store
}

func (g *removeGit) IsClean(_ context.Context, dir string) (bool, error) {
	for suffix, d := range g.dirty {
		if strings.HasSuffix(dir, suffix) {
			return !d, nil
		}
	}
	return true, nil
}

func (g *removeGit) PruneWorktrees(context.Context, string) error { g.pruned++; return nil }

func (g *removeGit) removedPath(p string) bool {
	for _, got := range g.removed {
		if got == p {
			return true
		}
	}
	return false
}

func (g *removeGit) RemoveWorktree(_ context.Context, _, dest string, _ bool) error {
	if g.refuse != "" && strings.HasSuffix(dest, g.refuse) {
		return errors.New("worktree is locked")
	}
	g.removed = append(g.removed, dest)
	return nil
}

type removeRuntime struct {
	Runtime
	// asRoot is what the throwaway container does with a directory; without
	// one, it cannot run, as with Docker unreachable.
	asRoot func(dir string) error
}

func (r removeRuntime) RemoveAsRoot(_ context.Context, dir string) error {
	if r.asRoot == nil {
		return errors.New("docker is not reachable")
	}
	return r.asRoot(dir)
}

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

// A worktree whose registration the clone has lost — git calls it "not a
// working tree" — is still a directory the universe owns, and removing the
// universe is exactly the moment to take it away.
func TestADirectoryGitHasNoRecordOfIsStillRemoved(t *testing.T) {
	git := &removeGit{refuse: "/ui"} // git says no for ui
	files := &countingFiles{}
	s, store := removeServiceWith(git, files)

	if err := s.Remove(context.Background(), RemoveInput{Universe: "3"}); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if !files.removed["/ws/universes/3/ui"] {
		t.Errorf("want the directory taken away anyway, removed %v", files.removed)
	}
	if len(store.deleted) != 1 {
		t.Errorf("want the universe gone from the listing, got %v", store.deleted)
	}
}

// What a container running as root leaves behind: files the person who
// started it cannot delete. The removal does everything else, keeps the state
// so `rm` can be pointed at it again, and says exactly what to run.
func TestWhatCannotBeDeletedIsNamedWithTheCommandToDeleteIt(t *testing.T) {
	git := &removeGit{refuse: "/ui"}
	files := &countingFiles{refuse: map[string]error{
		"/ws/universes/3/ui": &fs.PathError{Op: "unlinkat", Path: "/ws/universes/3/ui/node_modules", Err: fs.ErrPermission},
	}}
	s, store := removeServiceWith(git, files)

	err := s.Remove(context.Background(), RemoveInput{Universe: "3"})
	var left *NotFullyRemoved
	if !errors.As(err, &left) {
		t.Fatalf("want it reported as unfinished, got %v", err)
	}
	for _, want := range []string{
		"containers and volumes are gone", // what did happen
		"/ws/universes/3/ui",              // what did not
		"sudo rm -rf /ws/universes/3/ui",  // and how to finish it
		"universe rm 3` again",            // and then what
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("want %q in:\n%s", want, err)
		}
	}
	// The other repository was still removed: one stuck directory does not
	// leave the rest of a destroyed universe lying about.
	if !git.removedPath("/ws/universes/3/api") {
		t.Errorf("want api removed too, got %v", git.removed)
	}
	// And the universe is still listed, so running rm again finishes it.
	if len(store.deleted) != 0 {
		t.Errorf("the state was deleted with files still on disk: %v", store.deleted)
	}
}

// The files a container wrote as root are removed by a container, which is root
// too, so nobody is told to use sudo and the universe is gone in one command.
func TestFilesOnlyRootCanDeleteAreRemovedWithAContainer(t *testing.T) {
	git := &removeGit{refuse: "/ui"}
	files := &countingFiles{refuse: map[string]error{
		"/ws/universes/3/ui": &fs.PathError{Op: "unlinkat", Path: "/ws/universes/3/ui/node_modules", Err: fs.ErrPermission},
	}}
	var asked []string
	s, store := removeServiceRunning(git, files, removeRuntime{asRoot: func(dir string) error { asked = append(asked, dir); return nil }})

	if err := s.Remove(context.Background(), RemoveInput{Universe: "3"}); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(asked) != 1 || asked[0] != "/ws/universes/3/ui" {
		t.Errorf("the container was asked to remove %v", asked)
	}
	if len(store.deleted) != 1 {
		t.Errorf("the universe is still listed: %v", store.deleted)
	}
}

// A container for something that is not a permission problem would hide a real
// failure behind a second try.
func TestAnotherFailureIsNotHandedToAContainer(t *testing.T) {
	git := &removeGit{refuse: "/ui"}
	files := &countingFiles{refuse: map[string]error{"/ws/universes/3/ui": errors.New("device or resource busy")}}
	asked := false
	s, _ := removeServiceRunning(git, files, removeRuntime{asRoot: func(string) error { asked = true; return nil }})
	var left *NotFullyRemoved
	if err := s.Remove(context.Background(), RemoveInput{Universe: "3"}); !errors.As(err, &left) || asked {
		t.Errorf("err %v, container asked %v", err, asked)
	}
}

// Having removed a directory git did not know about, the clone should be told,
// or the branch that was in it stays unavailable to the next universe.
func TestTheCloneIsToldToForgetWhatIsGone(t *testing.T) {
	git := &removeGit{}
	s, _ := removeServiceWith(git, &countingFiles{})
	if err := s.Remove(context.Background(), RemoveInput{Universe: "3"}); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if git.pruned == 0 {
		t.Error("want the clone's worktree list pruned")
	}
}
