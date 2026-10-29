package domain

import (
	"fmt"
	"io/fs"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
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

	// Hooks are run for the multiverse as a whole, around what its apps do, and
	// only on the machine: there is no one container to name.
	Hooks map[HookEvent][]Hook

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
	// TLS names the entrypoints a browser reaches over https.
	TLS map[string]bool
	// Certificates are what those entrypoints present; with none, the proxy's
	// own self-signed one, which a browser warns about once.
	Certificates []Certificate
}

// Certificate is a certificate and its key, as files on this machine.
type Certificate struct {
	CertFile string
	KeyFile  string
}

// Origin is how a browser addresses a hostname on an entrypoint: the scheme
// it is served with, and the port when it is not that scheme's own.
func (m *Manifest) Origin(entrypoint, hostname string) string {
	scheme, standard := "http", 80
	if m.Proxy.TLS[entrypoint] {
		scheme, standard = "https", 443
	}
	if port := m.Proxy.Entrypoints[entrypoint]; port != 0 && port != standard {
		return fmt.Sprintf("%s://%s:%d", scheme, hostname, port)
	}
	return scheme + "://" + hostname
}

// DomainNone is the domain that opts out of hostnames: a universe is reached by
// the machine's address and a port of its own, and no name is looked up.
const DomainNone = "none"

// IPOnly reports that a universe is reached by IP and port, with no hostname
// anyone is asked to resolve. The proxy still routes by an internal name
// (.localhost), but no address shown or handed to a service uses it.
func (m *Manifest) IPOnly() bool { return m.Domain == DomainNone }

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
	// Base is the branch a new universe's branch starts from and is measured
	// against; empty is the remote's default branch.
	Base string
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
	// Hooks are the commands run at points of the app's life in a universe.
	Hooks map[HookEvent][]Hook
	// Render lists the files made from the universe's values before the app starts.
	Render  []Render
	Overlay []byte // inline compose fragment, rendered by the manifest adapter
	Build   bool   // build images before starting
	Hosts   map[string]HostRule
	// Env is exported into this app's runtime invocations and hooks, over the
	// multiverse's own.
	Env map[string]string

	// OnDemand keeps the app out of a start that names no app: it is checked out
	// and has its addresses like any other, but only runs when asked for by name.
	// It is the negative so that the zero value, and every manifest written before
	// the setting existed, still starts everything.
	OnDemand bool
	// AutostartSet says the file said either way, which is what lets a
	// multiverse start an app its repository's own file leaves on demand.
	AutostartSet bool

	// Derived marks an app no one declared: one repository, everything by
	// convention. Such an app may turn out to have no compose file at all,
	// which is a repository with nothing to run rather than a mistake.
	Derived bool
}

// Render is a file made from a template and the universe's values: for an app
// that reads a config file at startup and cannot take environment variables.
type Render struct {
	// To is the file written, relative to the app's checkout in the universe.
	To string
	// Template is the text of the template; From names a file holding it instead,
	// relative to the checkout or, with a leading "@", to the manifest's directory.
	Template string
	From     string
	// Mode is the file's permission bits; 0 is DefaultRenderMode.
	Mode fs.FileMode
	// Overwrite lets the file replace one that git tracks. Without it that is an
	// error, so a template cannot silently clobber source.
	Overwrite bool
}

// DefaultRenderMode is the permission of a rendered file that names none.
const DefaultRenderMode fs.FileMode = 0o644

// HookEvent is a point in a universe's life a command can be tied to.
type HookEvent string

const (
	// PostCreate runs once the universe's worktrees exist, before anything starts.
	PostCreate HookEvent = "postCreate"
	// PreUp runs before an app is built and started: the place to prepare what
	// it reads, such as a file or an entitlement. A failure stops the start.
	PreUp HookEvent = "preUp"
	// PostUp runs once an app's services are up (for the multiverse's own hooks,
	// once every app is): the place to load data or register the universe's
	// address somewhere.
	PostUp HookEvent = "postUp"
	// PreDown runs before an app's containers are stopped.
	PreDown HookEvent = "preDown"
	// PostDown runs once they are.
	PostDown HookEvent = "postDown"
	// PreRemove runs before a universe is destroyed: the place to take back what
	// a hook put somewhere else.
	PreRemove HookEvent = "preRemove"
)

