package domain

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Runtime says how a repository's services are run, or that it has none.
type Runtime string

const (
	RuntimeCompose Runtime = "compose"
	RuntimeNone    Runtime = "none"
)

// Manifest is the workspace description: which repositories make up the
// project, how to run them and how a universe is addressed. It never
// describes containers — that stays in the runtime's own files.
//
// This is the internal model, not the file format: the versioned wire types
// live in the manifest adapter and are converted into this.
// Exposure says who can reach a universe.
type Exposure string

const (
	// ExposeLAN publishes on every interface, for another device to open.
	ExposeLAN Exposure = "lan"
	// ExposeLocal publishes on the loopback address only.
	ExposeLocal Exposure = "local"
)

type Manifest struct {
	Name   string
	Expose Exposure // who can reach a universe: lan or local
	Domain string   // "auto" derives a domain from Expose
	Branch string   // branch template, {{universe}} expands to the number
	Proxy  Proxy
	Repos  map[string]Repo
	Apps   map[string]App
	Env    map[string]string // exported into every runtime invocation

	// Root is where clones and universes live. A manifest that names it can
	// be used from anywhere with -f, which is the point of keeping several
	// manifests side by side.
	Root string

	// Dir is the directory holding the manifest; relative overlay paths and
	// "@" references resolve against it. Stem is the manifest's file name
	// without its extension, used to find overlays belonging to it. Path is
	// the file itself, recorded in a universe's state so one can be traced
	// back to what made it.
	Dir  string
	Stem string
	Path string
}

// Proxy is the shared ingress every universe is reached through.
type Proxy struct {
	Kind        string // "traefik"
	Entrypoints map[string]int
	Network     string
}

// LocalOnly reports that universes must not be reachable from the network.
func (m *Manifest) LocalOnly() bool { return m.Expose == ExposeLocal }

// BindAddress is the address the shared proxy publishes its ports on. Binding
// the loopback address is what actually keeps a universe off the network; the
// hostnames only decide how easy it is to find.
func (m *Manifest) BindAddress() string {
	if m.LocalOnly() {
		return "127.0.0.1"
	}
	return "0.0.0.0"
}

// DefaultBranchFor renders the workspace's branch template for a universe.
func (m *Manifest) DefaultBranchFor(number int) string {
	template := m.Branch
	if template == "" {
		template = "u{{universe}}"
	}
	return strings.ReplaceAll(template, "{{universe}}", strconv.Itoa(number))
}

// Repo is one source repository of the workspace.
type Repo struct {
	URL     string
	Dir     string // clone directory name, defaults to the repo key
	Runtime Runtime
}

// App is a runnable unit: runtime files, the services to start and the
// hostnames it answers on.
type App struct {
	Repo     string
	Compose  []string            // runtime files, repo-relative or "@overlay.yml"
	Profiles []string            // always-on runtime profiles
	Services []string            // services to start; empty means every one the compose files define
	Optional map[string][]string // name -> extra services, enabled with --with
	Secrets  []string            // repo-relative files copied into each universe
	Publish  []string            // the few services allowed to keep a published host port
	Overlay  []byte              // inline compose fragment, rendered by the manifest adapter
	Build    bool                // build images before starting
	Hosts    map[string]HostRule

	// Derived marks an app no one declared: one repository, everything by
	// convention. Such an app may turn out to have no compose file at all,
	// which is a repository with nothing to run rather than a mistake.
	Derived bool
}

// HostRule routes one hostname to one service port through the proxy.
type HostRule struct {
	Service    string
	Port       int
	Entrypoint string   // proxy entrypoint, default "web"
	AlsoMatch  []string // other host keys routed to this same service
}

