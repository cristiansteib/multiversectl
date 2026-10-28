package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// maxCatalogDepth bounds how far references are followed, as a second guard
// next to cycle detection against a very long chain of repositories.
const maxCatalogDepth = 8

const defaultCatalogFile = "catalog.yaml"

// CatalogDeps are the ports the catalog service needs. It is deliberately not
// part of Service: browsing what exists never touches a multiverse or a
// universe, and the two should not be able to reach into each other.
type CatalogDeps struct {
	Git       Git
	Files     Files
	Decoder   CatalogDecoder
	Editor    CatalogEditor
	Config    ConfigStore
	Manifests ManifestLoader
	Out       Output
	Prompt    Prompter
	Host      RepoHost
	// Hosts are every service a repository can be searched or created on; Host
	// alone is enough when there is only one.
	Hosts  []RepoHost
	Writer ManifestWriter
	// Progress shows a spinner for slow calls; nil means none.
	Progress Progress
	CacheDir string // where remote repositories are cloned

	// Yes answers every confirmation with its default, for scripts.
	Yes bool

	// Timeout bounds each clone or fetch of a catalog repository, so one that
	// cannot be reached (offline, behind a VPN that is down) costs a few
	// seconds, not the minutes git would wait. Zero means DefaultFetchTimeout.
	Timeout time.Duration
}

// DefaultFetchTimeout is how long one catalog repository gets to answer.
const DefaultFetchTimeout = 20 * time.Second

// Catalogs resolves and browses the tree of catalogs.
type Catalogs struct {
	CatalogDeps
	// cfgMu serialises changes of the configuration that read it first.
	cfgMu sync.Mutex
}

func NewCatalogs(d CatalogDeps) *Catalogs {
	if len(d.Hosts) == 0 && d.Host != nil {
		d.Hosts = []RepoHost{d.Host}
	}
	if d.Progress == nil {
		d.Progress = noProgress{}
	}
	if d.Timeout <= 0 {
		d.Timeout = DefaultFetchTimeout
	}
	return &Catalogs{CatalogDeps: d}
}

type noProgress struct{}

func (noProgress) Do(_ string, fn func(context.Context) error) error { return fn(context.Background()) }

func (noProgress) Parallel(_ string, limit int, jobs []Job) []error {
	return RunJobs(context.Background(), limit, jobs, nil)
}

// RunJobs runs jobs with at most limit at a time and returns one error each, in
// order. started and finished, when set, are told as each job begins and ends,
// from the goroutine running it, so a caller can draw them.
func RunJobs(ctx context.Context, limit int, jobs []Job, event func(i int, done bool, err error)) []error {
	if limit < 1 {
		limit = 1
	}
	errs := make([]error, len(jobs))
	slots := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for i := range jobs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			if event != nil {
				event(i, false, nil)
			}
			if ctx.Err() != nil {
				errs[i] = ctx.Err()
			} else {
				errs[i] = jobs[i].Run(ctx)
			}
			if event != nil {
				event(i, true, errs[i])
			}
		}(i)
	}
	wg.Wait()
	return errs
}

// Use records the root catalog for this machine.
func (c *Catalogs) Use(source string) error {
	if source == "" {
		return errors.New("init: a git URL or a file path is required")
	}
	cfg, err := c.Config.Load()
	if err != nil {
		return err
	}
	cfg.Catalog = source
	if err := c.Config.Save(cfg); err != nil {
		return fmt.Errorf("saving the configuration: %w", err)
	}
	c.Out.OK("root catalog is `%s`", shortRepo(source))
	return nil
}

// Tree resolves the catalog at source, or the saved root when source is empty.
// With refresh, every remote repository is fetched first. A layer that cannot
// be read is returned as an unavailable node; only the root failing is an error.
func (c *Catalogs) Tree(ctx context.Context, source string, refresh bool) (domain.Node, error) {
	if !refresh {
		return c.tree(ctx, source, false, false)
	}
	// Fetching can take seconds per layer: show that something is happening.
	var node domain.Node
	err := c.Progress.Do("Fetching the catalog", func(ctx context.Context) (err error) {
		node, err = c.tree(ctx, source, true, false)
		return err
	})
	return node, err
}

