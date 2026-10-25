package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// Deps are the ports a Service needs. Adapters are wired in main.
type Deps struct {
	Git       Git
	Runtime   Runtime
	Proxy     Proxy
	Files     Files
	System    System
	Store     Store
	Config    ConfigStore
	Manifests ManifestLoader
	Releases  Releases
	Router    Router
	Out       Output
	// Progress draws slow work; nil runs it without drawing.
	Progress Progress
}

// Service carries out the use cases against one project.
type Service struct {
	Deps
	Workspace string
	Manifest  *domain.Manifest
	Version   string
}

// New builds a Service for a project whose manifest is already parsed.
func New(deps Deps, workspace string, manifest *domain.Manifest, version string) *Service {
	return &Service{Deps: deps, Workspace: workspace, Manifest: manifest, Version: version}
}

// Universe reconstructs the domain object for a universe's name.
func (s *Service) Universe(name string) domain.Universe {
	return domain.Universe{Name: name, Workspace: s.Workspace, Manifest: s.Manifest}
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

// ResolveDomain turns a manifest domain of "auto" into one that suits how the
// project is exposed: a public wildcard name resolving to this machine when
// another device has to reach it, and .localhost when nothing should.
func (s *Service) ResolveDomain(ctx context.Context) (string, error) {
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
		// hostname or a project name can carry.
		"UNIVERSE":          u.Name,
		"UNIVERSE_SLUG":     u.Slug(),
		"UNIVERSE_DIR":      u.Dir(),
		"MULTIVERSE_DOMAIN": st.Domain,
	}
	for hostKey, hostname := range st.Hosts {
		env[domain.EnvVarName(hostKey)] = hostname
		env[domain.EnvVarURL(hostKey)] = s.originOf(st, hostKey, hostname)
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

// universeAddresses are every hostname of a universe and every one as an
// origin, both in a stable order so a generated value does not churn.
func (s *Service) universeAddresses(st domain.UniverseState) (hosts, origins []string) {
	for _, hostKey := range mapKeys(st.Hosts) {
		hosts = append(hosts, st.Hosts[hostKey])
		origins = append(origins, s.originOf(st, hostKey, st.Hosts[hostKey]))
	}
	return hosts, origins
}

// originOf is a hostname as a browser would send it in an Origin header: with
// the port of the entrypoint that answers it, which is not the default port
// for anything on a second entrypoint.
func (s *Service) originOf(st domain.UniverseState, hostKey, hostname string) string {
	port := s.Manifest.Proxy.Entrypoints[st.Rules[hostKey].Entrypoint]
	if port != 0 && port != 80 {
		return fmt.Sprintf("http://%s:%d", hostname, port)
	}
	return "http://" + hostname
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
	for _, f := range candidates {
		if s.Files.Exists(f) {
			out = append(out, f)
		}
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
		Env:      s.runtimeEnv(u, st),
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
		})
	}
	return routes
}

// SecretPaths lists an app's secret files as (source in the clone, destination
// in the universe) pairs.
func (s *Service) SecretPaths(u domain.Universe, appKey string) [][2]string {
	app := s.Manifest.Apps[appKey]
	clone := s.Manifest.CloneDir(s.Workspace, app.Repo)
	var out [][2]string
	for _, name := range app.Secrets {
		out = append(out, [2]string{filepath.Join(clone, name), filepath.Join(u.RepoDir(app.Repo), name)})
	}
	return out
}
