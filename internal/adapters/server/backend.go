package server

import (
	"context"
	"errors"
	"fmt"
	"github.com/cristiansteib/multiversectl/internal/adapters/userdirs"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cristiansteib/multiversectl/internal/app"
	"github.com/cristiansteib/multiversectl/internal/domain"
)

// Backend is what the server reads. It is read-only by construction: there is
// no method here that changes a catalog, a multiverse or a universe.
type Backend interface {
	Status(ctx context.Context, fetch bool) (app.StatusReport, error)
	// Catalog is the tree; observe is told, while it is read, which repository
	// is being fetched and how that went. It may be nil.
	Catalog(ctx context.Context, fetch bool, observe func(app.CatalogEvent)) (catalogView, error)
	// Ignore leaves a path of the catalog tree out on this machine, or brings it back.
	Ignore(ctx context.Context, path string, ignore bool) error
	// Plan is what installing a path of the catalog would set up, without doing it.
	Plan(ctx context.Context, path string, exclude []string) (planView, error)
	// Config is the manifest a universe uses, against the one it was started with.
	Config(ctx context.Context, multiverse, universe string) (configView, error)
	// Fleet lists the universes of one installed multiverse, or of all.
	Fleet(ctx context.Context, multiverse string) (fleetView, error)
	Multiverse(ctx context.Context, name string) (multiverseView, error)
	Doctor(ctx context.Context, multiverse, universe string) (doctorView, error)
	// System reports the versions of what this machine runs on.
	System(ctx context.Context) (systemView, error)
	// Update says what a newer release would bring: from what the last look
	// found, or after asking for the newest releases (see lookMode).
	Update(ctx context.Context, mode lookMode) (updateView, error)
	// Unsaved names the repositories of a universe whose worktree has uncommitted
	// work, which removing the universe would discard.
	Unsaved(ctx context.Context, multiverse, universe string) ([]string, error)
	// NextFree is the number a new universe would get, and the numbers passed
	// over because a branch of that name is left behind.
	NextFree(ctx context.Context, multiverse string) (nextView, error)
	// Branches lists the branches across a multiverse's repositories.
	Branches(ctx context.Context, multiverse string) ([]branchView, error)
	// Containers lists a universe's containers; Stats reads their resource use;
	// Logs writes one service's log to w, until ctx ends when it follows.
	Containers(ctx context.Context, multiverse, universe string) ([]containerView, error)
	Stats(ctx context.Context, multiverse, universe string) ([]statView, error)
	// Notes is a universe's journal, oldest first; Note adds a line to it, or
	// with an id changes or removes one, and answers with the journal as it
	// stands, so a page that wrote need not ask again. The journal is what the
	// command line writes with `universe notes`, so a page and an agent read
	// the same thing.
	Notes(ctx context.Context, multiverse, universe string) ([]noteView, error)
	Note(ctx context.Context, r noteRequest) ([]noteView, error)
	// Code reads a universe's repositories: branch, how far it has moved from
	// what it follows, and the pull or merge request open for it.
	// With fetch the clones ask their remotes first; without it everything is as
	// of the last fetch.
	Code(ctx context.Context, multiverse, universe string, fetch bool) (codeView, error)
	// Diff lists the files of one repository's change, scope being "branch"
	// (against what it merges into) or "uncommitted"; with a file it is that
	// file's diff instead, in the second result.
	// base is what the page showed the change against; empty works it out.
	Diff(ctx context.Context, multiverse, universe, repo, scope, base string) (diffFilesView, error)
	Patch(ctx context.Context, multiverse, universe, repo, scope, base, file string) (patchView, error)
	Logs(ctx context.Context, multiverse, universe, appKey, service string, tail int, follow bool, w io.Writer) error
	// FleetStats reads every running container of one multiverse, or of all.
	FleetStats(ctx context.Context, multiverse string) (fleetStatsView, error)
	// Do carries out an action, reporting to the sink as it goes.
	Do(ctx context.Context, r opRequest, k Sink) error
}