// TreeCached is Tree from what is already on disk, with no network and no
// cloning; layers not fetched yet come back unavailable.
func (c *Catalogs) TreeCached(ctx context.Context) (domain.Node, error) {
	return c.tree(ctx, "", false, true)
}

func (c *Catalogs) tree(ctx context.Context, source string, refresh, offline bool) (domain.Node, error) {
	return c.treeObserved(ctx, source, refresh, offline, nil)
}

// TreeObserved is Tree that tells observe, as it happens, which repository is
// being read and how that went, so a page can show the catalogs coming in. It
// may be called from several goroutines at once.
func (c *Catalogs) TreeObserved(ctx context.Context, source string, refresh bool, observe func(CatalogEvent)) (domain.Node, error) {
	return c.treeObserved(ctx, source, refresh, false, observe)
}

func (c *Catalogs) treeObserved(ctx context.Context, source string, refresh, offline bool, observe func(CatalogEvent)) (domain.Node, error) {
	other := false // a catalog that is not this machine's root
	if source == "" {
		cfg, err := c.Config.Load()
		if err != nil {
			return domain.Node{}, err
		}
		if source = cfg.Catalog; source == "" {
			return domain.Node{}, errors.New("no root catalog: `multiversectl init`  (installing a single multiverse does not need one)")
		}
	} else if c.Config != nil {
		if cfg, err := c.Config.Load(); err == nil {
			other = source != cfg.Catalog
		}
	}
	r := c.newResolver(ctx, refresh, offline, observe)
	if other {
		// The paths this machine leaves out are paths of its root catalog: they
		// mean nothing in another one that happens to have a node of that name.
		r.ignored = map[string]bool{}
	}
	root := location{}
	if isGitURL(source) {
		root.repo = source
		dir, err := r.checkout(source, "")
		if err != nil {
			return domain.Node{}, err
		}
		root.dir, root.file = dir, defaultCatalogFile
	} else {
		abs, err := filepath.Abs(source)
		if err != nil {
			return domain.Node{}, err
		}
		root.dir, root.file = filepath.Dir(abs), filepath.Base(abs)
	}
	node, err := r.load(root, "", "", nil)
	if err != nil {
		return domain.Node{}, err
	}
	return node, nil
}

// location is one catalog file: the checkout it sits in and which repository
// that is (empty for a local file), so same-repo references can reuse it.
type location struct {
	repo, ref string
	dir, file string
}

func (l location) key() string { return l.repo + "@" + l.ref + ":" + filepath.Join(l.dir, l.file) }

// CatalogEvent is one step of reading a catalog repository.
type CatalogEvent struct {
	Repo, Ref string
	// Phase is fetching (a clone or fetch has started), done, stale (it could not
	// be fetched and the copy fetched earlier is used) or unavailable.
	Phase  string
	Detail string
	// Millis is how long it took, on the phases that end it.
	Millis int64
}

// maxParallelFetches bounds the git processes a tree reads at once.
const maxParallelFetches = 8

type resolver struct {
	*Catalogs
	ctx     context.Context
	refresh bool
	// offline reads only what is already cached: completion must be instant
	// and must never reach for the network.
	offline bool
	observe func(CatalogEvent)
	// ignored is the paths this machine leaves out: they are not read.
	ignored map[string]bool

	// Sibling catalogs are read at the same time, so everything below is shared
	// between goroutines. A repository is read once per run, by whoever gets
	// there first; the others wait for that and take its result.
	mu     sync.Mutex
	flight map[string]*flight
	// down is the hosts that timed out in this run, with why: the next
	// repository on one fails at once instead of waiting out its own timeout.
	down map[string]string
	// stale is the checkouts (by directory) that could not be refreshed and are
	// read as they were, with why.
	stale map[string]string
	slots chan struct{}
}

