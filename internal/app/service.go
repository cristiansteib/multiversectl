package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// Deps are the ports a Service needs. Adapters are wired in main.
type Deps struct {
	Git     Git
	Runtime Runtime
	// Commands runs a hook's command on the machine; nil refuses them.
	Commands  Commands
	Proxy     Proxy
	Files     Files
	System    System
	Store     Store
	Config    ConfigStore
	Manifests ManifestLoader
	Releases  Releases
	Updates   UpdateStore
	Router    Router
	Out       Output
	// Progress draws slow work; nil runs it without drawing.
	Progress Progress
	// Hosts are the services a repository may be on, asked about a branch's
	// pull or merge request. Empty leaves those unanswered rather than failing:
	// nothing else a universe shows depends on reaching a network.
	Hosts []RepoHost
	// Locks keeps this process out of the way of another one working on the
	// same multiverse. Nil runs without them, which is how the tests run.
	Locks Locks
	// Verbosity is how much of what is being done to say: 1 the steps, 3 the
	// detail behind them. The commands run are the adapters' to say, from 2.
	Verbosity int
}

// Service carries out the use cases against one multiverse.
type Service struct {
	Deps
	Multiverse string
	Manifest   *domain.Manifest
	Version    string
	// Install is the name this multiverse is installed under, "" when unknown
	// (a manifest run with -f) or when it is the manifest's own.
	Install string
}

// New builds a Service for a multiverse whose manifest is already parsed.
func New(deps Deps, multiverse string, manifest *domain.Manifest, version string) *Service {
	s := &Service{Deps: deps, Multiverse: multiverse, Manifest: manifest, Version: version}
	s.Install = s.installName()
	return s
}

// installName is the name the machine's config records for this root, when it
// differs from the manifest's own name.
func (s *Service) installName() string {
	if s.Config == nil || s.Manifest == nil || s.Multiverse == "" {
		return ""
	}
	cfg, err := s.Config.Load()
	if err != nil {
		return ""
	}
	for name, e := range cfg.Multiverses {
		if e.Root != "" && filepath.Clean(e.Root) == filepath.Clean(s.Multiverse) && name != s.Manifest.Name {
			return name
		}
	}
	return ""
}

// Universe reconstructs the domain object for a universe's name.
func (s *Service) Universe(name string) domain.Universe {
	return domain.Universe{Name: name, Multiverse: s.Multiverse, Manifest: s.Manifest, Install: s.Install}
}

// Resolve finds the universe a command's argument names. The argument may be
// the name as typed, the directory it lives in, or the slug that appears in
// its hostnames, because all three are in front of the user at some point.
func (s *Service) Resolve(arg string) (domain.UniverseState, error) {
	if err := domain.ValidateUniverseName(arg); err != nil {
		return domain.UniverseState{}, err
	}
	id := domain.CanonicalUniverseName(arg)
	st, err := s.Store.Load(id)
	if err == nil {
		return st, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return st, err
	}
	all, listErr := s.Store.List()
	if listErr != nil {
		return st, err
	}
	for _, candidate := range all {
		if strings.EqualFold(arg, candidate.Name) ||
			strings.EqualFold(arg, candidate.ID) ||
			strings.EqualFold(arg, candidate.Slug) {
			return candidate, nil
		}
	}
	if len(all) == 0 {
		return st, fmt.Errorf("universe %q: %w — there are none yet; create one with: `multiversectl universe new %s`",
			arg, domain.ErrNotFound, arg)
	}
	names := make([]string, 0, len(all))
	for _, candidate := range all {
		names = append(names, candidate.Name)
	}
	return st, fmt.Errorf("universe %q: %w — these exist: %s",
		arg, domain.ErrNotFound, strings.Join(names, ", "))
}

// ResolveDomain turns a manifest domain of "auto" (or "none") into one that suits how the
// multiverse is exposed: a public wildcard name resolving to this machine when
// another device has to reach it, and .localhost when nothing should.
func (s *Service) ResolveDomain(ctx context.Context) (string, error) {
	if s.Manifest.IPOnly() {
		// Only the proxy's routing rules carry it, and nothing resolves it:
		// addresses are the machine's own, with a port per host.
		return "localhost", nil
	}
	if s.Manifest.Domain != "auto" {
		return s.Manifest.Domain, nil
	}
	if s.Manifest.LocalOnly() {
		// Every browser resolves *.localhost to the loopback address itself,
		// so no name and no address is ever looked up anywhere.
		return "localhost", nil
	}
	ip, err := s.System.LANIP(ctx)
	if err != nil {
		return "", fmt.Errorf("deriving the domain: %w", err)
	}
	return strings.ReplaceAll(ip, ".", "-") + ".traefik.me", nil
}

