package server

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/cristiansteib/multiversectl/internal/app"
	"github.com/cristiansteib/multiversectl/internal/domain"
)

// Backend is what the server reads. It is read-only by construction: there is
// no method here that changes a catalog, a workspace or a universe.
type Backend interface {
	Status(ctx context.Context, fetch bool) (app.StatusReport, error)
	Catalog(ctx context.Context, fetch bool) (catalogView, error)
	// Fleet lists the universes of one installed workspace, or of all.
	Fleet(ctx context.Context, workspace string) (fleetView, error)
	Workspace(ctx context.Context, name string) (workspaceView, error)
	Doctor(ctx context.Context, workspace, universe string) (doctorView, error)
}

// Services is the Backend over the same use cases the command line runs.
type Services struct {
	Catalogs *app.Catalogs
	// mu keeps two requests from cloning the same catalog repository at once.
	mu      *sync.Mutex
	Machine *app.Service // knows the installed workspaces
	// At builds a service for one workspace. The output is where it warns, which
	// a page cannot show, so the server collects it per request.
	At func(out app.Output, root, manifest string) (*app.Service, error)
}

var _ Backend = Services{}

// NewServices wires the Backend.
func NewServices(c *app.Catalogs, machine *app.Service, at func(out app.Output, root, manifest string) (*app.Service, error)) Services {
	return Services{Catalogs: c, Machine: machine, At: at, mu: &sync.Mutex{}}
}

func (s Services) Status(ctx context.Context, fetch bool) (app.StatusReport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Catalogs.Status(ctx, fetch)
}

func (s Services) Catalog(ctx context.Context, fetch bool) (catalogView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tree, err := s.Catalogs.Tree(ctx, "", fetch)
	if err != nil {
		return catalogView{}, err
	}
	installed, err := s.Catalogs.InstalledIn(tree)
	if err != nil {
		return catalogView{}, err
	}
	return catalogView{Root: toNode(tree, installed), Installs: len(installed)}, nil
}

func toNode(n domain.Node, installed map[string]string) nodeView {
	v := nodeView{Name: n.Name, Path: n.Path, Kind: string(n.Kind), Repo: n.Source.Repo, Ref: n.Source.Ref,
		File: n.Source.Path, InstalledAs: installed[n.Path], Unavailable: n.Unavailable}
	for _, c := range n.Children {
		v.Children = append(v.Children, toNode(c, installed))
	}
	return v
}

func (s Services) infos(workspace string) ([]app.WorkspaceInfo, error) {
	if workspace != "" {
		w, err := s.Machine.WorkspaceNamed(workspace)
		if err != nil {
			return nil, err
		}
		return []app.WorkspaceInfo{w}, nil
	}
	return s.Machine.Workspaces()
}

// Fleet asks every workspace at once: each asks the container runtime about
// each of its universes, and the page would otherwise wait on them in turn.
func (s Services) Fleet(ctx context.Context, workspace string) (fleetView, error) {
	list, err := s.infos(workspace)
	if err != nil {
		return fleetView{}, err
	}
	out := make([]fleetWorkspace, len(list))
	var wg sync.WaitGroup
	for i, w := range list {
		wg.Add(1)
		go func(i int, w app.WorkspaceInfo) {
			defer wg.Done()
			out[i] = s.fleetOf(ctx, w)
		}(i, w)
	}
	wg.Wait()
	return fleetView{Workspaces: out}, nil
}

func (s Services) fleetOf(ctx context.Context, w app.WorkspaceInfo) fleetWorkspace {
	fw := fleetWorkspace{Name: w.Name, Current: w.Current, Root: w.Root, Universes: []universeView{}}
	warn := &collector{}
	svc, err := s.At(warn, w.Root, w.Manifest)
	if err != nil {
		fw.Error = err.Error()
		return fw
	}
	list, err := svc.List(ctx, app.ListOptions{Services: true})
	if err != nil {
		fw.Error = err.Error()
		return fw
	}
	for _, u := range list {
		fw.Universes = append(fw.Universes, toUniverse(u))
	}
	fw.Warnings = warn.all()
	return fw
}

