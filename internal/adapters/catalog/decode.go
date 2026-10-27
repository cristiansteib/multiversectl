package catalog

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/cristiansteib/multiversectl/internal/adapters/catalog/v1alpha1"
	"github.com/cristiansteib/multiversectl/internal/app"
	"github.com/cristiansteib/multiversectl/internal/domain"
)

const (
	DefaultCatalogPath    = "catalog.yaml"
	DefaultMultiversePath = "multiverse.yaml"
)

var nameRE = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Decoder is the app.CatalogDecoder for v1alpha1 files.
type Decoder struct{}

var _ app.CatalogDecoder = Decoder{}

// Decode checks the envelope first, as manifest.Decode does, so a wrong
// version or kind is reported as that and not as unknown fields.
func (Decoder) Decode(data []byte) (*domain.Catalog, error) {
	var env struct {
		APIVersion string `yaml:"apiVersion"`
		Kind       string `yaml:"kind"`
	}
	if err := yaml.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("catalog is not valid YAML: %w", err)
	}
	if env.APIVersion != v1alpha1.APIVersion {
		return nil, fmt.Errorf("unsupported apiVersion %q; supported: %s", env.APIVersion, v1alpha1.APIVersion)
	}
	if env.Kind != v1alpha1.Kind {
		return nil, fmt.Errorf("unsupported kind %q; this file must be a %s", env.Kind, v1alpha1.Kind)
	}

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var c v1alpha1.Catalog
	if err := dec.Decode(&c); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("decode %s %s: %w", env.APIVersion, env.Kind, err)
	}
	return convert(&c)
}

func convert(c *v1alpha1.Catalog) (*domain.Catalog, error) {
	if !nameRE.MatchString(c.ObjectMeta.Name) {
		return nil, fmt.Errorf("metadata.name %q must match %s", c.ObjectMeta.Name, nameRE)
	}
	out := &domain.Catalog{Name: c.ObjectMeta.Name}
	seen := map[string]bool{}
	claim := func(name string) error {
		if !nameRE.MatchString(name) {
			return fmt.Errorf("name %q must match %s", name, nameRE)
		}
		if seen[name] {
			return fmt.Errorf("name %q is used twice; siblings need distinct names", name)
		}
		seen[name] = true
		return nil
	}
	for i, r := range c.Spec.Catalogs {
		if err := claim(r.Name); err != nil {
			return nil, fmt.Errorf("spec.catalogs[%d]: %w", i, err)
		}
		src, err := source(r.Source, DefaultCatalogPath)
		if err != nil {
			return nil, fmt.Errorf("spec.catalogs[%d] %s: %w", i, r.Name, err)
		}
		out.Catalogs = append(out.Catalogs, domain.CatalogRef{Name: r.Name, Source: src})
	}
	for i, r := range c.Spec.Multiverses {
		if err := claim(r.Name); err != nil {
			return nil, fmt.Errorf("spec.multiverses[%d]: %w", i, err)
		}
		src, err := source(r.Source, DefaultMultiversePath)
		if err != nil {
			return nil, fmt.Errorf("spec.multiverses[%d] %s: %w", i, r.Name, err)
		}
		out.Multiverses = append(out.Multiverses, domain.MultiverseRef{Name: r.Name, Source: src})
	}
	return out, nil
}

// source rejects paths that leave the repository: a catalog is fetched from
// other people's repositories, so a reference must not read outside its own.
func source(s v1alpha1.Source, defaultPath string) (domain.Source, error) {
	if s.Ref != "" && s.Repo == "" {
		return domain.Source{}, errors.New("source.ref needs source.repo; without one the reference stays in the current repository and ref")
	}
	p := s.Path
	if p == "" {
		p = defaultPath
	}
	clean := path.Clean(p)
	if path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
		return domain.Source{}, fmt.Errorf("source.path %q must stay inside the repository", p)
	}
	return domain.Source{Repo: s.Repo, Ref: s.Ref, Path: clean}, nil
}
