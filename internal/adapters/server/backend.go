package server

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strings"
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
	// System reports the versions of what this machine runs on.
	System(ctx context.Context) (systemView, error)
	// Unsaved names the repositories of a universe whose worktree has uncommitted
	// work, which removing the universe would discard.
	Unsaved(ctx context.Context, workspace, universe string) ([]string, error)
	// NextFree is the number a new universe would get, and the numbers passed
	// over because a branch of that name is left behind.
	NextFree(ctx context.Context, workspace string) (nextView, error)
	// Branches lists the branches across a workspace's repositories.
	Branches(ctx context.Context, workspace string) ([]branchView, error)
	// Do carries out an action, reporting to the sink as it goes.
	Do(ctx context.Context, r opRequest, k Sink) error
}

// Sink is where an action reports: the use case's messages, and the output of
// the commands it runs.
type Sink interface {
	app.Output
	Stdout() io.Writer
	Stderr() io.Writer
}

// Services is the Backend over the same use cases the command line runs.
type Services struct {
	Catalogs *app.Catalogs
	// mu keeps two requests from cloning the same catalog repository at once.
	mu      *sync.Mutex
	Machine *app.Service // knows the installed workspaces
	// At builds a service for one workspace. The output is where it warns, which
	// a page cannot show, so the server collects it per request; stdout and
	// stderr are where the commands it runs print, nil to drop them.
	At func(out app.Output, stdout, stderr io.Writer, root, manifest string) (*app.Service, error)
}

var _ Backend = Services{}

// NewServices wires the Backend.
func NewServices(c *app.Catalogs, machine *app.Service, at func(out app.Output, stdout, stderr io.Writer, root, manifest string) (*app.Service, error)) Services {
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
	svc, err := s.At(warn, nil, nil, w.Root, w.Manifest)
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
	svc, err := s.At(&collector{}, nil, nil, w.Root, w.Manifest)
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
	svc, err := s.At(&collector{}, nil, nil, w.Root, w.Manifest)
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

func (s Services) Unsaved(ctx context.Context, workspace, universe string) ([]string, error) {
	w, err := s.Machine.WorkspaceNamed(workspace)
	if err != nil {
		return nil, err
	}
	svc, err := s.At(&collector{}, nil, nil, w.Root, w.Manifest)
	if err != nil {
		return nil, err
	}
	st, err := svc.Resolve(universe)
	if err != nil {
		return nil, err
	}
	items, err := svc.PrunePlan(ctx, app.PruneOptions{})
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		if it.Name == st.Name {
			return it.Dirty, nil
		}
	}
	return nil, nil
}

// Do runs one action as the command line would, with the sink as its terminal.
func (s Services) Do(ctx context.Context, r opRequest, k Sink) error {
	if r.Action == "use" {
		if err := s.Machine.Use(r.Workspace); err != nil {
			return err
		}
		k.OK("%s is now the current workspace", r.Workspace)
		return nil
	}
	w, err := s.Machine.WorkspaceNamed(r.Workspace)
	if err != nil {
		return err
	}
	svc, err := s.At(k, k.Stdout(), k.Stderr(), w.Root, w.Manifest)
	if err != nil {
		return err
	}
	switch r.Action {
	case "up":
		return svc.Start(ctx, app.StartInput{Universe: r.Universe})
	case "down":
		return svc.Stop(ctx, app.StopInput{Universe: r.Universe})
	case "rm":
		if err := svc.Remove(ctx, app.RemoveInput{Universe: r.Universe}); err != nil {
			return err
		}
		k.OK("%s removed", r.Universe)
		return nil
	case "new", "checkout":
		st, err := svc.Create(ctx, app.CreateInput{
			Name: strings.TrimSpace(r.Name), Branch: strings.TrimSpace(r.Branch), Base: strings.TrimSpace(r.Base), Note: r.Note,
			Checkout: r.Action == "checkout", OnlyExisting: r.OnlyExisting,
		})
		if err != nil {
			return err
		}
		k.OK("universe %s ready (%s)", st.Name, st.Project)
		return nil
	case "update":
		return s.update(ctx, w, svc, k)
	}
	return fmt.Errorf("unknown action %q", r.Action)
}

