package v1alpha1

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"strings"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

const (
	defaultDomain     = "auto"
	defaultExpose     = string(domain.ExposeLAN)
	defaultBranch     = "u{{universe}}"
	defaultProxyKind  = "traefik"
	defaultNetwork    = "multiversenet"
	defaultEntrypoint = "web"
	defaultWebPort    = 80
)

// Convert builds the domain manifest from a decoded workspace, applying
// defaults. dir is the directory holding the manifest file.
func Convert(w *Workspace, dir string) (*domain.Manifest, error) {
	s := w.Spec
	m := &domain.Manifest{
		Name:   w.ObjectMeta.Name,
		Expose: domain.Exposure(orDefault(s.Expose, defaultExpose)),
		Domain: orDefault(s.Domain, defaultDomain),
		Branch: orDefault(s.Branch, defaultBranch),
		Proxy: domain.Proxy{
			Kind:        orDefault(s.Proxy.Kind, defaultProxyKind),
			Network:     orDefault(s.Proxy.Network, defaultNetwork),
			Entrypoints: map[string]int{},
		},
		Repos: map[string]domain.Repo{},
		Apps:  map[string]domain.App{},
		Env:   map[string]string{},
		Dir:   dir,
		Root:  expandHome(s.Root),
	}

	for i, e := range s.Proxy.Entrypoints {
		if err := put(m.Proxy.Entrypoints, "spec.proxy.entrypoints", i, e.Name, e.Port); err != nil {
			return nil, err
		}
	}
	if len(m.Proxy.Entrypoints) == 0 {
		// Host rules default to "web", so a manifest with no entrypoints still needs one.
		m.Proxy.Entrypoints[defaultEntrypoint] = defaultWebPort
	}
	for i, r := range s.Repos {
		err := put(m.Repos, "spec.repos", i, r.Name, domain.Repo{
			URL:     r.URL,
			Dir:     r.Dir,
			Runtime: domain.Runtime(orDefault(r.Runtime, string(domain.RuntimeCompose))),
		})
		if err != nil {
			return nil, err
		}
	}
	for i, a := range s.Apps {
		app, err := convertApp(a)
		if err != nil {
			return nil, fmt.Errorf("spec.apps[%d] %q: %w", i, a.Name, err)
		}
		if err := put(m.Apps, "spec.apps", i, a.Name, app); err != nil {
			return nil, err
		}
	}
	// A repository that runs something and that no app names gets an app of
	// its own name. Writing `- name: x / repo: x` said nothing the repository
	// had not already said.
	named := map[string]bool{}
	for _, a := range m.Apps {
		named[a.Repo] = true
	}
	for key, repo := range m.Repos {
		if repo.Runtime != domain.RuntimeCompose || named[key] {
			continue
		}
		if _, clash := m.Apps[key]; clash {
			continue
		}
		m.Apps[key] = domain.App{
			Repo:     key,
			Optional: map[string][]string{},
			Hosts:    map[string]domain.HostRule{},
			Derived:  true,
		}
	}

	for i, e := range s.Env {
		if err := put(m.Env, "spec.env", i, e.Name, e.Value); err != nil {
			return nil, err
		}
	}

	if err := m.Validate(); err != nil {
		return nil, err
	}
	return m, nil
}

func convertApp(a App) (domain.App, error) {
	app := domain.App{
		Repo:     a.Repo,
		Compose:  a.Compose,
		Profiles: a.Profiles,
		Services: a.Services,
		Secrets:  a.Secrets,
		Publish:  a.Publish,
		Build:    a.Build,
		Optional: map[string][]string{},
		Hosts:    map[string]domain.HostRule{},
	}
	if !a.Overlay.IsZero() {
		// Rendered here, in the format adapter, so neither the domain nor the
		// use cases need to know what YAML is.
		raw, err := yaml.Marshal(a.Overlay)
		if err != nil {
			return app, fmt.Errorf("overlay: %w", err)
		}
		app.Overlay = raw
	}
	for i, o := range a.Optional {
		if err := put(app.Optional, "optional", i, o.Name, o.Services); err != nil {
			return app, err
		}
	}
	for i, h := range a.Hosts {
		err := put(app.Hosts, "hosts", i, h.Name, domain.HostRule{
			Service:    h.Service,
			Port:       h.Port,
			Entrypoint: orDefault(h.Entrypoint, defaultEntrypoint),
			AlsoMatch:  h.AlsoMatch,
		})
		if err != nil {
			return app, err
		}
	}
	return app, nil
}

// put stores a named list item in its map, rejecting empty and repeated names.
// expandHome keeps a manifest portable between machines: a root written as
// ~/work/acme resolves per user.
func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(path, "~"), "/"))
		}
	}
	return path
}

func put[V any](into map[string]V, field string, index int, name string, v V) error {
	if name == "" {
		return fmt.Errorf("%s[%d]: name is required", field, index)
	}
	if _, dup := into[name]; dup {
		return fmt.Errorf("%s: duplicate name %q", field, name)
	}
	into[name] = v
	return nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
