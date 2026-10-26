package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// ListOptions says how much of a universe the caller wants described. Service
// states and volumes cost a runtime call per app, so a plain listing does
// without them.
type ListOptions struct {
	Services bool
	// Freshness asks how each checkout stands against the branch it follows:
	// a few git calls per repository, so only for the views that show it.
	Freshness bool
}

// Summary is one universe as `list` shows it.
type Summary struct {
	Name     string
	Slug     string
	Project  string
	Domain   string
	Dir      string
	Branches map[string]string
	Hosts    map[string]string
	Running  int
	Apps     []AppSummary
	// Checkouts is every repository of the universe, including those with no
	// app: a worktree with nothing to run is still a directory the user edits.
	Checkouts []Checkout
	// Metadata nothing else records: when it appeared, when it last ran and
	// why it exists.
	Created     time.Time
	LastStarted time.Time
	Note        string
	PR          string
}

// Checkout is one repository's worktree inside a universe.
type Checkout struct {
	Repo   string
	Branch string
	Path   string
	App    string // the app that runs it; empty when it has none
	// Freshness is set only when the listing asked for it.
	Freshness *Freshness
}

// AppSummary is one app of one universe: where its code is, how to reach it
// and — when asked for — every service it is made of.
type AppSummary struct {
	Key     string
	Repo    string
	Branch  string
	Path    string // its repository's worktree in this universe
	Project string // the app's own compose project, e.g. acme-u3-api
	Running int
	URLs    []HostURL

	// Expected is how many services should be up once the app is started, so
	// a listing can say 2/3 rather than 2. Zero unless services were asked
	// for.
	Expected int
	Services []ServiceStatus
	Volumes  []string
}

// HostURL pairs a host rule with the service behind it and the address it is
// reached on.
type HostURL struct {
	Host    string
	Service string
	URL     string
}

// ServiceState is where one service of an app stands, including the two cases
// no container can report: defined but never started, and deliberately not
// part of this app.
type ServiceState string

const (
	StateRunning  ServiceState = "running"
	StateStopped  ServiceState = "stopped"
	StateNotUp    ServiceState = "not started"
	StateOptional ServiceState = "optional"
	StateOff      ServiceState = "off"
)

// ServiceStatus is one service of one app as a wide listing shows it.
type ServiceStatus struct {
	Name string
	// State is the single word a reader scans for; Detail is the runtime's own
	// words behind it, e.g. "Up 12 hours" or the flag that would start it.
	State     ServiceState
	Detail    string
	Published string // host ports it publishes; empty is the healthy case
	// Container is the name of the container the service runs in, for docker
	// commands that exec does not cover (logs, cp); empty when it has none.
	Container string
}

// Up reports whether the service is one of those a started app should run.
func (s ServiceStatus) Up() bool { return s.State == StateRunning }

// Counted reports whether the service belongs in the app's running total.
// Optional and excluded services do not: they are off on purpose.
func (s ServiceStatus) Counted() bool {
	return s.State == StateRunning || s.State == StateStopped || s.State == StateNotUp
}

// List reports every known universe with how many of its containers run.
func (s *Service) List(ctx context.Context, opts ListOptions) ([]Summary, error) {
	states, err := s.Store.List()
	if err != nil {
		return nil, fmt.Errorf("listing universes: %w", err)
	}
	out := make([]Summary, 0, len(states))
	for _, st := range states {
		containers, err := s.Runtime.Containers(ctx, st.Project)
		if err != nil {
			s.Out.Warn("universe %s: containers: %v", st.Name, err)
		}
		var volumes []Volume
		if opts.Services {
			// One query per universe: every app's project shares its prefix.
			volumes, err = s.Runtime.Volumes(ctx, st.Project+"-")
			if err != nil {
				s.Out.Warn("universe %s: volumes: %v", st.Name, err)
			}
		}
		out = append(out, Summary{
			Name:        st.Name,
			Slug:        st.Slug,
			Project:     st.Project,
			Domain:      st.Domain,
			Dir:         s.Universe(st.Name).Dir(),
			Branches:    st.Branches,
			Hosts:       st.Hosts,
			Running:     countRunning(containers),
			Apps:        s.appSummaries(ctx, st, containers, volumes, opts),
			Checkouts:   s.withFreshness(ctx, st, s.checkouts(st), opts),
			Created:     st.Created,
			LastStarted: st.LastStarted,
			Note:        st.Note,
			PR:          st.PR,
		})
	}
	return out, nil
}

// routeURL is where a route answers, with the entrypoint port when it is not
// the default.
func (s *Service) routeURL(route Route) string {
	port := s.Manifest.Proxy.Entrypoints[route.Entrypoint]
	if port != 0 && port != 80 {
		return fmt.Sprintf("http://%s:%d", route.Hostname, port)
	}
	return "http://" + route.Hostname
}