// lookMode is how much a look for a newer release asks for.
type lookMode int

const (
	// readCached asks nobody: it is what the last look found.
	readCached lookMode = iota
	// lookIfStale asks only when that is older than app.UpdateEvery, so a server
	// that is restarted often does not ask each time.
	lookIfStale
	// lookNow asks.
	lookNow
)

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
	Machine *app.Service // knows the installed multiverses
	// At builds a service for one multiverse. The output is where it warns, which
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

func (s Services) Catalog(ctx context.Context, fetch bool, observe func(app.CatalogEvent)) (catalogView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tree, err := s.Catalogs.TreeObserved(ctx, "", fetch, observe)
	if err != nil {
		return catalogView{}, err
	}
	installed, err := s.Catalogs.InstalledIn(tree)
	if err != nil {
		return catalogView{}, err
	}
	return catalogView{Root: toNode(tree, installed), Installs: len(installed)}, nil
}

// Ignore takes no part in s.mu, which a read of the catalog holds for as long as
// its slowest fetch: ignoring a catalog that hangs is what it is for, and it must
// not wait for the very read it is meant to stop. It reads the tree offline and
// the configuration serialises its own change.
func (s Services) Ignore(ctx context.Context, path string, ignore bool) error {
	return s.Catalogs.Ignore(ctx, []string{path}, ignore)
}

func toNode(n domain.Node, installed map[string]string) nodeView {
	v := nodeView{Name: n.Name, Path: n.Path, Kind: string(n.Kind), Repo: n.Source.Repo, Ref: n.Source.Ref,
		File: n.Source.Path, InstalledAs: installed[n.Path], Unavailable: n.Unavailable, Stale: n.Stale, Ignored: n.Ignored, Loc: locOf(n.Location()), Within: n.FileWithin()}
	for _, c := range n.Children {
		v.Children = append(v.Children, toNode(c, installed))
	}
	return v
}

func (s Services) infos(multiverse string) ([]app.MultiverseInfo, error) {
	if multiverse != "" {
		w, err := s.Machine.MultiverseNamed(multiverse)
		if err != nil {
			return nil, err
		}
		return []app.MultiverseInfo{w}, nil
	}
	return s.Machine.Multiverses()
}

// Fleet asks every multiverse at once: each asks the container runtime about
// each of its universes, and the page would otherwise wait on them in turn.
func (s Services) Fleet(ctx context.Context, multiverse string) (fleetView, error) {
	list, err := s.infos(multiverse)
	if err != nil {
		return fleetView{}, err
	}
	out := make([]fleetMultiverse, len(list))
	var wg sync.WaitGroup
	for i, w := range list {
		wg.Add(1)
		go func(i int, w app.MultiverseInfo) {
			defer wg.Done()
			out[i] = s.fleetOf(ctx, w)
		}(i, w)
	}
	wg.Wait()
	return fleetView{Multiverses: out}, nil
}