// HookEvents lists the events in the order they happen.
var HookEvents = []HookEvent{PostCreate, PreUp, PostUp, PreDown, PostDown, PreRemove}

// Valid says whether e is an event a hook can be tied to.
func (e HookEvent) Valid() bool { return slices.Contains(HookEvents, e) }

// InContainer says whether a command can be run in a service at this event: the
// containers have to be there, which they are once up and until they go down.
func (e HookEvent) InContainer() bool { return e == PostUp || e == PreDown }

// Failure says what a hook that does not succeed means.
type Failure string

const (
	// FailWarn says so and goes on: the universe is up, and refusing it for a
	// seed that did not load would be the worse of the two. The hooks after it
	// wait, since they may depend on it.
	FailWarn Failure = "warn"
	// FailStop ends the command with an error.
	FailStop Failure = "fail"
	// FailIgnore goes on as if it had worked, for a command that is only a nicety.
	FailIgnore Failure = "ignore"
)

// Hook is a command tied to an event: run in a service of the app's universe,
// or on the machine. The tool knows nothing of what it does; it gives it the
// universe's addresses and says how to run it again.
type Hook struct {
	Name    string // optional; says which one in the output
	Service string // container: empty is the app's only service, or its defaultService
	Command []string
	// Host runs the command on this machine instead of in a container.
	Host bool
	// Dir is where a host command runs, relative to the universe's directory:
	// "api" is that repository's checkout. Empty is the universe's directory.
	Dir string
	// Once runs it on a universe's first start only.
	Once bool
	// OnFailure is what a failure means; empty is FailStop for a preUp and
	// FailWarn for anything else, so a hook that breaks cannot keep a universe
	// from being stopped or removed.
	OnFailure Failure
	// RetryFor keeps trying for this long, because what it needs may still be
	// starting. Empty is a single try, except for a command in a container after
	// the app is up, which is tried for a few minutes.
	RetryFor time.Duration
	// Timeout ends one try that takes longer; empty never does.
	Timeout time.Duration
}

// Key names a hook within a universe, which is what remembers that a Once hook
// has run: the app, the event, where it runs, the service and the command, so
// reordering the hooks does not make one run again. A command in a container
// after the app is up, the one place hooks used to be, keeps the key it had.
func (h Hook) Key(app string, ev HookEvent) string {
	if ev == PostUp && !h.Host {
		return app + "|" + h.Service + "|" + strings.Join(h.Command, " ")
	}
	where := h.Service
	if h.Host {
		where = "host:" + h.Dir
	}
	return app + "|" + string(ev) + "|" + where + "|" + strings.Join(h.Command, " ")
}

// Policy is what a failure of h means at ev.
func (h Hook) Policy(ev HookEvent) Failure {
	switch {
	case h.OnFailure != "":
		return h.OnFailure
	case ev == PreUp:
		return FailStop
	}
	return FailWarn
}

// Retry is how long h is tried for at ev.
func (h Hook) Retry(ev HookEvent, fallback time.Duration) time.Duration {
	if h.RetryFor == 0 && ev == PostUp && !h.Host {
		return fallback
	}
	return h.RetryFor
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
	// Scheme is how the proxy talks to the service: "" or "http" (the default),
	// "https", or "h2c" for a service that speaks HTTP/2 without TLS, such as a
	// gRPC one. Without it the proxy forwards HTTP/1.1 and a gRPC server answers
	// with an error nothing in the universe explains.
	Scheme string
}

// Schemes are the ones the proxy can speak to a service.
var Schemes = []string{"http", "https", "h2c"}

// PortForwardAuto marks a resolved host as also published on a host port that
// the tool chooses: free when first chosen, then kept.
const PortForwardAuto = "auto"

// PortForwardOnly is auto without the hostname: the host is reached by IP and
// its port alone, and no address shown or handed to a service is a name.
const PortForwardOnly = "only"