// runtimeEnv is everything an overlay may reference: the universe's identity,
// one variable per hostname, one per checkout, plus the manifest's own env.
func (s *Service) runtimeEnv(u domain.Universe, st domain.UniverseState) map[string]string {
	env := map[string]string{
		// Images that bake in a user id need these; providing them always
		// keeps that out of every manifest. A manifest env entry still wins.
		"HOST_UID":             strconv.Itoa(s.System.UID()),
		"HOST_GID":             strconv.Itoa(s.System.GID()),
		"COMPOSE_PROJECT_NAME": st.Project,
		// UNIVERSE is what the universe is called; the slug is the part a
		// hostname or a multiverse name can carry.
		"UNIVERSE":          u.Name,
		"UNIVERSE_SLUG":     u.Slug(),
		"UNIVERSE_DIR":      u.Dir(),
		"MULTIVERSE_DOMAIN": st.Domain,
		// Where the multiverse is, for a script or an agent that has only the
		// environment to go on.
		"MULTIVERSE_ROOT":    s.Multiverse,
		"MULTIVERSE_PROJECT": s.Manifest.Name,
	}
	for hostKey, hostname := range st.Hosts {
		env[domain.EnvVarName(hostKey)] = s.hostOf(st, hostKey, hostname)
		env[domain.EnvVarURL(hostKey)] = s.originOf(st, hostKey, hostname)
		if forward := st.Rules[hostKey].ForwardURL; forward != "" {
			env[domain.EnvVarForwardURL(hostKey)] = forward
		}
	}
	// The whole set, in the two shapes a framework asks for. An app that
	// trusts origins or allows hosts can then name these instead of listing
	// hostnames it has to keep in step with the manifest by hand.
	hosts, origins := s.universeAddresses(st)
	env["MULTIVERSE_HOSTS"] = strings.Join(hosts, ",")
	env["MULTIVERSE_ORIGINS"] = strings.Join(origins, ",")
	for repoKey := range s.Manifest.Repos {
		env[strings.ToUpper(repoKey)+"_DIR"] = u.RepoDir(repoKey)
	}
	for key, value := range s.Manifest.Env {
		env[key] = s.expand(value)
	}
	return env
}

// appEnv is runtimeEnv with what the app sets on top of the multiverse.
func (s *Service) appEnv(u domain.Universe, st domain.UniverseState, appKey string) map[string]string {
	env := s.runtimeEnv(u, st)
	for key, value := range s.Manifest.Apps[appKey].Env {
		env[key] = s.expand(value)
	}
	return env
}

// baseBranch is what a repository's universe branches start from and are
// measured against: the multiverse's word for it, or the remote's default.
func (s *Service) baseBranch(ctx context.Context, repoKey, clone string) (string, error) {
	if base := s.Manifest.Repos[repoKey].Base; base != "" {
		return base, nil
	}
	return s.Git.DefaultBranch(ctx, clone)
}

// universeAddresses are every hostname of a universe and every one as an
// origin, both in a stable order so a generated value does not churn.
func (s *Service) universeAddresses(st domain.UniverseState) (hosts, origins []string) {
	for _, hostKey := range mapKeys(st.Hosts) {
		if host := s.hostOf(st, hostKey, st.Hosts[hostKey]); !slices.Contains(hosts, host) {
			hosts = append(hosts, host)
		}
		origins = appendNew(origins, s.originOf(st, hostKey, st.Hosts[hostKey]))
		// "Every address" includes the one by IP and port: a client that uses it
		// is refused by an app that trusts only the names. One whose port is not
		// chosen yet has no such address, and is left out.
		if forward := st.Rules[hostKey].ForwardURL; forward != "" {
			origins = appendNew(origins, forward)
		}
	}
	return hosts, origins
}

func appendNew(list []string, item string) []string {
	if slices.Contains(list, item) {
		return list
	}
	return append(list, item)
}

// hostOf is the name a host answers to as far as anything outside the proxy is
// concerned: its hostname, or with no domain the machine's address.
func (s *Service) hostOf(st domain.UniverseState, hostKey, hostname string) string {
	if rule := st.Rules[hostKey]; byIP(rule) {
		if u, err := url.Parse(rule.ForwardURL); err == nil && u.Hostname() != "" {
			return u.Hostname()
		}
	}
	return hostname
}