func (s Services) fleetOf(ctx context.Context, w app.MultiverseInfo) fleetMultiverse {
	fw := fleetMultiverse{Name: w.Name, Root: w.Root, Universes: []universeView{}}
	warn := &collector{}
	svc, err := s.At(warn, nil, nil, w.Root, w.Manifest)
	if err != nil {
		fw.Error = err.Error()
		return fw
	}
	list, err := svc.List(ctx, app.ListOptions{Services: true, Freshness: true})
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
		Note: u.Note, PR: u.PR, Branches: u.Branches, Apps: []appView{}, Checkouts: []checkoutView{},
		Created: stamp(u.Created), LastStarted: stamp(u.LastStarted)}
	for _, a := range u.Apps {
		av := appView{Key: a.Key, Repo: a.Repo, Branch: a.Branch, Path: a.Path, Running: a.Running, Expected: a.Expected, OnDemand: a.OnDemand}
		for _, h := range a.URLs {
			av.URLs = append(av.URLs, urlView{Host: h.Host, Service: h.Service, URL: h.URL})
		}
		for _, sv := range a.Services {
			av.Services = append(av.Services, serviceView{Name: sv.Name, State: string(sv.State), Detail: sv.Detail, Published: sv.Published})
		}
		v.Apps = append(v.Apps, av)
	}
	for _, c := range u.Checkouts {
		cv := checkoutView{Repo: c.Repo, Branch: c.Branch, Path: c.Path, App: c.App}
		if f := c.Freshness; f != nil {
			cv.Follows, cv.Behind, cv.Ahead, cv.Dirty = f.Follows, f.Behind, f.Ahead, f.Dirty
			v.Behind += f.Behind
		}
		v.Checkouts = append(v.Checkouts, cv)
	}
	return v
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func (s Services) Multiverse(ctx context.Context, name string) (multiverseView, error) {
	w, err := s.Machine.MultiverseNamed(name)
	if err != nil {
		return multiverseView{}, err
	}
	svc, err := s.At(&collector{}, nil, nil, w.Root, w.Manifest)
	if err != nil {
		return multiverseView{}, err
	}
	m := svc.Manifest
	ctxInfo := svc.Context()
	v := multiverseView{Name: w.Name, Root: w.Root, Manifest: ctxInfo.Manifest, From: ctxInfo.From, Source: ctxInfo.Source,
		UniversesDir: ctxInfo.UniversesDir, Domain: m.Domain, Expose: string(m.Expose),
		Network: m.Proxy.Network, Proxy: m.Proxy.Entrypoints, Env: m.Env, Repos: []repoView{}, Apps: []appDefView{}}
	fresh := svc.CloneFreshness(ctx)
	for _, key := range sortedKeys(m.Repos) {
		r := m.Repos[key]
		f := fresh[key]
		v.Repos = append(v.Repos, repoView{Follows: f.Follows, Behind: f.Behind, Ahead: f.Ahead, Dirty: f.Dirty, Key: key, Clone: m.CloneDir(w.Root, key), Loc: locOf(domain.Locate(r.URL, "")), URL: r.URL, Dir: r.Dir,
			Cloned: svc.Files.Exists(m.CloneDir(w.Root, key))})
	}
	discovered := discoveredHosts(svc)
	for _, key := range sortedKeys(m.Apps) {
		a := m.Apps[key]
		av := appDefView{Key: key, Repo: a.Repo, Services: a.Services}
		for _, opt := range sortedKeys(a.Optional) {
			av.Optional = append(av.Optional, opt)
		}
		for _, hk := range sortedKeys(a.Hosts) {
			h := a.Hosts[hk]
			forward := h.PortForward
			if m.IPOnly() {
				forward = domain.PortForwardOnly // what every host is, with no domain
			}
			av.Hosts = append(av.Hosts, hostDefView{Name: hk, Service: h.Service, Port: h.Port, Entrypoint: orWeb(h.Entrypoint),
				AlsoMatch: h.AlsoMatch, PortForward: forward, Scheme: h.Scheme})
		}
		if len(a.Hosts) == 0 {
			av.Hosts = discovered[key]
		}
		v.Apps = append(v.Apps, av)
	}
	return v, nil
}