// appSummaries describes each app of a universe, with the URLs its host rules
// resolve to — including the entrypoint port when it is not the default.
func (s *Service) appSummaries(ctx context.Context, st domain.UniverseState, containers []Container, volumes []Volume, opts ListOptions) []AppSummary {
	if s.Manifest == nil {
		return nil
	}
	u := s.Universe(st.Name)

	out := make([]AppSummary, 0, len(s.Manifest.Apps))
	for _, key := range mapKeys(s.Manifest.Apps) {
		app := s.Manifest.Apps[key]
		project := u.ProjectFor(key)
		summary := AppSummary{
			Key:     key,
			Repo:    app.Repo,
			Branch:  st.Branches[app.Repo],
			Path:    u.RepoDir(app.Repo),
			Project: project,
			Running: runningFor(containers, project),
		}
		for _, route := range s.Routes(st, key) {
			summary.URLs = append(summary.URLs, HostURL{
				Host:    strings.TrimSuffix(route.Name, "-"+u.Slug()),
				Service: route.Service,
				URL:     s.routeURL(route),
			})
			if route.ForwardURL != "" {
				summary.URLs = append(summary.URLs, HostURL{
					Host:    strings.TrimSuffix(route.Name, "-"+u.Slug()) + ":forward",
					Service: route.Service,
					URL:     route.ForwardURL,
				})
			}
		}
		if opts.Services {
			summary.Services = s.serviceStates(ctx, u, st, key, containers)
			for _, svc := range summary.Services {
				if svc.Counted() {
					summary.Expected++
				}
			}
			summary.Volumes = volumesOf(volumes, project)
		}
		out = append(out, summary)
	}
	return out
}

// checkouts lists every repository the universe has on disk: those the
// manifest declares, and any the state remembers from a manifest that has
// since changed — a directory that exists is worth naming either way.
func (s *Service) checkouts(st domain.UniverseState) []Checkout {
	u := s.Universe(st.Name)
	owner := map[string]string{}
	if s.Manifest != nil {
		for _, key := range mapKeys(s.Manifest.Apps) {
			repo := s.Manifest.Apps[key].Repo
			if owner[repo] == "" {
				owner[repo] = key
			}
		}
	}
	repos := map[string]bool{}
	if s.Manifest != nil {
		for repo := range s.Manifest.Repos {
			repos[repo] = true
		}
	}
	for repo := range st.Branches {
		repos[repo] = true
	}
	out := make([]Checkout, 0, len(repos))
	for _, repo := range mapKeys(repos) {
		out = append(out, Checkout{
			Repo:   repo,
			Branch: st.Branches[repo],
			Path:   u.RepoDir(repo),
			App:    owner[repo],
		})
	}
	return out
}

// serviceStates describes every service an app's compose files define, not
// only the ones with a container: what is off is as much of an answer as what
// is on. Optional groups are all activated so their services get a row too,
// marked with the flag that would start them.
func (s *Service) serviceStates(ctx context.Context, u domain.Universe, st domain.UniverseState, appKey string, containers []Container) []ServiceStatus {
	app := s.Manifest.Apps[appKey]
	project := u.ProjectFor(appKey)
	found := map[string]Container{}
	for _, c := range containers {
		if c.Project == project && c.Service != "" {
			found[c.Service] = c
		}
	}

	groups := mapKeys(app.Optional)
	names := s.definedServices(ctx, u, st, appKey, groups)
	if len(names) == 0 {
		// The compose files could not be read; the containers that exist are
		// still the truth about what is running.
		names = mapKeys(found)
	}

	selected := map[string]bool{}
	for _, name := range app.Services {
		selected[name] = true
	}
	group := map[string]string{}
	for _, name := range groups {
		for _, svc := range app.Optional[name] {
			group[svc] = name
		}
	}

	out := make([]ServiceStatus, 0, len(names))
	for _, name := range names {
		status := ServiceStatus{Name: name}
		c, exists := found[name]
		switch {
		case exists && isRunning(c):
			status.State, status.Detail, status.Published, status.Container = StateRunning, c.Status, c.Published, c.Name
		case exists:
			status.State, status.Detail, status.Container = StateStopped, c.Status, c.Name
		case group[name] != "":
			status.State, status.Detail = StateOptional, "--with "+group[name]
		case len(app.Services) > 0 && !selected[name]:
			status.State, status.Detail = StateOff, "not in this app's services"
		default:
			status.State = StateNotUp
		}
		out = append(out, status)
	}
	return out
}

// definedServices asks the runtime which services the app's files define,
// with every optional group active so a profile hides nothing.
func (s *Service) definedServices(ctx context.Context, u domain.Universe, st domain.UniverseState, appKey string, groups []string) []string {
	if s.Manifest.Repos[s.Manifest.Apps[appKey].Repo].Runtime != domain.RuntimeCompose {
		return nil
	}
	if s.NothingToRun(u, appKey) {
		return nil
	}
	cfg, err := s.Runtime.Inspect(ctx, s.request(u, st, appKey, groups))
	if err != nil {
		// The reason is long and the fallback is reasonable, so say only what
		// the user can act on.
		s.Out.Warn("app %s: its compose files could not be read; listing only what has a container", appKey)
		return nil
	}
	return cfg.Names()
}

// volumesOf are the volumes belonging to one app's project, named in full:
// the project prefix is the evidence that the data is this universe's own.
func volumesOf(all []Volume, project string) []string {
	var out []string
	for _, v := range all {
		if v.Project == project {
			out = append(out, v.Name)
		}
	}
	sort.Strings(out)
	return out
}

func mapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func countRunning(containers []Container) int {
	n := 0
	for _, c := range containers {
		if isRunning(c) {
			n++
		}
	}
	return n
}

// isRunning accepts both the "Up 3 minutes" a listing prints and the
// "running" a structured query reports.
func isRunning(c Container) bool {
	status := strings.ToLower(c.Status)
	return strings.HasPrefix(status, "up") || status == "running"
}
