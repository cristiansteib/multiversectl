package v1alpha1

import (
	"errors"
	"fmt"
	"gopkg.in/yaml.v3"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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
			URL:  r.URL,
			Dir:  orDefault(r.Dir, domain.DirFor(r.URL, r.Name)),
			Base: r.Base,
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
	hooks, err := convertHooks(s.Hooks)
	if err != nil {
		return nil, fmt.Errorf("spec.hooks: %w", err)
	}
	m.Hooks = hooks

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
		OnDemand:        a.Autostart != nil && !*a.Autostart,
		AutostartSet:    a.Autostart != nil,
		DefaultService:  a.DefaultService,
		Optional:        map[string][]string{},
		Hosts:           map[string]domain.HostRule{},
		Env:             map[string]string{},
	}
	for i, e := range a.Env {
		if err := put(app.Env, "env", i, e.Name, e.Value); err != nil {
			return app, err
		}
	}
	hooks, err := convertHooks(a.Hooks)
	if err != nil {
		return app, fmt.Errorf("hooks: %w", err)
	}
	app.Hooks = hooks
	for i, r := range a.Render {
		mode, err := parseMode(r.Mode)
		if err != nil {
			return app, fmt.Errorf("render[%d].mode: %w", i, err)
		}
		app.Render = append(app.Render, domain.Render{To: r.To, Template: r.Template, From: r.From, Mode: mode, Overwrite: r.Overwrite})
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
			Scheme:      h.Scheme,
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
	case s.Root != "", s.Expose != "", s.Domain != "", s.Branch != "", len(s.Env) > 0, s.Proxy.Kind != "", s.Proxy.Network != "", !s.Hooks.empty():
		return nil, errors.New("only spec.apps and spec.proxy.entrypoints can be set here; root, expose, domain, branch, env, hooks and the proxy's kind and network belong to the multiverse")
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

// convertHooks reads every event's list, with after as the earlier name of
// postUp. A hook is a command in a container or on the machine, not both.
func convertHooks(h Hooks) (map[domain.HookEvent][]domain.Hook, error) {
	out := map[domain.HookEvent][]domain.Hook{}
	lists := []struct {
		event domain.HookEvent
		name  string
		hooks []Hook
	}{
		{domain.PostCreate, "postCreate", h.PostCreate},
		{domain.PreUp, "preUp", h.PreUp},
		{domain.PostUp, "postUp", h.PostUp},
		{domain.PostUp, "after", h.After},
		{domain.PreDown, "preDown", h.PreDown},
		{domain.PostDown, "postDown", h.PostDown},
		{domain.PreRemove, "preRemove", h.PreRemove},
	}
	for _, l := range lists {
		for i, w := range l.hooks {
			hook, err := convertHook(w)
			if err != nil {
				return nil, fmt.Errorf("%s[%d]: %w", l.name, i, err)
			}
			out[l.event] = append(out[l.event], hook)
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func convertHook(w Hook) (domain.Hook, error) {
	hook := domain.Hook{Name: w.Name, Once: w.Once, OnFailure: domain.Failure(w.OnFailure)}
	switch {
	case w.Container != nil && w.Host != nil:
		return hook, errors.New("container and host: give one, not both")
	case w.Container != nil:
		hook.Service, hook.Command = w.Container.Service, w.Container.Command
	case w.Host != nil:
		hook.Host, hook.Command, hook.Dir = true, w.Host.Command, w.Host.Dir
	default:
		return hook, errors.New("container or host: give one")
	}
	var err error
	if hook.RetryFor, err = parseDuration("retryFor", w.RetryFor); err != nil {
		return hook, err
	}
	if hook.Timeout, err = parseDuration("timeout", w.Timeout); err != nil {
		return hook, err
	}
	return hook, nil
}

func parseDuration(field, value string) (time.Duration, error) {
	if value == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s: %q is not a duration such as 30s or 3m", field, value)
	}
	return d, nil
}

func (h Hooks) empty() bool {
	return len(h.PostCreate)+len(h.PreUp)+len(h.PostUp)+len(h.PreDown)+len(h.PostDown)+len(h.PreRemove)+len(h.After) == 0
}

// parseMode reads a permission written the way chmod does. "" is the default.
func parseMode(value string) (fs.FileMode, error) {
	if value == "" {
		return 0, nil
	}
	n, err := strconv.ParseUint(value, 8, 32)
	if err != nil || n > 0o777 || n == 0 {
		return 0, fmt.Errorf("%q is not a permission such as 0644", value)
	}
	return fs.FileMode(n), nil
}
