package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// PruneOptions say how much of a project's universes a prune may take.
type PruneOptions struct {
	// All includes universes with containers running; without it those are kept.
	All bool
	// Force includes universes with uncommitted work in a worktree; without it
	// those are kept, because removing a worktree discards that work.
	Force bool
}

// PruneItem is one universe and what a prune would do with it.
type PruneItem struct {
	Name     string
	Dir      string
	Running  int
	Branches map[string]string
	// Dirty names the repositories whose worktree has uncommitted changes.
	Dirty []string
	// Skip is why it is kept, "" when a prune would remove it.
	Skip string
}

// PrunePlan reports every universe of the project and whether a prune with
// these options would remove it. Nothing is changed. It is also the footprint
// a plain overview shows, which is why every universe is listed.
func (s *Service) PrunePlan(ctx context.Context, o PruneOptions) ([]PruneItem, error) {
	states, err := s.Store.List()
	if err != nil {
		return nil, fmt.Errorf("listing universes: %w", err)
	}
	items := make([]PruneItem, 0, len(states))
	for _, st := range states {
		u := s.Universe(st.Name)
		it := PruneItem{Name: st.Name, Dir: u.Dir(), Branches: st.Branches}
		containers, err := s.Runtime.Containers(ctx, st.Project)
		if err != nil {
			s.Out.Warn("universe %s: containers: %v", st.Name, err)
		}
		it.Running = countRunning(containers)

		keys := make([]string, 0, len(st.Branches))
		for k := range st.Branches {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, key := range keys {
			dir := u.RepoDir(key)
			if !s.Files.Exists(dir) {
				continue
			}
			// A worktree that cannot be read is not known to be clean.
			if clean, err := s.Git.IsClean(ctx, dir); err != nil || !clean {
				it.Dirty = append(it.Dirty, key)
			}
		}

		switch {
		case it.Running > 0 && !o.All:
			it.Skip = "running (--all includes it)"
		case len(it.Dirty) > 0 && !o.Force:
			it.Skip = "uncommitted work in " + strings.Join(it.Dirty, ", ") + " (--force discards it)"
		}
		items = append(items, it)
	}
	return items, nil
}

// Prune removes the universes of a plan that are not skipped, the way rm does:
// containers and volumes, worktrees, directory and state. One that fails is
// reported and does not stop the rest. The shared clones, the branches in
// them, the proxy and the installed project are untouched.
// Prune holds no lock of its own: it removes universe after universe, and
// Remove takes the locks for each. Holding the project lock here would be the
// same process asking for a lock it already has, which is a wait that never
// ends.
func (s *Service) Prune(ctx context.Context, items []PruneItem) (removed int, err error) {
	var errs []error
	for _, it := range items {
		if it.Skip != "" {
			continue
		}
		if err := s.Remove(ctx, RemoveInput{Universe: it.Name}); err != nil {
			errs = append(errs, err)
			continue
		}
		removed++
	}
	return removed, errors.Join(errs...)
}
