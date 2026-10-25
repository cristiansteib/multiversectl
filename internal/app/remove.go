package app

import (
	"context"
	"errors"
	"fmt"
	"sort"

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
		return fmt.Errorf("universe %s: %w", st.Name, err)
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
	}

	if err := s.Files.RemoveAll(u.Dir()); err != nil {
		return fmt.Errorf("universe %s: %w", st.Name, err)
	}
	if err := s.Store.Delete(st.ID); err != nil && !errors.Is(err, domain.ErrNotFound) {
		return fmt.Errorf("universe %s: %w", st.Name, err)
	}
	return nil
}