// discoveredHosts are the addresses of the apps that leave them to their compose
// files: labels, or a single exposed port. Only a universe that has been made
// knows them, and they are taken again on every start and sync, so the universe
// that last did either speaks for the multiverse. A universe on a branch that
// adds a label can make that one speak for it too; that is what the page's
// "discovered" says.
func discoveredHosts(svc *app.Service) map[string][]hostDefView {
	all, err := svc.Store.List()
	if err != nil {
		return nil
	}
	sort.SliceStable(all, func(i, j int) bool { return lastResolved(all[i]).After(lastResolved(all[j])) })
	out := map[string][]hostDefView{}
	for _, st := range all {
		byApp := map[string][]hostDefView{}
		for _, hk := range sortedKeys(st.Rules) {
			r := st.Rules[hk]
			byApp[r.App] = append(byApp[r.App], hostDefView{Name: hk, Service: r.Service, Port: r.Port, Entrypoint: orWeb(r.Entrypoint),
				AlsoMatch: r.Aliases, PortForward: r.PortForward, Scheme: r.Scheme, Discovered: true})
		}
		for appKey, hosts := range byApp {
			if _, seen := out[appKey]; !seen {
				out[appKey] = hosts
			}
		}
	}
	return out
}

func lastResolved(st domain.UniverseState) time.Time {
	if st.LastStarted.After(st.Created) {
		return st.LastStarted
	}
	return st.Created
}

func orWeb(entrypoint string) string {
	if entrypoint == "" {
		return "web"
	}
	return entrypoint
}

