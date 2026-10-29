package manifest

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/cristiansteib/multiversectl/internal/adapters/manifest/v1alpha1"
	"github.com/cristiansteib/multiversectl/internal/domain"
)

// partNames are where a repository keeps what it says about itself. A dotfile,
// so it is never taken for a multiverse of its own: those are multiverse.yaml
// and multiverse/*.yaml.
var partNames = []string{".multiverse.yaml", ".multiverse.yml"}

// DecodePart reads a repository's own file.
func DecodePart(data []byte, repoKey string) (*domain.Part, error) {
	var env envelope
	if err := yaml.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("not valid YAML: %w", err)
	}
	if env.APIVersion != v1alpha1.APIVersion {
		return nil, fmt.Errorf("apiVersion %q; supported: %s", env.APIVersion, v1alpha1.APIVersion)
	}
	if env.Kind != v1alpha1.Kind {
		return nil, fmt.Errorf("kind %q; this file is a %s", env.Kind, v1alpha1.Kind)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var w v1alpha1.Multiverse
	if err := dec.Decode(&w); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return v1alpha1.ConvertPart(&w, repoKey)
}

// MergeParts adds to a manifest what each of its cloned repositories says about
// itself. A repository not cloned yet, or with no file of its own, is left as
// the multiverse describes it.
func MergeParts(m *domain.Manifest, root string) error {
	return MergePartsFrom(m, func(key string) []string { return []string{m.CloneDir(root, key)} })
}

// MergePartsFrom is MergeParts with the directories to look in given per
// repository, first match wins: a universe reads its own worktree and falls
// back to the shared clone.
func MergePartsFrom(m *domain.Manifest, dirs func(repoKey string) []string) error {
	keys := make([]string, 0, len(m.Repos))
	for key := range m.Repos {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		merged := false
		for _, dir := range dirs(key) {
			for _, name := range partNames {
				path := filepath.Join(dir, name)
				data, err := os.ReadFile(path)
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				if err != nil {
					return fmt.Errorf("repo %s: %w", key, err)
				}
				part, err := DecodePart(data, key)
				if err != nil {
					return fmt.Errorf("repo %s: %s: %w", key, path, err)
				}
				if err := m.Merge(key, *part); err != nil {
					return fmt.Errorf("repo %s: %s: %w", key, path, err)
				}
				merged = true
				break
			}
			if merged {
				break
			}
		}
	}
	return nil
}

// LoadMerged is Load followed by the repositories' own files, read from the
// directories dirs names for each.
func LoadMerged(path string, dirs func(repoKey string) []string) (*domain.Manifest, error) {
	m, err := Load(path)
	if err != nil {
		return nil, err
	}
	if err := MergePartsFrom(m, dirs); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return m, nil
}
