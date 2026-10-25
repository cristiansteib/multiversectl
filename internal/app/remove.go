package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// RemoveInput names the universe to delete. Confirmation is the caller's job.
type RemoveInput struct {
	Universe string
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

	for _, appKey := range s.Manifest.RunnableApps() {
		if err := s.Runtime.Down(ctx, s.request(u, st, appKey, nil), true); err != nil {
			s.Out.Warn("%s: down: %v", appKey, err)
		}
	}

	repoKeys := make([]string, 0, len(s.Manifest.Repos))
	for key := range s.Manifest.Repos {
		repoKeys = append(repoKeys, key)
	}
	sort.Strings(repoKeys)
	for _, repoKey := range repoKeys {
		dest := u.RepoDir(repoKey)
		if !s.Files.Exists(dest) {
			continue
		}
		clone := s.Manifest.CloneDir(s.Workspace, repoKey)
		if err := s.Git.RemoveWorktree(ctx, clone, dest); err != nil {
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
