package app

import (
	"fmt"
	"path/filepath"
)

// forUniverse is this service as one universe sees the multiverse: the
// repositories' own files (.multiverse.yaml) are read from the universe's
// worktrees, falling back to the shared clone for a repository not checked out
// there. Without it every universe would run on the one file the clone happens
// to hold, a change on a branch could not be tried before it is merged, and
// merging it would not reach a universe until somebody updated the clone.
//
// A service that cannot reload its manifest (no file behind it, or a loader
// that cannot read the parts) is returned as it is.
func (s *Service) forUniverse(name string) (*Service, error) {
	if s.Manifest == nil || s.Manifest.Path == "" || s.Manifests == nil {
		return s, nil
	}
	loader, ok := s.Manifests.(PartLoader)
	if !ok {
		return s, nil
	}
	u := s.Universe(name)
	m, err := loader.LoadMerged(s.Manifest.Path, func(repoKey string) []string {
		return []string{u.RepoDir(repoKey), s.Manifest.CloneDir(s.Multiverse, repoKey)}
	})
	if err != nil {
		return s, fmt.Errorf("universe %s: %w", name, err)
	}
	scoped := *s
	scoped.Manifest = m
	return &scoped, nil
}

// forUniverseOrSelf is forUniverse for what only reads or tears down: a file
// that cannot be read there is said, and the multiverse as the clone describes
// it is what is used, so a broken branch can still be stopped, listed and removed.
func (s *Service) forUniverseOrSelf(name string) *Service {
	scoped, err := s.forUniverse(name)
	if err != nil {
		s.Out.Warn("%v (using the multiverse as the shared clones describe it)", err)
		return s
	}
	return scoped
}

// RepoFile is a repository's own file as a universe reads it, and where from.
type RepoFile struct {
	Repo string
	Path string
	// FromClone is set when the universe's worktree has no such file and the
	// shared clone's is the one in use.
	FromClone bool
}

// repoFileNames are where a repository keeps what it says about itself.
var repoFileNames = []string{".multiverse.yaml", ".multiverse.yml"}

// RepoFiles lists the repositories' own files a universe reads, worktree first.
func (s *Service) RepoFiles(name string) []RepoFile {
	if s.Manifest == nil {
		return nil
	}
	u := s.Universe(name)
	var out []RepoFile
	for _, key := range sortedRepoKeys(s.Manifest.Repos) {
		for i, dir := range []string{u.RepoDir(key), s.Manifest.CloneDir(s.Multiverse, key)} {
			if path := firstExisting(s.Files, dir, repoFileNames); path != "" {
				out = append(out, RepoFile{Repo: key, Path: path, FromClone: i == 1})
				break
			}
		}
	}
	return out
}

func firstExisting(files Files, dir string, names []string) string {
	for _, n := range names {
		if p := filepath.Join(dir, n); files.Exists(p) {
			return p
		}
	}
	return ""
}
