package domain

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Manifest is the multiverse description: which repositories make up the
// multiverse, how to run them and how a universe is addressed. It never
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

// DefaultBranchFor renders the multiverse's branch template for a universe.
func (m *Manifest) DefaultBranchFor(number int) string {
	template := m.Branch
	if template == "" {
		template = "u{{universe}}"
	}
	return strings.ReplaceAll(template, "{{universe}}", strconv.Itoa(number))
}

// Repo is one source repository of the multiverse.
type Repo struct {
	URL string
	Dir string // clone directory name, defaults to the repository's own name
}

// DirFor is the directory a repository is cloned into when the manifest does
// not say: the repository's own name, as git would. fallback is used for an
// address that has none.
func DirFor(url, fallback string) string {
	l := Locate(url, "")
	p := strings.TrimSuffix(strings.TrimRight(l.Path, "/\\"), ".git")
	if i := strings.LastIndexAny(p, "/\\"); i >= 0 {
		p = p[i+1:]
	}
	if p == "" || p == "." || p == ".." {
		return fallback
	}
	return p
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
	// SecretTemplates says, for a secret, the repo-relative file a missing one is
	// made from; absent means <name>.example, and "" means none.
	SecretTemplates map[string]string
	Publish         []string // the few services allowed to keep a published host port
	// DefaultService is where exec and run go when no service is named and the
	// app has several.
	DefaultService string
	// After are the hooks run once the app's services are up.
	After   []Hook
	Overlay []byte // inline compose fragment, rendered by the manifest adapter
	Build   bool   // build images before starting
	Hosts   map[string]HostRule

	// Derived marks an app no one declared: one repository, everything by
	// convention. Such an app may turn out to have no compose file at all,
	// which is a repository with nothing to run rather than a mistake.
	Derived bool
}

// Hook is a command run in a service of an app's universe once it is up.
type Hook struct {
	Service string // empty: the app's only service, or its defaultService
	Command []string
	// Once runs it on a universe's first start only.
	Once bool
}

// Key names a hook within a universe, which is what remembers that a Once hook
// has run: the app, the service and the command, so reordering the hooks does
// not make one run again.
func (h Hook) Key(app string) string {
	return app + "|" + h.Service + "|" + strings.Join(h.Command, " ")
}

// HostRule routes one hostname to one service port through the proxy.
type HostRule struct {
	Service    string
	Port       int
	Entrypoint string   // proxy entrypoint, default "web"
	AlsoMatch  []string // other host keys routed to this same service
	// PortForward is "auto" when the host is also reachable by IP and a free
	// port: a client that cannot use a hostname, such as a phone refusing
	// plain http to a name, needs an address it may call.
	PortForward string
}

