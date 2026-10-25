package app

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// BootstrapInput says where the project lives and where its manifest comes
// from: a git URL, which is cloned into the project, or a local path.
type BootstrapInput struct {
	Name           string // empty: derived from the root directory
	Root           string
	ManifestSource string
	// Ref and ManifestPath only apply to a git source: the revision to check
	// out, and where in the repository the manifest is (default multiverse.yaml).
	Ref          string
	ManifestPath string
	// From and Origin are recorded so a later update or status can compare the
	// project with where it came from.
	From   string
	Origin Origin
}

// Bootstrap records this machine's project so the tool works from any
// directory afterwards. Other projects already recorded are kept.
func (s *Service) Bootstrap(ctx context.Context, in BootstrapInput) (WorkspaceEntry, error) {
	if in.Root == "" || in.ManifestSource == "" {
		return WorkspaceEntry{}, fmt.Errorf("bootstrap: root and manifest are both required")
	}
	root, err := s.expandHome(in.Root)
	if err != nil {
		return WorkspaceEntry{}, err
	}
	if err := s.Files.MkdirAll(root); err != nil {
		return WorkspaceEntry{}, fmt.Errorf("root %s: %w", root, err)
	}

	manifest := in.ManifestSource
	if isGitURL(manifest) {
		dest := filepath.Join(root, ".multiverse", "workspace")
		if s.Files.Exists(dest) {
			s.Out.Info("manifest repository already at %s", dest)
		} else {
			if err := s.Files.MkdirAll(filepath.Dir(dest)); err != nil {
				return WorkspaceEntry{}, err
			}
			if err := s.Git.Clone(ctx, manifest, dest); err != nil {
				return WorkspaceEntry{}, fmt.Errorf("cloning the manifest repository: %w", err)
			}
			if in.Ref != "" {
				if err := s.Git.Checkout(ctx, dest, in.Ref); err != nil {
					return WorkspaceEntry{}, fmt.Errorf("checking out %s: %w", in.Ref, err)
				}
			}
		}
		manifest = filepath.Join(dest, filepath.FromSlash(firstOf(in.ManifestPath, "multiverse.yaml")))
	}

	name := in.Name
	if name == "" {
		name = WorkspaceNameFor(root)
	} else if !workspaceNameRE.MatchString(name) {
		return WorkspaceEntry{}, fmt.Errorf("project name %q must match %s", name, workspaceNameRE)
	}
	cfg, err := s.Config.Load()
	if err != nil {
		return WorkspaceEntry{}, err
	}
	entry := WorkspaceEntry{Root: root, Manifest: manifest, From: in.From, Source: in.Origin}
	if cfg.Workspaces == nil {
		cfg.Workspaces = map[string]WorkspaceEntry{}
	}
	cfg.Workspaces[name] = entry
	// The first project on a machine is the one to work in; later ones are
	// installed beside it, and switching is an explicit `use`.
	if cfg.Current == "" {
		cfg.Current = name
	}
	if err := s.Config.Save(cfg); err != nil {
		return WorkspaceEntry{}, fmt.Errorf("saving the configuration: %w", err)
	}
	s.Out.OK("project %s at %s, manifest %s", name, entry.Root, entry.Manifest)
	return entry, nil
}

// CloneRepos clones every repository of the manifest that is not here yet.
func (s *Service) CloneRepos(ctx context.Context) error {
	if s.Manifest == nil {
		return domain.ErrNoManifest
	}
	repoKeys := make([]string, 0, len(s.Manifest.Repos))
	for key := range s.Manifest.Repos {
		repoKeys = append(repoKeys, key)
	}
	sort.Strings(repoKeys)

	if err := s.Files.MkdirAll(s.Workspace); err != nil {
		return err
	}
	for _, repoKey := range repoKeys {
		dest := s.Manifest.CloneDir(s.Workspace, repoKey)
		if s.Files.Exists(dest) {
			s.Out.OK("%s: already cloned", repoKey)
			continue
		}
		if err := s.Git.Clone(ctx, s.Manifest.Repos[repoKey].URL, dest); err != nil {
			return fmt.Errorf("%s: clone: %w", repoKey, err)
		}
		s.Out.OK("%s: cloned", repoKey)
	}
	return nil
}

// CloneMissing clones the repositories of the manifest that are not here yet,
// and says nothing about the ones that are: what an update reports is what it
// changed.
func (s *Service) CloneMissing(ctx context.Context) error {
	if s.Manifest == nil {
		return domain.ErrNoManifest
	}
	keys := make([]string, 0, len(s.Manifest.Repos))
	for key := range s.Manifest.Repos {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		dest := s.Manifest.CloneDir(s.Workspace, key)
		if s.Files.Exists(dest) {
			continue
		}
		if err := s.Git.Clone(ctx, s.Manifest.Repos[key].URL, dest); err != nil {
			return fmt.Errorf("%s: clone: %w", key, err)
		}
		s.Out.OK("%s: cloned", key)
	}
	return nil
}

func (s *Service) expandHome(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home := s.System.Getenv("HOME")
	if home == "" {
		return "", fmt.Errorf("root %q: HOME is not set", path)
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~")), nil
}

func isGitURL(source string) bool {
	return strings.Contains(source, "://") || strings.HasPrefix(source, "git@")
}