// update is what `catalog update` does for one workspace.
func (s Services) update(ctx context.Context, w app.WorkspaceInfo, svc *app.Service, k Sink) error {
	res, err := s.Catalogs.RefreshWorkspace(ctx, w.Name, w.WorkspaceEntry)
	if err != nil {
		return err
	}
	if res.Skipped != "" {
		k.Info("%s: %s", w.Name, res.Skipped)
		return nil
	}
	missing := s.Catalogs.MissingRepos(svc.Manifest, w.Root)
	if len(missing) > 0 {
		if err := svc.CloneMissing(ctx); err != nil {
			return err
		}
	}
	var did []string
	if res.Changed {
		did = append(did, res.Detail)
	}
	for _, key := range missing {
		did = append(did, key+" cloned")
	}
	if len(did) == 0 {
		k.OK("%s: up to date", w.Name)
		return nil
	}
	k.OK("%s: %s", w.Name, strings.Join(did, " · "))
	return nil
}

// System asks each tool for its version at once. A tool that is missing is
// reported as such, since the page is where someone looks to find out why
// something does not run.
func (s Services) System(ctx context.Context) (systemView, error) {
	probes := []struct {
		name, note string
		args       []string
		parse      func(string) string
	}{
		{"multiversectl", "the command line", []string{"multiversectl", "version"}, wordAt(1)},
		{"git", "worktrees and clones", []string{"git", "--version"}, wordAt(2)},
		{"docker", "the container runtime", []string{"docker", "--version"}, func(o string) string { return strings.TrimSuffix(wordAt(2)(o), ",") }},
		{"docker compose", "runs each app", []string{"docker", "compose", "version", "--short"}, wordAt(0)},
		{"gh", "GitHub sign-in and repositories", []string{"gh", "--version"}, wordAt(2)},
		{"glab", "GitLab sign-in and repositories", []string{"glab", "--version"}, wordAt(1)},
	}
	v := systemView{Format: "multiverse/v1alpha1", Tools: make([]toolVersion, len(probes))}
	var wg sync.WaitGroup
	for i, p := range probes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			t := toolVersion{Name: p.name, Note: p.note}
			path, err := exec.LookPath(p.args[0])
			if err != nil {
				t.Note = "not installed"
				v.Tools[i] = t
				return
			}
			c, cancel := context.WithTimeout(ctx, 6*time.Second)
			defer cancel()
			out, err := exec.CommandContext(c, path, p.args[1:]...).Output()
			if err != nil {
				t.Path, t.Note = path, "installed, but its version could not be read"
				v.Tools[i] = t
				return
			}
			t.Path, t.OK, t.Version = path, true, p.parse(string(out))
			v.Tools[i] = t
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		c, cancel := context.WithTimeout(ctx, 6*time.Second)
		defer cancel()
		rel, err := s.Machine.Releases.Latest(c)
		if err != nil {
			v.Latest.Error = "could not look up the latest release"
			return
		}
		v.Latest.Version = rel.Version
	}()
	wg.Wait()
	for _, t := range v.Tools {
		// A build from source says "dev", which is neither older nor newer than a
		// release, so it is not told to update.
		if t.Name == "multiversectl" && t.OK && v.Latest.Version != "" && looksLikeRelease(t.Version) {
			v.Latest.Newer = app.NewerVersion(v.Latest.Version, t.Version)
		}
	}
	return v, nil
}

// wordAt picks the n-th word of the first line, which is where tools put
// their version.
func wordAt(n int) func(string) string {
	return func(out string) string {
		line, _, _ := strings.Cut(strings.TrimSpace(out), "\n")
		f := strings.Fields(line)
		if n < len(f) {
			return f[n]
		}
		return strings.TrimSpace(line)
	}
}

func looksLikeRelease(v string) bool {
	v = strings.TrimPrefix(v, "v")
	return v != "" && v[0] >= '0' && v[0] <= '9'
}

func (s Services) service(workspace string) (*app.Service, error) {
	w, err := s.Machine.WorkspaceNamed(workspace)
	if err != nil {
		return nil, err
	}
	return s.At(&collector{}, nil, nil, w.Root, w.Manifest)
}

func (s Services) NextFree(ctx context.Context, workspace string) (nextView, error) {
	svc, err := s.service(workspace)
	if err != nil {
		return nextView{}, err
	}
	n, skipped, err := svc.NextFree(ctx)
	if err != nil {
		return nextView{}, err
	}
	return nextView{Number: n, Skipped: skipped}, nil
}

func (s Services) Branches(ctx context.Context, workspace string) ([]branchView, error) {
	svc, err := s.service(workspace)
	if err != nil {
		return nil, err
	}
	list, err := svc.Branches(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]branchView, len(list))
	for i, b := range list {
		out[i] = branchView{Name: b.Name, Repos: b.Repos}
	}
	return out, nil
}