// PortForwardAuto marks a resolved host as also published on a host port that
// the tool chooses: free when first chosen, then kept.
const PortForwardAuto = "auto"

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
	}
	seenHost := map[string]string{}
	for key, app := range m.Apps {
		if !nameRe.MatchString(key) {
			errs = append(errs, fmt.Sprintf("apps.%s: invalid key", key))
		}
		if _, ok := m.Repos[app.Repo]; !ok {
			errs = append(errs, fmt.Sprintf("apps.%s.repo: no such repo %q", key, app.Repo))
		}
		if app.DefaultService != "" && len(app.Services) > 0 && !slices.Contains(app.Services, app.DefaultService) {
			errs = append(errs, fmt.Sprintf("apps.%s.defaultService: %q is not one of its services (%s)", key, app.DefaultService, strings.Join(app.Services, ", ")))
		}
		for name, tpl := range app.SecretTemplates {
			switch {
			case !slices.Contains(app.Secrets, name):
				errs = append(errs, fmt.Sprintf("apps.%s.secretTemplates.%s: not one of its secrets (%s)", key, name, strings.Join(app.Secrets, ", ")))
			case tpl != "" && !filepath.IsLocal(tpl):
				// It is read from the shared clone: it must stay inside it.
				errs = append(errs, fmt.Sprintf("apps.%s.secretTemplates.%s: %q must be a path inside the repository", key, name, tpl))
			case tpl == name:
				errs = append(errs, fmt.Sprintf("apps.%s.secretTemplates.%s: a secret cannot be made from itself", key, name))
			}
		}
		for i, hook := range app.After {
			if len(hook.Command) == 0 {
				errs = append(errs, fmt.Sprintf("apps.%s.hooks.after[%d].container.command: required", key, i))
			}
			if hook.Service != "" && len(app.Services) > 0 && !slices.Contains(app.Services, hook.Service) {
				errs = append(errs, fmt.Sprintf("apps.%s.hooks.after[%d].container.service: %q is not one of its services (%s)", key, i, hook.Service, strings.Join(app.Services, ", ")))
			}
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

// RunnableApps returns the app keys, sorted so start-up order is reproducible.
// An app whose repository turns out to have nothing to run is skipped where it
// is acted on, not here: only the files on disk know.
func (m *Manifest) RunnableApps() []string {
	keys := make([]string, 0, len(m.Apps))
	for key := range m.Apps {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// ComposeNames are the file names docker compose looks for, in the order it
// prefers them: a repository with more than one is run from the first.
var ComposeNames = []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"}

// ComposeFiles resolves an app's runtime files: "@name.yml" comes from the
// manifest directory, anything else from the repository checkout.
//
// With nothing declared it looks for the two conventional places — the
// repository's own compose file and overlays/<app>.yml beside the
// manifest — so an app that follows the convention needs no `compose:` line.
func (m *Manifest) ComposeFiles(appKey, repoDir string) []string {
	declared := m.Apps[appKey].Compose
	if len(declared) == 0 {
		// Two conventional homes for an overlay: overlays/<app>.yml for a
		// directory holding one manifest, and <manifest-name>/<app>.yml for a
		// directory holding several.
		declared = append(append([]string{}, ComposeNames...), "@overlays/"+appKey+".yml")
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

// ProxyClashes says where two multiverses disagree about the proxy. There is
// one proxy per machine, so sharing its network and its ports is the design:
// the trouble is that starting a multiverse rewrites the proxy from its own
// manifest, which drops what another multiverse needs from it. aName and bName
// are what the user calls the multiverses, which is not always metadata.name.
func ProxyClashes(aName string, a *Manifest, bName string, b *Manifest) []string {
	var out []string
	if a.Proxy.Network != "" && b.Proxy.Network != "" && a.Proxy.Network != b.Proxy.Network {
		out = append(out, fmt.Sprintf("%s and %s want different proxy networks (%s, %s) but a machine has one proxy; only one is reachable",
			aName, bName, a.Proxy.Network, b.Proxy.Network))
	}
	for _, n := range sortedEntrypoints(a.Proxy.Entrypoints) {
		ap := a.Proxy.Entrypoints[n]
		if bp, ok := b.Proxy.Entrypoints[n]; !ok {
			out = append(out, fmt.Sprintf("%s needs proxy entrypoint %q (port %d); starting %s reconfigures the proxy without it", aName, n, ap, bName))
		} else if bp != ap {
			out = append(out, fmt.Sprintf("entrypoint %q is port %d for %s and %d for %s; the proxy can only have one", n, ap, aName, bp, bName))
		}
	}
	for _, n := range sortedEntrypoints(b.Proxy.Entrypoints) {
		if _, ok := a.Proxy.Entrypoints[n]; !ok {
			out = append(out, fmt.Sprintf("%s needs proxy entrypoint %q (port %d); starting %s reconfigures the proxy without it", bName, n, b.Proxy.Entrypoints[n], aName))
		}
	}
	if x, y := a.BindAddress(), b.BindAddress(); x != y {
		out = append(out, fmt.Sprintf("%s publishes the proxy on %s and %s on %s; starting either one moves it", aName, x, bName, y))
	}
	return out
}

func sortedEntrypoints(m map[string]int) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Part is what one repository says about itself, in its own file: the apps it
// runs and the proxy entrypoints they need. The multiverse that lists the
// repository only has to say where it is.
type Part struct {
	Apps        map[string]App
	Entrypoints map[string]int
}

// Merge adds what a repository says about itself. The apps it declares replace
// the one the multiverse would have derived for it; they cannot take the name
// of another repository's app, and an entrypoint another part or the manifest
// already has must be on the same port.
func (m *Manifest) Merge(repoKey string, p Part) error {
	if _, ok := m.Repos[repoKey]; !ok {
		return fmt.Errorf("no repo %q to merge into", repoKey)
	}
	var errs []string
	for key, a := range m.Apps {
		if a.Repo == repoKey && a.Derived && len(p.Apps) > 0 {
			delete(m.Apps, key)
		}
	}
	for key, a := range p.Apps {
		if other, taken := m.Apps[key]; taken {
			errs = append(errs, fmt.Sprintf("app %q is already defined for repo %q", key, other.Repo))
			continue
		}
		a.Repo = repoKey
		m.Apps[key] = a
	}
	for name, port := range p.Entrypoints {
		if have, ok := m.Proxy.Entrypoints[name]; ok && have != port {
			errs = append(errs, fmt.Sprintf("proxy entrypoint %q is %d here and %d elsewhere", name, port, have))
			continue
		}
		m.Proxy.Entrypoints[name] = port
	}
	if len(errs) > 0 {
		sort.Strings(errs)
		return &ValidationError{Problems: errs}
	}
	return m.Validate()
}
