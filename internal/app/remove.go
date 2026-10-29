package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// RemoveInput names the universe to delete. Confirming with the person is the
// caller's job; whether work may be thrown away is not.
type RemoveInput struct {
	Universe string
	// Force removes a universe whose worktrees hold work that is only there.
	// Without it such a universe is left alone and named, because a question
	// answered about a universe is not an answer about the commits in it:
	// somebody typing a name back is saying "yes, this universe", not "yes,
	// and the afternoon's work in it too".
	Force bool
}

// UnsavedWork is the repositories of a universe whose worktree holds changes
// that are committed nowhere. Removing the universe would be the end of them.
type UnsavedWork struct {
	Universe string
	Repos    []string
}

func (e *UnsavedWork) Error() string {
	return "uncommitted work in " + strings.Join(e.Repos, ", ") +
		": removing universe " + e.Universe + " would discard it (--force removes it anyway)"
}

// Unsaved names the repositories of a universe with uncommitted work in them.
// A worktree that cannot be read is left out rather than guessed at: this
// answers "what would be lost", and it may not invent a loss.
func (s *Service) Unsaved(ctx context.Context, name string) ([]string, error) {
	st, err := s.Resolve(name)
	if err != nil {
		return nil, err
	}
	u := s.Universe(st.Name)
	var dirty []string
	for _, repoKey := range sortedRepoKeys(s.Manifest.Repos) {
		dest := u.RepoDir(repoKey)
		if !s.Files.Exists(dest) {
			continue
		}
		if clean, err := s.Git.IsClean(ctx, dest); err == nil && !clean {
			dirty = append(dirty, repoKey)
		}
	}
	return dirty, nil
}

// Remove destroys a universe: its containers and volumes, its worktrees, its
// directory and its state. A container that will not go down is reported and
// does not stop the rest, so a half-broken universe can still be cleaned up.
func (s *Service) Remove(ctx context.Context, in RemoveInput) error {
	unlock, err := s.lockProject(ctx)
	if err != nil {
		return err
	}
	defer unlock()

	// Resolved after the lock, not before: another process may have removed
	// this universe while this one was waiting, and the answer to that is to
	// say so rather than to go looking for its containers.
	st, err := s.Resolve(in.Universe)
	if err != nil {
		// Resolve names the universe it could not find, and returns a zero
		// state, so wrapping with st.Name would prefix an empty one.
		return err
	}
	// And the universe's own, so this cannot run while something is starting
	// or stopping it. Always after the multiverse lock, never before.
	unlockUniverse, err := s.lockUniverse(ctx, st.ID, st.Name)
	if err != nil {
		return err
	}
	defer unlockUniverse()
	u := s.Universe(st.Name)

	// Asked before anything is destroyed, not repository by repository: half a
	// universe removed and then a refusal is worse than either answer.
	if !in.Force {
		dirty, err := s.Unsaved(ctx, st.Name)
		if err != nil {
			return err
		}
		if len(dirty) > 0 {
			return &UnsavedWork{Universe: st.Name, Repos: dirty}
		}
	}

	// Before anything is destroyed, while the containers a command may need are
	// still there.
	for _, appKey := range append(s.withoutIdleOnDemand(ctx, st, s.Manifest.RunnableApps()), multiverseHooks) {
		if st, err = s.fire(ctx, domain.PreRemove, st, appKey, nil, nil); err != nil {
			return err
		}
	}

	for _, appKey := range s.Manifest.RunnableApps() {
		if err := s.Runtime.Down(ctx, s.request(u, st, appKey, nil), true); err != nil {
			s.Out.Warn("%s: down: %v", appKey, err)
		}
	}

	// Every repository is attempted, whatever the one before it did. By this
	// point the containers and the volumes are already gone, so stopping at
	// the first worktree that will not go would leave the universe destroyed
	// in every way that matters and still listed as if it were there.
	var stuck []Leftover
	for _, repoKey := range sortedRepoKeys(s.Manifest.Repos) {
		dest := u.RepoDir(repoKey)
		if !s.Files.Exists(dest) {
			continue
		}
		clone := s.Manifest.CloneDir(s.Multiverse, repoKey)
		// Whatever was in them has been accounted for above, by refusing or by
		// being forced, so this is where the work is actually discarded.
		err := s.Git.RemoveWorktree(ctx, clone, dest, true)
		if err != nil && s.Files.Exists(dest) {
			// git would not, or could not. It refuses a directory it has no
			// record of, which is what a worktree whose registration was lost
			// looks like — and the universe is being destroyed either way, so
			// the directory goes.
			if rmErr := s.removeDir(ctx, dest); rmErr != nil {
				stuck = append(stuck, Leftover{Repo: repoKey, Path: dest, Err: rmErr})
				continue
			}
			err = nil
		}
		if err != nil {
			stuck = append(stuck, Leftover{Repo: repoKey, Path: dest, Err: err})
			continue
		}
		// However it went, git may still have it written down.
		if err := s.Git.PruneWorktrees(ctx, clone); err != nil {
			s.Out.Warn("%s: %v", repoKey, err)
		}
		s.Out.OK("%s: worktree removed", repoKey)
		s.dropBranch(ctx, st, repoKey, clone)
	}

	if len(stuck) == 0 {
		if err := s.removeDir(ctx, u.Dir()); err != nil {
			stuck = append(stuck, Leftover{Path: u.Dir(), Err: err})
		}
	}
	if len(stuck) > 0 {
		// The state file stays, so the universe is still something `rm` can be
		// pointed at: once the paths below are gone, running it again finishes
		// the job rather than leaving a directory nothing accounts for.
		return &NotFullyRemoved{Universe: st.Name, Left: stuck}
	}
	if err := s.Store.Delete(st.ID); err != nil && !errors.Is(err, domain.ErrNotFound) {
		return fmt.Errorf("universe %s: %w", st.Name, err)
	}
	return nil
}