func toUniverse(u app.Summary) universeView {
	v := universeView{Name: u.Name, Slug: u.Slug, Project: u.Project, Domain: u.Domain, Dir: u.Dir, Running: u.Running,
		Note: u.Note, Branches: u.Branches, Apps: []appView{}, Checkouts: []checkoutView{},
		Created: stamp(u.Created), LastStarted: stamp(u.LastStarted)}
	for _, a := range u.Apps {
		av := appView{Key: a.Key, Repo: a.Repo, Branch: a.Branch, Path: a.Path, Running: a.Running, Expected: a.Expected}
		for _, h := range a.URLs {
			av.URLs = append(av.URLs, urlView{Host: h.Host, Service: h.Service, URL: h.URL})
		}
		for _, sv := range a.Services {
			av.Services = append(av.Services, serviceView{Name: sv.Name, State: string(sv.State), Detail: sv.Detail, Published: sv.Published})
		}
		v.Apps = append(v.Apps, av)
	}
	for _, c := range u.Checkouts {
		v.Checkouts = append(v.Checkouts, checkoutView{Repo: c.Repo, Branch: c.Branch, Path: c.Path, App: c.App})
	}
	return v
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func (s Services) Workspace(ctx context.Context, name string) (workspaceView, error) {
	w, err := s.Machine.WorkspaceNamed(name)
	if err != nil {
		return workspaceView{}, err
	}
	svc, err := s.At(&collector{}, w.Root, w.Manifest)
	if err != nil {
		return workspaceView{}, err
	}
	m := svc.Manifest
	v := workspaceView{Name: w.Name, Current: w.Current, Root: w.Root, Domain: m.Domain, Expose: string(m.Expose),
		Network: m.Proxy.Network, Proxy: m.Proxy.Entrypoints, Env: m.Env, Repos: []repoView{}, Apps: []appDefView{}}
	for _, key := range sortedKeys(m.Repos) {
		r := m.Repos[key]
		v.Repos = append(v.Repos, repoView{Key: key, URL: r.URL, Dir: r.Dir, Runtime: string(r.Runtime),
			Cloned: svc.Files.Exists(m.CloneDir(w.Root, key))})
	}
	for _, key := range sortedKeys(m.Apps) {
		a := m.Apps[key]
		av := appDefView{Key: key, Repo: a.Repo, Services: a.Services}
		for _, opt := range sortedKeys(a.Optional) {
			av.Optional = append(av.Optional, opt)
		}
		for _, hk := range sortedKeys(a.Hosts) {
			h := a.Hosts[hk]
			ep := h.Entrypoint
			if ep == "" {
				ep = "web"
			}
			av.Hosts = append(av.Hosts, hostDefView{Name: hk, Service: h.Service, Port: h.Port, Entrypoint: ep})
		}
		v.Apps = append(v.Apps, av)
	}
	return v, nil
}

func (s Services) Doctor(ctx context.Context, workspace, universe string) (doctorView, error) {
	w, err := s.Machine.WorkspaceNamed(workspace)
	if err != nil {
		return doctorView{}, err
	}
	svc, err := s.At(&collector{}, w.Root, w.Manifest)
	if err != nil {
		return doctorView{}, err
	}
	rep, err := svc.Doctor(ctx, app.DoctorInput{Universe: universe})
	if err != nil {
		return doctorView{}, err
	}
	v := doctorView{OK: true, Checks: []checkView{}}
	for _, c := range rep.Checks {
		v.Checks = append(v.Checks, checkView{Name: c.Name, Status: string(c.Status), Detail: c.Detail})
		if c.Status == app.StatusFail {
			v.OK = false
		}
	}
	return v, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// collector is an Output that keeps warnings for the response and drops the
// rest: a server has no terminal to print progress on.
type collector struct {
	mu    sync.Mutex
	warns []string
}

func (c *collector) Info(string, ...any)        {}
func (c *collector) OK(string, ...any)          {}
func (c *collector) Plain(string, ...any)       {}
func (c *collector) Table([]string, [][]string) {}
func (c *collector) Warn(format string, a ...any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.warns = append(c.warns, fmt.Sprintf(format, a...))
}
func (c *collector) all() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.warns...)
}

var _ app.Output = (*collector)(nil)

// Quiet is an Output for services the server builds: warnings are dropped, and
// a request that wants them uses its own collector.
func Quiet() app.Output { return &collector{} }
