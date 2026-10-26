package app

import (
	"context"
	"errors"
	"fmt"
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
	st, err := s.Resolve(in.Universe)
	if err != nil {
		// Resolve names the universe it could not find, and returns a zero
		// state, so wrapping with st.Name would prefix an empty one.
		return err
	}
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

	for _, appKey := range s.Manifest.RunnableApps() {
		if err := s.Runtime.Down(ctx, s.request(u, st, appKey, nil), true); err != nil {
			s.Out.Warn("%s: down: %v", appKey, err)
		}
	}

	for _, repoKey := range sortedRepoKeys(s.Manifest.Repos) {
		dest := u.RepoDir(repoKey)
		if !s.Files.Exists(dest) {
			continue
		}
		clone := s.Manifest.CloneDir(s.Workspace, repoKey)
		// Whatever was in them has been accounted for above, by refusing or by
		// being forced, so this is where the work is actually discarded.
		if err := s.Git.RemoveWorktree(ctx, clone, dest, true); err != nil {
			return fmt.Errorf("universe %s: %s: remove worktree: %w", st.Name, repoKey, err)
		}
		s.Out.OK("%s: worktree removed", repoKey)
		s.dropBranch(ctx, st, repoKey, clone)
	}

	if err := s.Files.RemoveAll(u.Dir()); err != nil {
		return fmt.Errorf("universe %s: %w", st.Name, err)
	}
	if err := s.Store.Delete(st.ID); err != nil && !errors.Is(err, domain.ErrNotFound) {
		return fmt.Errorf("universe %s: %w", st.Name, err)
	}
	return nil
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
	base, err := s.Git.DefaultBranch(ctx, clone)
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