// removeDir removes a directory, and when that is refused for want of
// permission, which is what files a container wrote as root look like, asks a
// container to. Only when that fails too is the original error returned, and
// the person told how to do it by hand.
func (s *Service) removeDir(ctx context.Context, dir string) error {
	err := s.Files.RemoveAll(dir)
	if err == nil || !deniedByOwner(err) {
		return err
	}
	if rootErr := s.Runtime.RemoveAsRoot(ctx, dir); rootErr != nil {
		return err
	}
	s.Out.Info("%s had files a container wrote as root: removed with a container", dir)
	return nil
}

func deniedByOwner(err error) bool {
	return errors.Is(err, fs.ErrPermission) || strings.Contains(strings.ToLower(err.Error()), "permission denied")
}

// Leftover is one path a removal could not take away, and why.
type Leftover struct {
	Repo string
	Path string
	Err  error
}

// NotFullyRemoved says the containers and volumes are gone but something on
// disk is not, and what to do about it. The usual cause is a container that
// wrote into the worktree as root — a node_modules, a cache — which leaves
// files the person who started it cannot delete.
type NotFullyRemoved struct {
	Universe string
	Left     []Leftover
}

func (e *NotFullyRemoved) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "universe %s: its containers and volumes are gone, but this is still on disk:", e.Universe)
	denied := false
	for _, l := range e.Left {
		fmt.Fprintf(&b, "\n    %s: %v", l.Path, l.Err)
		denied = denied || deniedByOwner(l.Err)
	}
	if denied {
		b.WriteString("\n  Files owned by another user, which a container that runs as root leaves behind. Remove them with:")
		for _, l := range e.Left {
			fmt.Fprintf(&b, "\n    sudo rm -rf %s", l.Path)
		}
	}
	fmt.Fprintf(&b, "\n  Then run `multiversectl universe rm %s` again to finish.", e.Universe)
	return b.String()
}

// dropBranch deletes the branch a universe started, so the number can be used
// again without landing on it, but only when every commit on it is somewhere
// else: merged into the base branch or already on its own remote copy. A branch
// the universe merely checked out was never its to delete, and one with work
// that is on no remote is kept and said so.
func (s *Service) dropBranch(ctx context.Context, st domain.UniverseState, repoKey, clone string) {
	owned := false
	for _, k := range st.CreatedBranches {
		owned = owned || k == repoKey
	}
	branch := st.Branches[repoKey]
	if !owned || branch == "" || strings.HasPrefix(branch, "origin/") {
		return
	}
	base, err := s.baseBranch(ctx, repoKey, clone)
	if err != nil {
		s.Out.Info("%s: kept branch %s (%v)", repoKey, branch, err)
		return
	}
	deleted, why, err := s.Git.DeleteBranchIfSafe(ctx, clone, branch, base)
	switch {
	case err != nil:
		s.Out.Warn("%s: could not delete branch %s: %v", repoKey, branch, err)
	case deleted:
		s.Out.OK("%s: branch %s deleted (its commits are elsewhere)", repoKey, branch)
	case why != "":
		s.Out.Warn("%s: kept branch %s: %s; push it, or delete it with `git -C %s branch -D %s`", repoKey, branch, why, clone, branch)
	}
}

// sortedRepoKeys is the manifest's repositories in a fixed order, so what is
// removed, and what is reported, reads the same every time.
func sortedRepoKeys(repos map[string]domain.Repo) []string {
	keys := make([]string, 0, len(repos))
	for key := range repos {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