type flight struct {
	done  chan struct{}
	dir   string
	fresh bool
	err   error
}

func (c *Catalogs) newResolver(ctx context.Context, refresh, offline bool, observe func(CatalogEvent)) *resolver {
	ignored := map[string]bool{}
	if c.Config != nil {
		if cfg, err := c.Config.Load(); err == nil {
			for _, p := range cfg.Ignored {
				ignored[p] = true
			}
		}
	}
	return &resolver{ignored: ignored, Catalogs: c, ctx: ctx, refresh: refresh, offline: offline, observe: observe,
		flight: map[string]*flight{}, down: map[string]string{}, stale: map[string]string{},
		slots: make(chan struct{}, maxParallelFetches)}
}

func (r *resolver) emit(e CatalogEvent) {
	if r.observe != nil {
		r.observe(e)
	}
}

// network runs a git operation against repo's host within the timeout. A
// timeout says what it probably means, and marks the host unreachable for the
// rest of the run; any other failure (no access, not found) says nothing about
// the host and the next repository on it is still tried.
func (r *resolver) network(repo string, op func(ctx context.Context) error) error {
	host, _, _ := strings.Cut(canonical(repo), "/")
	r.mu.Lock()
	why, bad := r.down[host]
	r.mu.Unlock()
	if bad {
		return errors.New(why)
	}
	select {
	case r.slots <- struct{}{}:
		defer func() { <-r.slots }()
	case <-r.ctx.Done():
		return r.ctx.Err()
	}
	// Another repository on this host may have timed out while this one waited
	// for a slot.
	r.mu.Lock()
	why, bad = r.down[host]
	r.mu.Unlock()
	if bad {
		return errors.New(why)
	}
	ctx, cancel := context.WithTimeout(r.ctx, r.Timeout)
	defer cancel()
	err := op(ctx)
	if err != nil && ctx.Err() == context.DeadlineExceeded && r.ctx.Err() == nil {
		why := fmt.Sprintf("timed out after %s: is %s reachable? (offline, or behind a VPN that is down)", r.Timeout, host)
		r.mu.Lock()
		r.down[host] = why
		r.mu.Unlock()
		return errors.New(why)
	}
	return err
}

func (r *resolver) load(at location, name, nodePath string, stack []string) (domain.Node, error) {
	k := at.key()
	for _, seen := range stack {
		if seen == k {
			return domain.Node{}, fmt.Errorf("catalog %s refers back to itself", at.file)
		}
	}
	if len(stack) >= maxCatalogDepth {
		return domain.Node{}, fmt.Errorf("catalogs nest deeper than %d levels", maxCatalogDepth)
	}
	data, err := r.Files.ReadFile(filepath.Join(at.dir, at.file))
	if err != nil {
		return domain.Node{}, fmt.Errorf("read catalog: %w", err)
	}
	cat, err := r.Decoder.Decode(data)
	if err != nil {
		return domain.Node{}, fmt.Errorf("%s: %w", at.file, err)
	}
	if name == "" {
		name = cat.Name
	}
	node := domain.Node{
		Name: name, Path: nodePath, Kind: domain.CatalogNode,
		Source: domain.Source{Repo: at.repo, Ref: at.ref, Path: filepath.ToSlash(at.file)},
		File:   filepath.Join(at.dir, at.file), Dir: at.dir,
	}
	stack = append(slices.Clone(stack), k)
	r.mu.Lock()
	node.Stale = r.stale[at.dir]
	r.mu.Unlock()

	// The children are independent, and each may wait on the network: read them
	// together, and keep them in the order the catalog lists them.
	children := make([]domain.Node, len(cat.Catalogs))
	var wg sync.WaitGroup
	for i, ref := range cat.Catalogs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			children[i] = r.child(at, ref, nodePath, stack)
		}()
	}
	wg.Wait()
	node.Children = append(node.Children, children...)
	for _, ref := range cat.Multiverses {
		path := domain.ChildPath(nodePath, ref.Name)
		node.Children = append(node.Children, domain.Node{
			Name: ref.Name, Path: path, Kind: domain.MultiverseNode,
			Source: r.absolute(at, ref.Source), Ignored: r.ignored[path],
		})
	}
	return node, nil
}