// Forwarded says whether a portForward value publishes the host on a port.
func Forwarded(portForward string) bool {
	return portForward == PortForwardAuto || portForward == PortForwardOnly
}

var nameRe = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// ValidKey says whether s can name a repository, an app or a host: a host key
// becomes the first part of a hostname, so it is held to what DNS accepts.
func ValidKey(s string) bool { return nameRe.MatchString(s) }

// DNSLabelLimit is the longest one part of a hostname may be.
const DNSLabelLimit = 63

// ProxyKinds are the proxies a universe can be served through.
var ProxyKinds = []string{"traefik"}

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
		if strings.HasPrefix(repo.Base, "origin/") {
			// It is compared with origin/<base>, so the remote's name would be there twice.
			errs = append(errs, fmt.Sprintf("repos.%s.base: name the branch (%s), not the remote's", key, strings.TrimPrefix(repo.Base, "origin/")))
		}
	}
	if m.Proxy.Kind != "" && !slices.Contains(ProxyKinds, m.Proxy.Kind) {
		errs = append(errs, fmt.Sprintf("proxy.kind: %q is not supported (want %s)", m.Proxy.Kind, strings.Join(ProxyKinds, ", ")))
	}
	byPort := map[int]string{}
	for _, name := range sortedEntrypoints(m.Proxy.Entrypoints) {
		port := m.Proxy.Entrypoints[name]
		switch {
		case port <= 0 || port > 65535:
			errs = append(errs, fmt.Sprintf("proxy.entrypoints.%s.port: %d is out of range", name, port))
		case byPort[port] != "":
			errs = append(errs, fmt.Sprintf("proxy.entrypoints.%s.port: %d is already entrypoint %q; the proxy can listen on a port once", name, port, byPort[port]))
		default:
			byPort[port] = name
		}
	}
	for _, name := range slices.Sorted(maps.Keys(m.Proxy.TLS)) {
		if _, ok := m.Proxy.Entrypoints[name]; !ok {
			errs = append(errs, fmt.Sprintf("proxy.tls: no entrypoint %q", name))
		}
	}
	for i, c := range m.Proxy.Certificates {
		if !filepath.IsAbs(c.CertFile) || !filepath.IsAbs(c.KeyFile) {
			errs = append(errs, fmt.Sprintf("proxy.certificates[%d]: certFile and keyFile are both required", i))
		}
	}
	errs = append(errs, validateHooks("hooks", m.Hooks, nil, false)...)
	seenHost := map[string]string{}
	for key, app := range m.Apps {
		if !nameRe.MatchString(key) {
			errs = append(errs, fmt.Sprintf("apps.%s: invalid key", key))
		}
		if _, ok := m.Repos[app.Repo]; !ok {
			errs = append(errs, fmt.Sprintf("apps.%s.repo: no such repo %q", key, app.Repo))
		}
		for name, extra := range app.Optional {
			// A group with no services is a compose profile and nothing more. With
			// the app's services named, compose starts only those, so the profile's
			// would never run.
			if len(extra) == 0 && len(app.Services) > 0 {
				errs = append(errs, fmt.Sprintf("apps.%s.optional.%s.services: required when the app names its services, or the group starts nothing", key, name))
			}
		}
		if app.DefaultService != "" && len(app.Services) > 0 && !slices.Contains(app.Services, app.DefaultService) {
			errs = append(errs, fmt.Sprintf("apps.%s.defaultService: %q is not one of its services (%s)", key, app.DefaultService, strings.Join(app.Services, ", ")))
		}
		for _, name := range app.Secrets {
			// Copied out of the shared clone into the universe's checkout: a path
			// that left either would read or write files the multiverse does not own.
			if !filepath.IsLocal(name) {
				errs = append(errs, fmt.Sprintf("apps.%s.secrets: %q must be a path inside the repository", key, name))
			}
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
		errs = append(errs, validateHooks("apps."+key+".hooks", app.Hooks, app.Services, true)...)
		errs = append(errs, validateRender("apps."+key+".render", app.Render)...)
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
			if rule.PortForward != "" && !Forwarded(rule.PortForward) {
				errs = append(errs, fmt.Sprintf("apps.%s.hosts.%s.portForward: want %q or %q", key, hostKey, PortForwardAuto, PortForwardOnly))
			}
			if rule.Scheme != "" && !slices.Contains(Schemes, rule.Scheme) {
				errs = append(errs, fmt.Sprintf("apps.%s.hosts.%s.scheme: want %s", key, hostKey, strings.Join(Schemes, ", ")))
			}
			for _, alias := range rule.AlsoMatch {
				if alias == hostKey {
					errs = append(errs, fmt.Sprintf("apps.%s.hosts.%s.alsoMatch: a host cannot match itself", key, hostKey))
				}
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

// AutostartApps are the apps a start that names none brings up: every one but
// those marked on demand.
func (m *Manifest) AutostartApps() []string {
	var keys []string
	for _, key := range m.RunnableApps() {
		if !m.Apps[key].OnDemand {
			keys = append(keys, key)
		}
	}
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
		} else if a.Proxy.TLS[n] != b.Proxy.TLS[n] {
			out = append(out, fmt.Sprintf("entrypoint %q is served over TLS for one of %s and %s and not the other; the proxy can only do one", n, aName, bName))
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
	TLS         map[string]bool
}

// Merge adds what a repository says about itself. The apps it declares replace
// the one the multiverse would have derived for it; they cannot take the name
// of another repository's app, and an entrypoint another part or the manifest
// already has must be on the same port. An app the multiverse also declares for
// this repository is the repository's, with what the multiverse sets on top:
// the multiverse is what this machine runs, and the repository its default.
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
			if other.Repo != repoKey {
				errs = append(errs, fmt.Sprintf("app %q is already defined for repo %q", key, other.Repo))
				continue
			}
			a = Layer(a, other)
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
	for name, on := range p.TLS {
		if on {
			if m.Proxy.TLS == nil {
				m.Proxy.TLS = map[string]bool{}
			}
			m.Proxy.TLS[name] = true
		}
	}
	if len(errs) > 0 {
		sort.Strings(errs)
		return &ValidationError{Problems: errs}
	}
	return m.Validate()
}

// Layer is base with every setting top makes put over it. A list or map that top
// gives replaces base's whole, as a value it sets does: merging the entries of
// two lists of compose files or hosts would make an order neither file wrote.
// Env and secret templates are merged by key, since each key is a setting of
// its own.
func Layer(base, top App) App {
	out := base
	if len(top.Compose) > 0 {
		out.Compose = top.Compose
	}
	if len(top.Profiles) > 0 {
		out.Profiles = top.Profiles
	}
	if len(top.Services) > 0 {
		out.Services = top.Services
	}
	if len(top.Optional) > 0 {
		out.Optional = top.Optional
	}
	if len(top.Secrets) > 0 {
		out.Secrets = top.Secrets
	}
	if len(top.Publish) > 0 {
		out.Publish = top.Publish
	}
	if top.DefaultService != "" {
		out.DefaultService = top.DefaultService
	}
	if len(top.Hooks) > 0 {
		out.Hooks = top.Hooks
	}
	if len(top.Render) > 0 {
		out.Render = top.Render
	}
	if len(top.Overlay) > 0 {
		out.Overlay = top.Overlay
	}
	if len(top.Hosts) > 0 {
		out.Hosts = top.Hosts
	}
	if top.DefaultService == "" && len(top.Services) > 0 && !slices.Contains(out.Services, out.DefaultService) {
		// The repository's choice among services the multiverse no longer runs.
		out.DefaultService = ""
	}
	out.Build = base.Build || top.Build
	if top.AutostartSet {
		out.OnDemand, out.AutostartSet = top.OnDemand, true
	}
	out.SecretTemplates = maps.Clone(layerMap(base.SecretTemplates, top.SecretTemplates))
	for name := range out.SecretTemplates {
		if !slices.Contains(out.Secrets, name) {
			// A template the repository gave for a secret the multiverse dropped.
			delete(out.SecretTemplates, name)
		}
	}
	out.Env = layerMap(base.Env, top.Env)
	out.Derived = false
	return out
}

func layerMap(base, top map[string]string) map[string]string {
	if len(top) == 0 {
		return base
	}
	out := maps.Clone(base)
	if out == nil {
		out = map[string]string{}
	}
	maps.Copy(out, top)
	return out
}

// validateHooks reports what is wrong with the hooks under path. services are
// the ones the app names, empty when it leaves them to its compose files;
// inApp says whether a command may run in a container, which only an app has.
func validateHooks(path string, hooks map[HookEvent][]Hook, services []string, inApp bool) []string {
	var errs []string
	for _, ev := range slices.Sorted(maps.Keys(hooks)) {
		if !ev.Valid() {
			errs = append(errs, fmt.Sprintf("%s.%s: not an event (%s)", path, ev, JoinHookEvents()))
			continue
		}
		for i, hook := range hooks[ev] {
			at := fmt.Sprintf("%s.%s[%d]", path, ev, i)
			if len(hook.Command) == 0 {
				errs = append(errs, at+".command: required")
			}
			switch hook.OnFailure {
			case "", FailWarn, FailStop, FailIgnore:
			default:
				errs = append(errs, fmt.Sprintf("%s.onFailure: want %q, %q or %q", at, FailWarn, FailStop, FailIgnore))
			}
			if hook.RetryFor < 0 || hook.Timeout < 0 {
				errs = append(errs, at+": retryFor and timeout cannot be negative")
			}
			if hook.Host {
				if hook.Service != "" {
					errs = append(errs, at+": a host command runs in no service")
				}
				if hook.Dir != "" && !filepath.IsLocal(hook.Dir) {
					errs = append(errs, fmt.Sprintf("%s.host.dir: %q must be a path inside the universe", at, hook.Dir))
				}
				continue
			}
			if !inApp {
				errs = append(errs, at+": the multiverse's hooks run on the machine; give it `host`")
			} else if !ev.InContainer() {
				errs = append(errs, fmt.Sprintf("%s: no container is up at %s, so give it `host` (a command in a service is for postUp and preDown)", at, ev))
			}
			if hook.Service != "" && len(services) > 0 && !slices.Contains(services, hook.Service) {
				errs = append(errs, fmt.Sprintf("%s.container.service: %q is not one of its services (%s)", at, hook.Service, strings.Join(services, ", ")))
			}
		}
	}
	return errs
}

func JoinHookEvents() string {
	names := make([]string, len(HookEvents))
	for i, e := range HookEvents {
		names[i] = string(e)
	}
	return strings.Join(names, ", ")
}

// validateRender reports what is wrong with an app's render entries. Everything
// that decides where a file is written is checked here, before any universe
// exists, so a bad path never gets as far as a disk.
func validateRender(path string, entries []Render) []string {
	var errs []string
	seen := map[string]int{}
	for i, r := range entries {
		at := fmt.Sprintf("%s[%d]", path, i)
		switch {
		case r.To == "":
			errs = append(errs, at+".to: required")
		case !filepath.IsLocal(r.To):
			errs = append(errs, fmt.Sprintf("%s.to: %q must be a path inside the repository", at, r.To))
		default:
			clean := filepath.Clean(r.To)
			if clean == ".git" || strings.HasPrefix(clean, ".git"+string(filepath.Separator)) {
				errs = append(errs, fmt.Sprintf("%s.to: %q is git's own directory", at, r.To))
			}
			if prev, dup := seen[clean]; dup {
				errs = append(errs, fmt.Sprintf("%s.to: %q is already written by render[%d]", at, r.To, prev))
			}
			seen[clean] = i
		}
		switch {
		case r.Template != "" && r.From != "":
			errs = append(errs, at+": give template or from, not both")
		case r.Template == "" && r.From == "":
			errs = append(errs, at+": template or from: give one")
		case r.From != "" && !filepath.IsLocal(strings.TrimPrefix(r.From, "@")):
			errs = append(errs, fmt.Sprintf("%s.from: %q must be a path inside the repository, or @ and a path inside the manifest's directory", at, r.From))
		}
		if r.Mode&^fs.ModePerm != 0 {
			errs = append(errs, at+".mode: want permission bits such as 0644")
		}
	}
	return errs
}
