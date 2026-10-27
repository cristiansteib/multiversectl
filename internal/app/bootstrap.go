package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// BootstrapInput says where the multiverse lives and where its manifest comes
// from: a git URL, which is cloned into the multiverse, or a local path.
type BootstrapInput struct {
	Name           string // empty: derived from the root directory
	Root           string
	ManifestSource string
	// Ref and ManifestPath only apply to a git source: the revision to check
	// out, and where in the repository the manifest is (default multiverse.yaml).
	Ref          string
	ManifestPath string
	// From and Origin are recorded so a later update or status can compare the
	// multiverse with where it came from.
	From   string
	Origin Origin
}

// Bootstrap records this machine's multiverse so the tool works from any
// directory afterwards. Other multiverses already recorded are kept.
func (s *Service) Bootstrap(ctx context.Context, in BootstrapInput) (MultiverseEntry, error) {
	if in.Root == "" || in.ManifestSource == "" {
		return MultiverseEntry{}, fmt.Errorf("bootstrap: root and manifest are both required")
	}
	root, err := s.expandHome(in.Root)
	if err != nil {
		return MultiverseEntry{}, err
	}
	if err := s.Files.MkdirAll(root); err != nil {
		return MultiverseEntry{}, fmt.Errorf("root %s: %w", root, err)
	}

	manifest := in.ManifestSource
	if isGitURL(manifest) {
		dest := filepath.Join(root, ".multiverse", "source")
		if s.Files.Exists(dest) {
			s.Out.Info("manifest repository already at %s", dest)
		} else {
			if err := s.Files.MkdirAll(filepath.Dir(dest)); err != nil {
				return MultiverseEntry{}, err
			}
			if err := s.Git.Clone(ctx, manifest, dest); err != nil {
				return MultiverseEntry{}, fmt.Errorf("cloning the manifest repository: %w", err)
			}
			if in.Ref != "" {
				if err := s.Git.Checkout(ctx, dest, in.Ref); err != nil {
					return MultiverseEntry{}, fmt.Errorf("checking out %s: %w", in.Ref, err)
				}
			}
		}
		manifest = filepath.Join(dest, filepath.FromSlash(firstOf(in.ManifestPath, "multiverse.yaml")))
	}

	name := in.Name
	if name == "" {
		name = MultiverseNameFor(root)
	} else if !multiverseNameRE.MatchString(name) {
		return MultiverseEntry{}, fmt.Errorf("multiverse name %q must match %s", name, multiverseNameRE)
	}
	cfg, err := s.Config.Load()
	if err != nil {
		return MultiverseEntry{}, err
	}
	entry := MultiverseEntry{Root: root, Manifest: manifest, From: in.From, Source: in.Origin}
	if cfg.Multiverses == nil {
		cfg.Multiverses = map[string]MultiverseEntry{}
	}
	cfg.Multiverses[name] = entry
	if err := s.Config.Save(cfg); err != nil {
		return MultiverseEntry{}, fmt.Errorf("saving the configuration: %w", err)
	}
	s.Out.OK("multiverse %s at %s, manifest %s", name, entry.Root, entry.Manifest)
	return entry, nil
}

// cloneLimit is how many clones run at once: enough to hide the latency of each,
// few enough not to be throttled by the host.
const cloneLimit = 4

// CloneRepos clones every repository of the manifest that is not here yet.
func (s *Service) CloneRepos(ctx context.Context) error {
	return s.clone(ctx, true)
}

// CloneMissing clones the repositories of the manifest that are not here yet,
// and says nothing about the ones that are: what an update reports is what it
// changed.
func (s *Service) CloneMissing(ctx context.Context) error {
	return s.clone(ctx, false)
}

func (s *Service) clone(ctx context.Context, sayPresent bool) error {
	if s.Manifest == nil {
		return domain.ErrNoManifest
	}
	keys := make([]string, 0, len(s.Manifest.Repos))
	for key := range s.Manifest.Repos {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if err := s.Files.MkdirAll(s.Multiverse); err != nil {
		return err
	}

	var jobs []Job
	for _, key := range keys {
		key, dest := key, s.Manifest.CloneDir(s.Multiverse, key)
		if s.Files.Exists(dest) {
			if sayPresent {
				s.Out.OK("%s: already cloned", key)
			}
			continue
		}
		url := s.Manifest.Repos[key].URL
		jobs = append(jobs, Job{Name: key, Run: func(ctx context.Context) error {
			return s.Git.Clone(ctx, url, dest)
		}})
	}
	if len(jobs) == 0 {
		return nil
	}
	var progress Progress = noProgress{}
	if s.Progress != nil {
		progress = s.Progress
	}
	errs := progress.Parallel(fmt.Sprintf("Cloning %s", plural(len(jobs), "repository")), cloneLimit, jobs)

	// Reported after the lines are gone, in the manifest's order, and every
	// failure is named: one bad URL does not hide the others.
	var failed []string
	for i, j := range jobs {
		if errs[i] != nil {
			failed = append(failed, fmt.Sprintf("%s: clone: %v", j.Name, errs[i]))
			continue
		}
		s.Out.OK("%s: cloned", j.Name)
	}
	if len(failed) > 0 {
		if len(failed) == 1 {
			return errors.New(failed[0])
		}
		return fmt.Errorf("%d repositories could not be cloned:\n  %s", len(failed), strings.Join(failed, "\n  "))
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