// byIP says whether a host is given by its forwarded address and no name: it
// asked for that (portForward: only), or the multiverse has no domain. Until
// the first start there is no forwarded address, and the name stands in.
func byIP(rule domain.RouteRule) bool {
	return rule.PortForward == domain.PortForwardOnly && rule.ForwardURL != ""
}

// originOf is a hostname as a browser would send it in an Origin header: with
// the port of the entrypoint that answers it, which is not the default port
// for anything on a second entrypoint.
func (s *Service) originOf(st domain.UniverseState, hostKey, hostname string) string {
	if rule := st.Rules[hostKey]; byIP(rule) {
		return rule.ForwardURL
	}
	return s.Manifest.Origin(st.Rules[hostKey].Entrypoint, hostname)
}

// expand resolves ${VAR} from the environment and the {{uid}} / {{gid}}
// tokens, which is all a manifest needs and nothing more.
func (s *Service) expand(value string) string {
	value = strings.ReplaceAll(value, "{{uid}}", strconv.Itoa(s.System.UID()))
	value = strings.ReplaceAll(value, "{{gid}}", strconv.Itoa(s.System.GID()))
	return os.Expand(value, s.System.Getenv)
}

// composeFiles is the file list for an app: the ones that exist on disk, plus
// the manifest's inline fragment written out so the runtime can read it like
// any other file.
func (s *Service) composeFiles(u domain.Universe, appKey string) ([]string, error) {
	app := s.Manifest.Apps[appKey]
	candidates := s.Manifest.ComposeFiles(appKey, u.RepoDir(app.Repo))

	var out []string
	repoDir := u.RepoDir(app.Repo)
	ownFile := false // the repository's own compose file: one is run, not each
	for _, f := range candidates {
		if !s.Files.Exists(f) {
			continue
		}
		if len(app.Compose) == 0 && filepath.Dir(f) == repoDir && slices.Contains(domain.ComposeNames, filepath.Base(f)) {
			if ownFile {
				continue
			}
			ownFile = true
		}
		out = append(out, f)
	}
	if len(app.Overlay) > 0 {
		path := filepath.Join(u.Dir(), ".multiverse-overlay-"+appKey+".yml")
		header := []byte("# generated by multiversectl from the manifest's overlay: for " + appKey + "\n")
		if err := s.Files.WriteFile(path, append(header, app.Overlay...), 0o644); err != nil {
			return nil, err
		}
		out = append(out, path)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("app %q: nothing to run; declare compose files, an overlay, or add %s",
			appKey, strings.Join(candidates, " or "))
	}
	return out, nil
}

// NothingToRun reports an app that was derived from a repository which turns
// out to have no compose file. Declaring an app and giving it nothing to run
// is a mistake worth an error; a repository the tool merely offered to run is
// not, so every command that would start or inspect it skips it instead.
func (s *Service) NothingToRun(u domain.Universe, appKey string) bool {
	if !s.Manifest.Apps[appKey].Derived {
		return false
	}
	_, err := s.composeFiles(u, appKey)
	return err != nil
}

// request assembles a runtime invocation for one app of one universe.
func (s *Service) request(u domain.Universe, st domain.UniverseState, appKey string, extra []string, extraFiles ...string) RuntimeRequest {
	app := s.Manifest.Apps[appKey]
	repoDir := u.RepoDir(app.Repo)
	services := append([]string{}, app.Services...)
	profiles := append([]string{}, app.Profiles...)
	for _, name := range extra {
		if optional, ok := app.Optional[name]; ok {
			services = append(services, optional...)
			profiles = append(profiles, name)
		}
	}
	files, err := s.composeFiles(u, appKey)
	if err != nil {
		// request cannot fail; starting reports the real error.
		files = s.Manifest.ComposeFiles(appKey, repoDir)
	}
	return RuntimeRequest{
		Project:  u.ProjectFor(appKey),
		Files:    append(files, extraFiles...),
		Profiles: profiles,
		Services: services,
		Env:      s.appEnv(u, st, appKey),
		WorkDir:  repoDir,
	}
}

// appsToAct returns the apps a command should act on: the named ones, or all
// runnable ones, rejecting anything the manifest does not declare.
func (s *Service) appsToAct(named []string) ([]string, error) {
	if len(named) == 0 {
		return s.Manifest.RunnableApps(), nil
	}
	for _, key := range named {
		if _, ok := s.Manifest.Apps[key]; !ok {
			return nil, fmt.Errorf("app %q: %w in the manifest", key, domain.ErrNotFound)
		}
	}
	return named, nil
}

