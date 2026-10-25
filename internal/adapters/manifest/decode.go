package manifest

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/cristiansteib/multiversectl/internal/adapters/manifest/v1alpha1"
	"github.com/cristiansteib/multiversectl/internal/domain"
)

var fileNames = []string{"multiverse.yaml", "multiverse.yml"}

type envelope struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
}

// Decode reads the envelope first so an unsupported version is reported as
// such, instead of as a pile of unknown-field errors from the wrong schema.
func Decode(data []byte, dir string) (*domain.Manifest, error) {
	var env envelope
	if err := yaml.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("manifest is not valid YAML: %w", err)
	}
	if env.APIVersion == "" {
		return nil, fmt.Errorf("manifest has no apiVersion; supported: %s", v1alpha1.APIVersion)
	}
	if env.APIVersion != v1alpha1.APIVersion {
		return nil, fmt.Errorf("unsupported apiVersion %q; supported: %s", env.APIVersion, v1alpha1.APIVersion)
	}
	if env.Kind != v1alpha1.Kind {
		return nil, fmt.Errorf("unsupported kind %q for apiVersion %s; supported: %s", env.Kind, env.APIVersion, v1alpha1.Kind)
	}

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var w v1alpha1.Workspace
	if err := dec.Decode(&w); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("decode %s %s: %w", env.APIVersion, env.Kind, err)
	}
	return v1alpha1.Convert(&w, dir)
}

func Load(path string) (*domain.Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	m, err := Decode(data, filepath.Dir(path))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	m.Stem = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	m.Path = path
	return m, nil
}

// Locate walks up from startDir to the filesystem root.
func Locate(startDir string) (string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", startDir, err)
	}
	for {
		for _, name := range fileNames {
			candidate := filepath.Join(dir, name)
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("looked for %s from %s upwards: %w", strings.Join(fileNames, " or "), startDir, domain.ErrNoManifest)
		}
		dir = parent
	}
}
