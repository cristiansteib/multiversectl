package app

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// BootstrapInput says where the workspace lives and where its manifest comes
// from: a git URL, which is cloned into the workspace, or a local path.
type BootstrapInput struct {
	Root           string
	ManifestSource string
}

// Bootstrap records this machine's workspace so the tool works from any
// directory afterwards.
func (s *Service) Bootstrap(ctx context.Context, in BootstrapInput) (Config, error) {
	if in.Root == "" || in.ManifestSource == "" {
		return Config{}, fmt.Errorf("bootstrap: root and manifest are both required")
	}
	root, err := s.expandHome(in.Root)
	if err != nil {
		return Config{}, err
	}
	if err := s.Files.MkdirAll(root); err != nil {
		return Config{}, fmt.Errorf("root %s: %w", root, err)
	}

	manifest := in.ManifestSource
	if isGitURL(manifest) {
		dest := filepath.Join(root, ".multiverse", "workspace")
		if s.Files.Exists(dest) {
			s.Out.Info("manifest repository already at %s", dest)
		} else {
			if err := s.Files.MkdirAll(filepath.Dir(dest)); err != nil {
				return Config{}, err
			}
			if err := s.Git.Clone(ctx, manifest, dest); err != nil {
				return Config{}, fmt.Errorf("cloning the manifest repository: %w", err)
			}
		}
		manifest = filepath.Join(dest, "multiverse.yaml")
	}

	cfg := Config{Root: root, Manifest: manifest}
	if err := s.Config.Save(cfg); err != nil {
		return Config{}, fmt.Errorf("saving the configuration: %w", err)
	}
	s.Out.OK("workspace at %s, manifest %s", cfg.Root, cfg.Manifest)
	return cfg, nil
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