// appsToStart is appsToAct for a start: with none named, the apps on demand are
// left out, and the one line says how to start them. Naming one starts it.
func (s *Service) appsToStart(universe string, named []string) ([]string, error) {
	if len(named) > 0 {
		return s.appsToAct(named)
	}
	all, err := s.appsToAct(nil)
	if err != nil {
		return nil, err
	}
	started := s.Manifest.AutostartApps()
	for _, key := range all {
		if s.Manifest.Apps[key].OnDemand {
			s.Out.Info("%s: on demand, not started (start it with: multiversectl universe up %s %s)", key, universe, key)
		}
	}
	return started, nil
}

// withoutIdleOnDemand drops the apps on demand that nothing of is running, for
// the commands that look after what runs when no app was named: there is nothing
// to stop, remove the containers of or run a hook in. When the containers cannot
// be listed every app stays, since leaving something running is the worse error.
func (s *Service) withoutIdleOnDemand(ctx context.Context, st domain.UniverseState, keys []string) []string {
	hasOnDemand := false
	for _, key := range keys {
		hasOnDemand = hasOnDemand || s.Manifest.Apps[key].OnDemand
	}
	if !hasOnDemand {
		return keys
	}
	containers, err := s.Runtime.Containers(ctx, st.Project)
	if err != nil {
		return keys
	}
	u := s.Universe(st.Name)
	var out []string
	for _, key := range keys {
		if s.Manifest.Apps[key].OnDemand && runningFor(containers, u.ProjectFor(key)) == 0 {
			continue
		}
		out = append(out, key)
	}
	return out
}

// Routes are the resolved addresses of one app of one universe, as stored when
// it was last created or started.
func (s *Service) Routes(st domain.UniverseState, appKey string) []Route {
	network := s.Manifest.Proxy.Network
	if network == "" {
		network = "multiversenet"
	}
	keys := make([]string, 0, len(st.Rules))
	for hostKey := range st.Rules {
		keys = append(keys, hostKey)
	}
	sort.Strings(keys)

	routes := make([]Route, 0, len(keys))
	for _, hostKey := range keys {
		rule := st.Rules[hostKey]
		if rule.App != appKey {
			continue
		}
		var aliases []string
		for _, other := range rule.Aliases {
			if alias, ok := st.Rules[other]; ok {
				aliases = append(aliases, alias.Hostname)
			}
		}
		routes = append(routes, Route{
			Name:       hostKey + "-" + st.Slug,
			Hostname:   rule.Hostname,
			Aliases:    aliases,
			Service:    rule.Service,
			Port:       rule.Port,
			Entrypoint: rule.Entrypoint,
			Project:    st.Project,
			Network:    network,
			HostPort:   rule.HostPort,
			ForwardURL: rule.ForwardURL,
			ByIP:       byIP(rule),
			Scheme:     rule.Scheme,
			Ready:      rule.Ready,
			Probe:      rule.Probe,
		})
	}
	return routes
}

// SecretPath is one secret file of an app: its name in the repository, where it
// is kept in the clone, where it goes in the universe, and the file a missing
// one is made from (empty when none is).
type SecretPath struct{ Name, Src, Dst, Template string }

// SecretPaths lists an app's secret files for a universe.
func (s *Service) SecretPaths(u domain.Universe, appKey string) []SecretPath {
	app := s.Manifest.Apps[appKey]
	clone := s.Manifest.CloneDir(s.Multiverse, app.Repo)
	var out []SecretPath
	for _, name := range app.Secrets {
		p := SecretPath{Name: name, Src: filepath.Join(clone, name), Dst: filepath.Join(u.RepoDir(app.Repo), name)}
		// Set and empty means none; not set means the convention.
		if tpl, set := app.SecretTemplates[name]; !set {
			p.Template = p.Src + ".example"
		} else if tpl != "" {
			p.Template = filepath.Join(clone, tpl)
		}
		out = append(out, p)
	}
	return out
}

// say tells what is being done, for a person who asked to be told with -v. It
// is for the steps a command is made of, not for its result, which the command
// reports whatever was asked.
func (s *Service) say(level int, format string, a ...any) {
	if s.Verbosity >= level {
		s.Out.Plain("  · "+format, a...)
	}
}