// child reads one nested catalog; one that cannot be read is a node that says why.
func (r *resolver) child(at location, ref domain.CatalogRef, nodePath string, stack []string) domain.Node {
	child := domain.Node{Name: ref.Name, Path: domain.ChildPath(nodePath, ref.Name), Kind: domain.CatalogNode}
	if r.ignored[child.Path] {
		// Left out: not located, not fetched, so an unreachable one costs nothing.
		child.Ignored = true
		child.Source = domain.Source{Repo: firstOf(ref.Source.Repo, at.repo), Ref: ref.Source.Ref, Path: ref.Source.Path}
		return child
	}
	loc, err := r.locate(at, ref.Source)
	if err == nil {
		var sub domain.Node
		if sub, err = r.load(loc, ref.Name, child.Path, stack); err == nil {
			return sub
		}
	}
	child.Source = domain.Source{Repo: firstOf(ref.Source.Repo, at.repo), Ref: ref.Source.Ref, Path: ref.Source.Path}
	child.Unavailable = reason(err, firstOf(ref.Source.Repo, at.repo))
	return child
}

// locate finds the checkout a CatalogRef's file is in: this one when the ref
// stays in the repository, otherwise a cached clone at the wanted ref.
func (r *resolver) locate(from location, s domain.Source) (location, error) {
	rel := s.Path
	if rel == "" {
		rel = defaultCatalogFile
	}
	if s.Repo == "" || (s.Repo == from.repo && s.Ref == "") {
		return location{repo: from.repo, ref: from.ref, dir: from.dir, file: filepath.FromSlash(rel)}, nil
	}
	dir, err := r.checkout(s.Repo, s.Ref)
	if err != nil {
		return location{}, err
	}
	return location{repo: s.Repo, ref: s.Ref, dir: dir, file: rel}, nil
}

// absolute fills in what a multiverse ref left implicit. In a local catalog
// there is no repository, so the path becomes the absolute manifest file.
func (r *resolver) absolute(from location, s domain.Source) domain.Source {
	if s.Repo == "" {
		s.Repo, s.Ref = from.repo, from.ref
	}
	if s.Repo == "" {
		s.Path = filepath.Join(from.dir, s.Path)
	}
	return s
}

var unsafeRE = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// checkout makes sure repo is cloned (and fetched, once per run, on refresh)
// at ref. Each repo and ref pair gets its own directory: two references to one
// repository at different refs must not fight over a single working tree.
func (r *resolver) checkout(repo, ref string) (string, error) {
	dir, _, err := r.ensure(repo, ref)
	return dir, err
}

// ensure reports whether the clone was made just now. A new clone is already
// at the default branch, and when the repository is empty there is no branch
// to move to, so only an older clone is brought forward on refresh. A
// repository is read once per run: a second caller waits for the first.
func (r *resolver) ensure(repo, ref string) (dir string, fresh bool, err error) {
	sum := sha256.Sum256([]byte(repo))
	dir = filepath.Join(r.CacheDir, hex.EncodeToString(sum[:])[:12], unsafeRE.ReplaceAllString(firstOf(ref, "HEAD"), "_"))
	r.mu.Lock()
	if f, ok := r.flight[dir]; ok {
		r.mu.Unlock()
		<-f.done
		return f.dir, false, f.err
	}
	f := &flight{done: make(chan struct{})}
	r.flight[dir] = f
	r.mu.Unlock()
	f.dir, f.fresh, f.err = r.read(repo, ref, dir)
	close(f.done)
	return f.dir, f.fresh, f.err
}

