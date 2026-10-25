package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// CreateInput asks for a universe. Name is a number or a branch-like name;
// Branch is checked out in every repository and Branches overrides it per
// repository key. A repository named by neither gets the universe's own
// branch, which for a named universe is its name.
type CreateInput struct {
	Name     string
	Branch   string
	Branches map[string]string
	Note     string
}

// Create scaffolds a universe: one worktree per repository, its secrets and
// its state. It is safe to run again — existing checkouts are kept.
func (s *Service) Create(ctx context.Context, in CreateInput) (domain.UniverseState, error) {
	if err := domain.ValidateUniverseName(in.Name); err != nil {
		return domain.UniverseState{}, err
	}
	u := s.Universe(strings.TrimSpace(in.Name))

	dom, err := s.ResolveDomain(ctx)
	if err != nil {
		return domain.UniverseState{}, err
	}
	if err := s.Files.MkdirAll(u.Dir()); err != nil {
		return domain.UniverseState{}, err
	}

	repoKeys := make([]string, 0, len(s.Manifest.Repos))
	for key := range s.Manifest.Repos {
		repoKeys = append(repoKeys, key)
	}
	sort.Strings(repoKeys)

	branches := map[string]string{}
	for _, repoKey := range repoKeys {
		clone := s.Manifest.CloneDir(s.Workspace, repoKey)
		if !s.Files.Exists(clone) {
			return domain.UniverseState{}, fmt.Errorf("clone %s: %w — run multiversectl update to clone it", clone, domain.ErrNotFound)
		}
		branch := firstNonEmpty(in.Branches[repoKey], in.Branch, u.DefaultBranch())
		dest := u.RepoDir(repoKey)
		if s.Files.Exists(dest) {
			current, err := s.Git.CurrentBranch(ctx, dest)
			if err != nil {
				return domain.UniverseState{}, err
			}
			branches[repoKey] = current
			s.Out.OK("%s: checkout exists on %s", repoKey, current)
			continue
		}
		if err := s.addWorktree(ctx, clone, dest, branch); err != nil {
			return domain.UniverseState{}, fmt.Errorf("%s: %w", repoKey, err)
		}
		branches[repoKey] = branch
		s.Out.OK("%s: worktree on %s", repoKey, branch)
	}

	if err := s.copySecrets(u); err != nil {
		return domain.UniverseState{}, err
	}

	st := domain.UniverseState{
		Name:     u.Name,
		ID:       u.ID(),
		Slug:     u.Slug(),
		Number:   u.Number(),
		Domain:   dom,
		Project:  u.Project(),
		Branches: branches,
		Created:  time.Now().UTC(),
		Note:     in.Note,
		Manifest: s.Manifest.Path,
		Version:  s.Version,
	}
	// The checkouts exist now, so their compose files can be read for the
	// labels that say which addresses this universe answers on.
	rules, err := s.resolveRules(ctx, u, dom)
	if err != nil {
		s.Out.Warn("addresses: %v (they will be resolved on the first start)", err)
	}
	st.Rules, st.Hosts = rules, hostnames(rules)
	if err := s.Store.Save(st); err != nil {
		return domain.UniverseState{}, err
	}
	return st, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// addWorktree checks out an existing branch when there is one and otherwise
// starts it from the clone's default branch.
func (s *Service) addWorktree(ctx context.Context, clone, dest, branch string) error {
	if err := s.Git.Fetch(ctx, clone); err != nil {
		s.Out.Warn("fetch %s: %v (continuing with what is already here)", clone, err)
	}
	kind, err := s.Git.RefExists(ctx, clone, branch)
	if err != nil {
		return err
	}
	switch kind {
	case RefLocal:
		return s.Git.AddWorktree(ctx, clone, dest, branch, "")
	case RefRemote:
		return s.Git.AddWorktree(ctx, clone, dest, branch, "origin/"+branch)
	default:
		base, err := s.Git.DefaultBranch(ctx, clone)
		if err != nil {
			return err
		}
		// Branch from the remote-tracking ref that Fetch has just updated,
		// not from the clone's own copy of it: that one is left wherever it
		// last was and is routinely behind, which would start the universe on
		// stale code without saying so.
		return s.Git.AddWorktree(ctx, clone, dest, branch, "origin/"+base)
	}
}

// copySecrets brings the files that are deliberately not in git — credentials
// and service accounts — into the universe's checkouts.
func (s *Service) copySecrets(u domain.Universe) error {
	for appKey := range s.Manifest.Apps {
		for _, pair := range s.SecretPaths(u, appKey) {
			src, dst := pair[0], pair[1]
			if s.Files.Exists(dst) {
				continue
			}
			if !s.Files.Exists(src) {
				s.Out.Warn("%s: missing %s — the universe will not start until it exists", appKey, src)
				continue
			}
			if err := s.Files.CopyFile(src, dst); err != nil {
				return err
			}
			s.Out.OK("%s: copied %s", appKey, src)
		}
	}
	return nil
}