func (s Services) Doctor(ctx context.Context, multiverse, universe string) (doctorView, error) {
	w, err := s.Machine.MultiverseNamed(multiverse)
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

func (s Services) Containers(ctx context.Context, multiverse, universe string) ([]containerView, error) {
	svc, err := s.service(multiverse)
	if err != nil {
		return nil, err
	}
	rows, err := svc.UniverseContainers(ctx, universe)
	if err != nil {
		return nil, err
	}
	out := make([]containerView, 0, len(rows))
	for _, r := range rows {
		out = append(out, containerView{App: r.App, Service: r.Service, Name: r.Name, Status: r.Status, Running: r.Running, Ports: r.Ports})
	}
	return out, nil
}

// Code reads one universe's repositories: the branches, how far they have
// moved, and the review each has open.
func (s Services) Code(ctx context.Context, multiverse, universe string, fetch bool) (codeView, error) {
	svc, err := s.service(multiverse)
	if err != nil {
		return codeView{}, err
	}
	rows, err := svc.UniverseCode(ctx, universe, fetch)
	if err != nil {
		return codeView{}, err
	}
	v := codeView{Rows: []codeRow{}}
	var oldest time.Time
	for _, r := range rows {
		row := codeRow{Repo: r.Repo, Branch: r.Branch, Follows: r.Follows,
			Behind: r.Behind, Ahead: r.Ahead, Dirty: r.Dirty, ReviewError: r.ReviewError,
			Work: diffOf(r.Work), Base: r.Base, BaseUnknown: r.BaseUnknown, Change: diffOf(r.Change), Uncommitted: diffOf(r.Uncommitted)}
		if r.Review != nil {
			row.Review = &reviewView{Number: r.Review.Number, Mark: reviewMark(r.URL), Title: r.Review.Title,
				URL: r.Review.URL, State: r.Review.State, Author: r.Review.Author, Updated: stamp(r.Review.Updated)}
		}
		v.Rows = append(v.Rows, row)
		if !r.Fetched.IsZero() && (oldest.IsZero() || r.Fetched.Before(oldest)) {
			oldest = r.Fetched
		}
	}
	v.Fetched = stamp(oldest)
	return v, nil
}

// Diff lists the files of one repository's change.
func (s Services) Diff(ctx context.Context, multiverse, universe, repo, scope, base string) (diffFilesView, error) {
	svc, err := s.service(multiverse)
	if err != nil {
		return diffFilesView{}, err
	}
	base, files, err := svc.UniverseDiff(ctx, universe, repo, scope, base)
	if err != nil {
		return diffFilesView{}, err
	}
	v := diffFilesView{Base: base, Files: []fileView{}}
	for _, f := range files {
		v.Files = append(v.Files, fileView{Path: f.Path, OldPath: f.OldPath, Status: f.Status, Added: f.Added, Removed: f.Removed, Binary: f.Binary})
	}
	if scope == app.ScopeBranch {
		// The commits are of the same change, against the same base. A log that
		// cannot be read leaves the files to be shown on their own.
		if commits, err := svc.UniverseCommits(ctx, universe, repo, base); err == nil {
			for _, c := range commits {
				cv := commitView{SHA: c.SHA, Author: c.Author, Subject: c.Subject, Body: c.Body}
				if !c.At.IsZero() {
					cv.At = c.At.Format(time.RFC3339)
				}
				v.Commits = append(v.Commits, cv)
			}
		}
	}
	return v, nil
}

// maxPatch is how much of one file's diff is sent: the page draws it as text,
// and a generated file of a few megabytes is not something to read there.
const maxPatch = 256 << 10

// Patch is one file's diff.
func (s Services) Patch(ctx context.Context, multiverse, universe, repo, scope, base, file string) (patchView, error) {
	svc, err := s.service(multiverse)
	if err != nil {
		return patchView{}, err
	}
	patch, truncated, err := svc.UniverseDiffFile(ctx, universe, repo, scope, base, file, maxPatch)
	if err != nil {
		return patchView{}, err
	}
	return patchView{Path: file, Patch: patch, Truncated: truncated}, nil
}

// diffOf leaves out a change of nothing, so a page can ask whether there is one.
func diffOf(d app.DiffStat) *diffView {
	if d.Empty() {
		return nil
	}
	return &diffView{Files: d.Files, Added: d.Added, Removed: d.Removed}
}

// reviewMark is how the service writes a review's number: GitLab's merge
// requests are !12, and everything else is a pull request, #12.
func reviewMark(repoURL string) string {
	if domain.Locate(repoURL, "").Platform == domain.PlatformGitLab {
		return "!"
	}
	return "#"
}

func (s Services) Stats(ctx context.Context, multiverse, universe string) ([]statView, error) {
	svc, err := s.service(multiverse)
	if err != nil {
		return nil, err
	}
	stats, err := svc.Stats(ctx, universe)
	if err != nil {
		return nil, err
	}
	out := make([]statView, 0, len(stats))
	for _, c := range stats {
		out = append(out, statOf(c))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// FleetStats asks every multiverse at once, as Fleet does, and says which ones
// could not answer rather than leaving their containers out silently.
func (s Services) FleetStats(ctx context.Context, multiverse string) (fleetStatsView, error) {
	list, err := s.infos(multiverse)
	if err != nil {
		return fleetStatsView{}, err
	}
	rows := make([][]fleetStatRow, len(list))
	fails := make([]fleetStatError, len(list))
	var wg sync.WaitGroup
	for i, w := range list {
		wg.Add(1)
		go func(i int, w app.MultiverseInfo) {
			defer wg.Done()
			svc, err := s.At(&collector{}, nil, nil, w.Root, w.Manifest)
			if err == nil {
				var read []app.StatRow
				if read, err = svc.ProjectStats(ctx); err == nil {
					for _, r := range read {
						rows[i] = append(rows[i], fleetStatRow{Multiverse: w.Name, Universe: r.Universe, App: r.App, Service: r.Service, statView: statOf(r.ContainerStats)})
					}
				}
			}
			if err != nil {
				fails[i] = fleetStatError{Multiverse: w.Name, Error: err.Error()}
			}
		}(i, w)
	}
	wg.Wait()
	v := fleetStatsView{At: time.Now().UTC().Format(time.RFC3339), Rows: []fleetStatRow{}}
	for i := range list {
		v.Rows = append(v.Rows, rows[i]...)
		if fails[i].Error != "" {
			v.Sources = append(v.Sources, fails[i])
		}
	}
	return v, nil
}

func (s Services) Logs(ctx context.Context, multiverse, universe, appKey, service string, tail int, follow bool, w io.Writer) error {
	svc, err := s.service(multiverse)
	if err != nil {
		return err
	}
	return svc.Logs(ctx, app.LogsInput{Universe: universe, App: appKey, Service: service, Tail: tail, Follow: follow}, w)
}

func statOf(c app.ContainerStats) statView {
	return statView{Name: c.Name, CPU: c.CPU, Memory: c.Memory, MemUsage: c.MemUsage,
		Mem: c.MemBytes, MemLimit: c.MemLimit, NetIO: c.NetIO, PIDs: c.PIDs}
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

func (s Services) Unsaved(ctx context.Context, multiverse, universe string) ([]string, error) {
	w, err := s.Machine.MultiverseNamed(multiverse)
	if err != nil {
		return nil, err
	}
	svc, err := s.At(&collector{}, nil, nil, w.Root, w.Manifest)
	if err != nil {
		return nil, err
	}
	// The use case answers this for one universe. It used to be read out of a
	// prune plan, which is the same question asked of every universe of the
	// multiverse to throw all but one of the answers away.
	return svc.Unsaved(ctx, universe)
}

// ErrRestart is what an action returns when it succeeded and the server has to
// start again to run what it installed: not a failure, so the operation ends as
// done and the server then replaces itself.
var ErrRestart = errors.New("restart to run the new build")

// selfUpdate replaces the programs on disk with the latest release. The command
// line is looked for beside this server, then on the PATH; the server's own file
// is the one that is running, which is why the process has to start again.
func (s Services) selfUpdate(ctx context.Context, k Sink) error {
	if !app.Released(s.Machine.Version) {
		return fmt.Errorf("this server is %s, a build from source: it is not replaced by a release", s.Machine.Version)
	}
	server, err := os.Executable()
	if err != nil {
		return err
	}
	server = strings.TrimSuffix(server, " (deleted)")
	// What replaces the running file is the file, not a link to it.
	if resolved, err := filepath.EvalSymlinks(server); err == nil {
		server = resolved
	}
	// The command line is beside the server, or on the PATH; a machine that has
	// only the server has none, and none is made.
	cli := filepath.Join(filepath.Dir(server), "multiversectl")
	if _, err := os.Stat(cli); err != nil {
		cli = ""
		if p, err := exec.LookPath("multiversectl"); err == nil {
			cli = p
		}
	}
	if resolved, err := filepath.EvalSymlinks(cli); cli != "" && err == nil {
		cli = resolved
	}
	m := *s.Machine
	m.Out, m.Progress = k, nil
	res, err := m.SelfUpdate(ctx, app.SelfUpdateInput{
		CurrentVersion: s.Machine.Version, OS: runtime.GOOS, Arch: runtime.GOARCH, BinaryPath: cli, ServerPath: server,
	})
	if err != nil {
		return err
	}
	if !res.Updated {
		k.OK("already up to date (%s)", res.From)
		return nil
	}
	k.OK("updated %s -> %s; the web view restarts to run it", res.From, res.To)
	return ErrRestart
}

// installBase is the directory multiverses go under, as the command line has it.
func installBase() string {
	if b := userdirs.RootEnv(); b != "" {
		return b
	}
	return userdirs.DefaultRoot()
}

func (s Services) plan(ctx context.Context, path string, exclude []string) ([]app.InstallTarget, error) {
	// The catalog is read under the same lock as everywhere else: it may clone.
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Catalogs.PlanInstall(ctx, []string{path}, app.InstallOptions{Base: installBase(), Exclude: exclude})
}

func (s Services) Plan(ctx context.Context, path string, exclude []string) (planView, error) {
	targets, err := s.plan(ctx, path, exclude)
	if err != nil {
		return planView{}, err
	}
	v := planView{Targets: []planTarget{}}
	for _, t := range targets {
		v.Targets = append(v.Targets, planTarget{Name: t.Name, Root: t.Root, Path: t.From, Skip: t.Skip, Renamed: t.Renamed})
	}
	return v, nil
}

// install sets up what a path of the catalog names, as `start <path> --install-only`
// does: the manifest is recorded and the repositories it lists are cloned.
func (s Services) install(ctx context.Context, r opRequest, k Sink) error {
	plan, err := s.plan(ctx, r.Path, r.Exclude)
	if err != nil {
		return err
	}
	machine := *s.Machine
	machine.Out, machine.Progress = k, nil
	var todo []app.InstallTarget
	for _, t := range plan {
		if t.Skip != "" {
			k.Info("%s: %s", t.Name, t.Skip)
			continue
		}
		todo = append(todo, t)
	}
	if len(todo) == 0 {
		return fmt.Errorf("nothing to install under %s", r.Path)
	}
	var failures []string
	for _, t := range todo {
		// Where it lives can be chosen for one multiverse; for several each takes the default.
		if r.Root != "" && len(todo) == 1 {
			t.Root = r.Root
		}
		if t.Renamed != "" {
			k.Info("%s", t.Renamed)
		}
		k.Info("installing %s into %s", t.Name, t.Root)
		entry, err := machine.Bootstrap(ctx, app.BootstrapInput{Name: t.Name, Root: t.Root, ManifestSource: t.ManifestSource, Ref: t.Ref,
			ManifestPath: t.ManifestPath, From: t.From, Origin: t.Origin, Generated: t.Generated})
		if err == nil {
			var svc *app.Service
			if svc, err = s.At(k, k.Stdout(), k.Stderr(), entry.Root, entry.Manifest); err == nil {
				err = svc.CloneRepos(ctx)
			}
		}
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", t.Name, err))
			k.Warn("%s: %v", t.Name, err)
			continue
		}
		k.OK("installed %s", t.Name)
	}
	if len(failures) > 0 {
		return fmt.Errorf("%s", strings.Join(failures, "; "))
	}
	return nil
}

// Do runs one action as the command line would, with the sink as its terminal.
func (s Services) Do(ctx context.Context, r opRequest, k Sink) error {
	switch r.Action {
	case "selfupdate":
		return s.selfUpdate(ctx, k)
	case "install":
		return s.install(ctx, r, k)
	}
	w, err := s.Machine.MultiverseNamed(r.Multiverse)
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
	case "restart":
		if err := svc.Restart(ctx, app.RestartInput{Universe: r.Universe, App: r.App, Service: r.Service}); err != nil {
			return err
		}
		k.OK("%s/%s restarted", r.App, r.Service)
		return nil
	case "sync":
		res, err := svc.Sync(ctx, r.Universe, app.SyncOptions{})
		if err != nil {
			return err
		}
		for _, x := range res {
			if x.Moved > 0 {
				k.OK("%s: %s (%d commits)", x.Repo, x.Detail, x.Moved)
			} else {
				k.Info("%s: %s", x.Repo, x.Detail)
			}
		}
		return nil
	case "rm":
		if err := svc.Remove(ctx, app.RemoveInput{Universe: r.Universe, Force: r.Force}); err != nil {
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
		if n, ok := k.(interface{ Universe(string) }); ok {
			n.Universe(st.Name)
		}
		k.OK("universe %s ready (%s)", st.Name, st.Project)
		return nil
	case "update":
		return s.update(ctx, w, svc, k)
	}
	return fmt.Errorf("unknown action %q", r.Action)
}

// refreshCatalog is the part of update that works in the catalog clone, under
// the lock Status and Catalog hold while they read it: two git processes in one
// clone contend, and this one was taking none. The lock covers this call alone
// — the cloning that may follow takes minutes, and holding it across that would
// stall every read the page makes.
func (s Services) refreshCatalog(ctx context.Context, w app.MultiverseInfo) (app.UpdateResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Catalogs.RefreshMultiverse(ctx, w.Name, w.MultiverseEntry)
}

// update is what `catalog update` does for one multiverse.
func (s Services) update(ctx context.Context, w app.MultiverseInfo, svc *app.Service, k Sink) error {
	res, err := s.refreshCatalog(ctx, w)
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
	clones, err := svc.UpdateClones(ctx)
	if err != nil {
		return err
	}
	for _, c := range clones {
		if c.Moved > 0 {
			did = append(did, fmt.Sprintf("%s %s", c.Repo, c.Detail))
		} else {
			k.Warn("%s: %s: %s", w.Name, c.Repo, c.Detail)
		}
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
		if t.Name == "multiversectl" && t.OK && v.Latest.Version != "" && app.Released(t.Version) {
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

// Update says what this build has missed, from what the last look found, or
// after a new one: always, or only if the last is old. A look that fails still
// answers with what the earlier ones found.
func (s Services) Update(ctx context.Context, mode lookMode) (updateView, error) {
	rec, _ := s.Machine.CachedUpdate()
	var err error
	if mode == lookNow || (mode == lookIfStale && rec.Stale(time.Now())) {
		rec, err = s.Machine.CheckForUpdate(ctx, time.Now())
	}
	var v updateView
	if n, ok := rec.Notice(s.Machine.Version); ok {
		v.Notice = &n
	}
	return v, err
}

func (s Services) service(multiverse string) (*app.Service, error) {
	w, err := s.Machine.MultiverseNamed(multiverse)
	if err != nil {
		return nil, err
	}
	return s.At(&collector{}, nil, nil, w.Root, w.Manifest)
}

func (s Services) Notes(_ context.Context, multiverse, universe string) ([]noteView, error) {
	svc, err := s.service(multiverse)
	if err != nil {
		return nil, err
	}
	notes, err := svc.Journal(universe)
	if err != nil {
		return nil, err
	}
	out := make([]noteView, 0, len(notes))
	for _, n := range notes {
		out = append(out, noteViewOf(n))
	}
	return out, nil
}

func (s Services) Note(ctx context.Context, r noteRequest) ([]noteView, error) {
	svc, err := s.service(r.Multiverse)
	if err != nil {
		return nil, err
	}
	switch r.Action {
	case "add":
		_, err = svc.AddNote(ctx, r.Universe, r.Text, r.By)
	case "set":
		_, err = svc.EditNote(ctx, r.Universe, r.ID, r.Text)
	case "rm":
		_, err = svc.RemoveNote(ctx, r.Universe, r.ID)
	default:
		return nil, fmt.Errorf("unknown action %q: add, set or rm", r.Action)
	}
	if err != nil {
		return nil, err
	}
	return s.Notes(ctx, r.Multiverse, r.Universe)
}

func (s Services) Config(_ context.Context, multiverse, universe string) (configView, error) {
	svc, err := s.service(multiverse)
	if err != nil {
		return configView{}, err
	}
	rep, err := svc.UniverseConfig(universe)
	if err != nil {
		return configView{}, err
	}
	info := svc.Context()
	v := configView{
		Path: rep.Path, From: info.From, Source: info.Source, SHA256: rep.SHA256, Bytes: len(rep.Current), YAML: string(rep.Current),
		Problems: rep.Problems, Snapshot: string(rep.Snapshot), SnapshotProblems: rep.SnapshotProblems, Changed: rep.Changed,
	}
	if v.Problems == nil {
		v.Problems = []string{}
	}
	if rec := rep.StartedWith; rec != nil {
		v.StartedWith = &configRecordView{SHA256: rec.SHA256, At: rec.At.UTC().Format(time.RFC3339), Version: rec.Version}
	}
	return v, nil
}

func (s Services) NextFree(ctx context.Context, multiverse string) (nextView, error) {
	svc, err := s.service(multiverse)
	if err != nil {
		return nextView{}, err
	}
	n, skipped, err := svc.NextFree(ctx)
	if err != nil {
		return nextView{}, err
	}
	return nextView{Number: n, Skipped: skipped}, nil
}

func (s Services) Branches(ctx context.Context, multiverse string) ([]branchView, error) {
	svc, err := s.service(multiverse)
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