func (r *resolver) read(repo, ref, dir string) (string, bool, error) {
	fresh := false
	began := time.Now()
	ended := func(phase, detail string) {
		r.emit(CatalogEvent{Repo: repo, Ref: ref, Phase: phase, Detail: detail, Millis: time.Since(began).Milliseconds()})
	}
	switch {
	case !r.Files.Exists(dir) && r.offline:
		return "", false, fmt.Errorf("%s is not fetched yet", repo)
	case !r.Files.Exists(dir):
		if err := r.Files.MkdirAll(filepath.Dir(dir)); err != nil {
			return "", false, err
		}
		r.emit(CatalogEvent{Repo: repo, Ref: ref, Phase: "fetching", Detail: "cloning"})
		if err := r.network(repo, func(ctx context.Context) error { return r.Git.Clone(ctx, repo, dir) }); err != nil {
			// A clone that did not finish must not be taken for a cached one next time.
			_ = r.Files.RemoveAll(dir)
			ended("unavailable", reason(err, repo))
			return "", false, fmt.Errorf("clone %s: %w", repo, err)
		}
		fresh = true
		ended("done", "")
	case r.refresh:
		r.emit(CatalogEvent{Repo: repo, Ref: ref, Phase: "fetching", Detail: "fetching"})
		if err := r.network(repo, func(ctx context.Context) error { return r.Git.Fetch(ctx, dir) }); err != nil {
			// What was fetched before is still a catalog: use it, and say it may be old.
			why := reason(err, repo)
			r.mu.Lock()
			r.stale[dir] = why
			r.mu.Unlock()
			r.Out.Warn("%s could not be fetched (%s); using the copy fetched earlier", shortRepo(repo), why)
			ended("stale", why)
		} else {
			ended("done", "")
		}
	}
	switch {
	case ref != "":
		if err := r.Git.Checkout(r.ctx, dir, ref); err != nil {
			return "", false, fmt.Errorf("%s at %s: %w", repo, ref, err)
		}
	case r.refresh && !fresh:
		// A refresh must move to the new tip of the default branch, not stay
		// on the commit the first clone happened to land on.
		branch, err := r.Git.DefaultBranch(r.ctx, dir)
		if err != nil {
			return "", false, err
		}
		if err := r.Git.Checkout(r.ctx, dir, branch); err != nil {
			return "", false, err
		}
	}
	return dir, fresh, nil
}

func firstOf(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// reason keeps what a person needs from an error. Git prints progress before
// the "fatal:" line that says what went wrong, and says it in terms of
// transports; "no access or not found" is what it comes to for the user.
func reason(err error, repo string) string {
	text := err.Error()
	low := strings.ToLower(text)
	for _, marker := range []string{
		"repository not found", "could not read from remote", "does not appear to be a git repository",
		"authentication failed", "permission denied", "terminal prompts disabled", "could not read username", "not found",
	} {
		if repo != "" && strings.Contains(low, marker) {
			return "no access or not found: " + shortRepo(repo)
		}
	}
	lines := strings.Split(strings.TrimSpace(text), "\n")
	for _, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "fatal:") {
			return strings.TrimSpace(l)
		}
	}
	return strings.TrimSpace(lines[0])
}

// InstalledIn maps the catalog paths of a tree's multiverses to the names they
// are installed under here, by the path they were installed from or, failing
// that, by the source they came from, so one installed straight from its
// repository is still marked once the catalog lists it.
func (c *Catalogs) InstalledIn(tree domain.Node) (map[string]string, error) {
	cfg, err := c.Config.Load()
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, leaf := range tree.Leaves() {
		if name, ok := installedAs(cfg, leaf.Path, originOf(leaf.Source)); ok {
			out[leaf.Path] = name
		}
	}
	return out, nil
}
