package v1alpha1

import (
	"errors"
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

// Convert builds the domain manifest from a decoded multiverse, applying
// defaults. dir is the directory holding the manifest file.
func Convert(w *Multiverse, dir string) (*domain.Manifest, error) {
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
			URL: r.URL,
			Dir: orDefault(r.Dir, domain.DirFor(r.URL, r.Name)),
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
	// A repository that no app names gets an app of its own name, which is
	// skipped when the repository has no compose file. Writing
	// `- name: x / repo: x` said nothing the repository had not already said.
	named := map[string]bool{}
	for _, a := range m.Apps {
		named[a.Repo] = true
	}
	for key := range m.Repos {
		if named[key] {
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
		Repo:            a.Repo,
		Compose:         a.Compose,
		Profiles:        a.Profiles,
		Services:        a.Services,
		Secrets:         a.Secrets,
		SecretTemplates: a.SecretTemplates,
		Publish:         a.Publish,
		Build:           a.Build,
		DefaultService:  a.DefaultService,
		Optional:        map[string][]string{},
		Hosts:           map[string]domain.HostRule{},
	}
	for _, h := range a.Hooks.After {
		app.After = append(app.After, domain.Hook{Service: h.Container.Service, Command: h.Container.Command, Once: h.Once})
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
			Service:     h.Service,
			Port:        h.Port,
			Entrypoint:  orDefault(h.Entrypoint, defaultEntrypoint),
			AlsoMatch:   h.AlsoMatch,
			PortForward: h.PortForward,
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

// ConvertPart reads a repository's own file: the same kind as a multiverse,
// but about one repository only, so it lists no repositories and sets nothing
// that belongs to the whole multiverse. An app that leaves out its repo, or its
// name when it is the only one, is the repository's.
func ConvertPart(w *Multiverse, repoKey string) (*domain.Part, error) {
	s := w.Spec
	switch {
	case len(s.Repos) > 0:
		return nil, errors.New("spec.repos: a repository's own file describes only itself; list repositories in the multiverse that uses it")
	case s.Root != "", s.Expose != "", s.Domain != "", s.Branch != "", len(s.Env) > 0, s.Proxy.Kind != "", s.Proxy.Network != "":
		return nil, errors.New("only spec.apps and spec.proxy.entrypoints can be set here; root, expose, domain, branch, env and the proxy's kind and network belong to the multiverse")
	}
	p := &domain.Part{Apps: map[string]domain.App{}, Entrypoints: map[string]int{}}
	for i, e := range s.Proxy.Entrypoints {
		if err := put(p.Entrypoints, "spec.proxy.entrypoints", i, e.Name, e.Port); err != nil {
			return nil, err
		}
	}
	for i, a := range s.Apps {
		if a.Repo != "" && a.Repo != repoKey {
			return nil, fmt.Errorf("spec.apps[%d]: repo %q — an app in a repository's own file is that repository's (leave repo out)", i, a.Repo)
		}
		if a.Name == "" && len(s.Apps) == 1 {
			a.Name = repoKey
		}
		app, err := convertApp(a)
		if err != nil {
			return nil, fmt.Errorf("spec.apps[%d] %q: %w", i, a.Name, err)
		}
		app.Repo = repoKey
		if err := put(p.Apps, "spec.apps", i, a.Name, app); err != nil {
			return nil, err
		}
	}
	return p, nil
}