var nameRe = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Validate reports every problem it finds, so one run fixes the manifest.
func (m *Manifest) Validate() error {
	var errs []string
	if !nameRe.MatchString(m.Name) {
		errs = append(errs, fmt.Sprintf("name %q: want lowercase letters, digits and dashes", m.Name))
	}
	if m.Domain == "" {
		errs = append(errs, "domain: required (\"auto\" to derive it from expose)")
	}
	if m.Expose != ExposeLAN && m.Expose != ExposeLocal {
		errs = append(errs, fmt.Sprintf("expose: want %q or %q", ExposeLAN, ExposeLocal))
	}
	if len(m.Repos) == 0 {
		errs = append(errs, "repos: at least one is required")
	}
	for key, repo := range m.Repos {
		if !nameRe.MatchString(key) {
			errs = append(errs, fmt.Sprintf("repos.%s: invalid key", key))
		}
		if repo.URL == "" {
			errs = append(errs, fmt.Sprintf("repos.%s.url: required", key))
		}
		if repo.Runtime != RuntimeCompose && repo.Runtime != RuntimeNone {
			errs = append(errs, fmt.Sprintf("repos.%s.runtime: want %q or %q", key, RuntimeCompose, RuntimeNone))
		}
	}
	seenHost := map[string]string{}
	for key, app := range m.Apps {
		if !nameRe.MatchString(key) {
			errs = append(errs, fmt.Sprintf("apps.%s: invalid key", key))
		}
		if _, ok := m.Repos[app.Repo]; !ok {
			errs = append(errs, fmt.Sprintf("apps.%s.repo: no such repo %q", key, app.Repo))
		}
		for hostKey, rule := range app.Hosts {
			if !nameRe.MatchString(hostKey) {
				errs = append(errs, fmt.Sprintf("apps.%s.hosts.%s: invalid key", key, hostKey))
			}
			if owner, dup := seenHost[hostKey]; dup {
				errs = append(errs, fmt.Sprintf("apps.%s.hosts.%s: already declared by apps.%s", key, hostKey, owner))
			}
			seenHost[hostKey] = key
			if rule.Service == "" {
				errs = append(errs, fmt.Sprintf("apps.%s.hosts.%s.service: required", key, hostKey))
			}
			if rule.Port <= 0 || rule.Port > 65535 {
				errs = append(errs, fmt.Sprintf("apps.%s.hosts.%s.port: %d is out of range", key, hostKey, rule.Port))
			}
			if rule.Entrypoint != "" {
				if _, ok := m.Proxy.Entrypoints[rule.Entrypoint]; !ok {
					errs = append(errs, fmt.Sprintf("apps.%s.hosts.%s.entrypoint: no such proxy entrypoint %q", key, hostKey, rule.Entrypoint))
				}
			}
		}
	}
	if len(errs) == 0 {
		return nil
	}
	sort.Strings(errs)
	return &ValidationError{Problems: errs}
}

// CloneDir is where a repository is cloned in the multiverse root.
func (m *Manifest) CloneDir(root, repoKey string) string {
	dir := m.Repos[repoKey].Dir
	if dir == "" {
		dir = repoKey
	}
	return filepath.Join(root, dir)
}

// RunnableApps returns the app keys whose repository has a runtime, sorted so
// start-up order is reproducible.
func (m *Manifest) RunnableApps() []string {
	var keys []string
	for key, app := range m.Apps {
		if m.Repos[app.Repo].Runtime == RuntimeCompose {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

// ComposeFiles resolves an app's runtime files: "@name.yml" comes from the
// manifest directory, anything else from the repository checkout.
//
// With nothing declared it looks for the two conventional places — the
// repository's own docker-compose.yml and overlays/<app>.yml beside the
// manifest — so an app that follows the convention needs no `compose:` line.
func (m *Manifest) ComposeFiles(appKey, repoDir string) []string {
	declared := m.Apps[appKey].Compose
	if len(declared) == 0 {
		// Two conventional homes for an overlay: overlays/<app>.yml for a
		// directory holding one manifest, and <manifest-name>/<app>.yml for a
		// directory holding several.
		declared = []string{"docker-compose.yml", "@overlays/" + appKey + ".yml"}
		if m.Stem != "" {
			declared = append(declared, "@"+m.Stem+"/"+appKey+".yml")
		}
	}
	var out []string
	for _, file := range declared {
		if len(file) > 0 && file[0] == '@' {
			out = append(out, filepath.Join(m.Dir, file[1:]))
			continue
		}
		out = append(out, filepath.Join(repoDir, file))
	}
	return out
}
