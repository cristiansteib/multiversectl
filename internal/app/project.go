package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// ProjectInput describes a new multiverse: the repositories it is made of, and
// where its own repository (its index and its manifest) lives. Whatever is
// left out is asked, so the same call serves a guided setup and a script.
type ProjectInput struct {
	Dir  string // where the multiverse's repository is, or will be; default .
	Name string

	Repos []string // repositories to include: URLs, or owner/repo for GitHub
	Scan  bool     // include the git checkouts found here, without asking

	// Where the multiverse's own repository lives: one of these, or the origin of
	// Dir when it already has one, or ask.
	URL      string // an existing repository
	Create   string // owner/name of a new one
	Provider string // the service Create is on; the first available when empty
	Public   bool
	Local    bool // keep it on this machine for now

	// Where to list it in the root catalog.
	At      string
	AtGiven bool
	NoLink  bool
}

// ProjectResult is what a multiverse init made.
type ProjectResult struct {
	Name string
	Dir  string
	URL  string // "" when it is only on this machine
	// Node is where it was listed in the root catalog, "" when it was not.
	Node string
	// Root is the directory the multiverse's checkouts already sit under, which is
	// the multiverse root to install it with; "" when there were none.
	Root string
}

// FoundCheckout is a git working copy found next to the multiverse.
type FoundCheckout struct {
	Dir     string
	URL     string // its origin
	Compose bool   // it has a compose file of its own
}

var composeFiles = domain.ComposeNames

// ScanCheckouts finds git working copies under dir, and when there are none
// there, next to it: a multiverse's repository usually sits beside the repositories
// it describes. It returns the directory they were found in.
func (c *Catalogs) ScanCheckouts(ctx context.Context, dir string) (base string, found []FoundCheckout) {
	for _, b := range []string{dir, filepath.Dir(dir)} {
		names, err := c.Files.ListDirs(b)
		if err != nil {
			continue
		}
		for _, n := range names {
			p := filepath.Join(b, n)
			if p == dir || strings.HasPrefix(n, ".") || !c.Files.Exists(filepath.Join(p, ".git")) {
				continue
			}
			// A checkout nobody else can fetch is no use in a shared manifest.
			url, err := c.Git.RemoteURL(ctx, p)
			if err != nil || url == "" {
				continue
			}
			co := FoundCheckout{Dir: p, URL: url}
			for _, f := range composeFiles {
				if c.Files.Exists(filepath.Join(p, f)) {
					co.Compose = true
				}
			}
			found = append(found, co)
		}
		if len(found) > 0 {
			return b, found
		}
	}
	return "", nil
}

// hostChecks asks each hosting service once whether it can be used, in
// parallel and behind one spinner, the first time a menu needs to know. A menu
// is drawn again after every step; asking each time is a pause on every one.
type hostChecks struct {
	c       *Catalogs
	checked bool
	err     map[string]error
}

func (c *Catalogs) newHostChecks() *hostChecks { return &hostChecks{c: c, err: map[string]error{}} }

func (hc *hostChecks) probe(ctx context.Context) {
	if hc.checked {
		return
	}
	hc.checked = true
	results := make([]error, len(hc.c.Hosts))
	_ = hc.c.Progress.Do("Checking your sign-in to "+hostNames(hc.c.Hosts), func(ctx context.Context) error {
		var wg sync.WaitGroup
		for i, h := range hc.c.Hosts {
			wg.Add(1)
			go func() {
				defer wg.Done()
				results[i] = h.Available(ctx)
			}()
		}
		wg.Wait()
		return nil
	})
	for i, h := range hc.c.Hosts {
		hc.err[h.Name()] = results[i]
	}
}

func hostNames(hosts []RepoHost) string {
	names := make([]string, len(hosts))
	for i, h := range hosts {
		names[i] = h.Name()
	}
	return strings.Join(names, " and ")
}

// option is the menu line for using the service: ready when it can be, one
// that signs in first when only the person is missing, and shown with its
// reason when something has to be installed.
func (hc *hostChecks) option(ctx context.Context, label string, h RepoHost) Option {
	hc.probe(ctx)
	o := Option{Label: label}
	if err := hc.err[h.Name()]; err != nil {
		var hu *HostUnavailable
		if errors.As(err, &hu) && hu.CanSignIn {
			o.Note = "not signed in; it signs you in first"
		} else {
			o.Disabled, o.Note = true, err.Error()
		}
	}
	return o
}

// ready makes the service usable, signing in when that is all it needs.
func (hc *hostChecks) ready(ctx context.Context, h RepoHost) error {
	hc.probe(ctx)
	err := hc.err[h.Name()]
	var hu *HostUnavailable
	if err == nil || !errors.As(err, &hu) || !hu.CanSignIn {
		return err
	}
	hc.c.Out.Info("signing in to %s: follow what it shows, then come back here", h.Name())
	if err := h.Login(ctx); err != nil {
		return err
	}
	hc.err[h.Name()] = h.Available(ctx)
	return hc.err[h.Name()]
}

// pick is a repository chosen for the multiverse; co is set when it is already
// cloned on this machine.
type pick struct {
	url string
	co  *FoundCheckout
}

type picks struct{ list []pick }

func (p *picks) add(url string, co *FoundCheckout) {
	for i, have := range p.list {
		if sameRepo(have.url, url) {
			if have.co == nil && co != nil {
				p.list[i].co = co
			}
			return
		}
	}
	p.list = append(p.list, pick{url, co})
}

// InitProject sets a multiverse up the way `firebase init` would: it gathers the
// repositories, writes a manifest and a catalog into Dir, puts them in a
// repository of their own, and lists that in the root catalog.
func (c *Catalogs) InitProject(ctx context.Context, in ProjectInput) (ProjectResult, error) {
	if c.Writer == nil {
		return ProjectResult{}, errors.New("no manifest writer configured")
	}
	dir, err := filepath.Abs(firstOf(in.Dir, "."))
	if err != nil {
		return ProjectResult{}, err
	}
	if len(manifestsIn(c.Files, dir)) > 0 || c.Files.Exists(filepath.Join(dir, defaultCatalogFile)) {
		return ProjectResult{}, fmt.Errorf("%s already defines a multiverse or a catalog: edit those, or list it with `catalog add`", dir)
	}

	base, found := c.ScanCheckouts(ctx, dir)
	hosts := c.newHostChecks()
	chosen := &picks{}
	for _, r := range in.Repos {
		chosen.add(c.expand(r), nil)
	}
	if in.Scan {
		for i := range found {
			chosen.add(found[i].URL, &found[i])
		}
	}
	if len(in.Repos) == 0 && !in.Scan {
		if err := c.askRepos(ctx, hosts, chosen, found, base); err != nil && !errors.Is(err, ErrNotInteractive) {
			return ProjectResult{}, err
		}
	}
	if len(chosen.list) == 0 {
		return ProjectResult{}, errors.New("no repositories: choose some in the guide, or pass --repo <url> (repeatable) or --scan")
	}

	name, err := c.projectName(in.Name, dir, chosen)
	if err != nil {
		return ProjectResult{}, err
	}
	draft, root := c.draft(name, base, chosen)
	data, err := c.Writer.Multiverse(draft)
	if err != nil {
		return ProjectResult{}, err
	}

	target, err := c.projectTarget(ctx, hosts, in, dir)
	if err != nil {
		return ProjectResult{}, err
	}

	c.Out.Plain("")
	c.Out.Plain("Multiverse %s, in `%s`", name, dir)
	width := 0
	for _, r := range draft.Repos {
		width = max(width, len(r.Key))
	}
	for _, r := range draft.Repos {
		c.Out.Plain("  %-*s `%s`", width, r.Key, r.URL)
	}
	c.Out.Plain("  repository:    %s", target.describe())
	if ok, err := c.confirm(target.question(dir), true); err != nil {
		return ProjectResult{}, err
	} else if !ok {
		return ProjectResult{}, ErrCancelled
	}

	res := ProjectResult{Name: name, Dir: dir, Root: root}
	if res.URL, err = c.publishProject(ctx, dir, name, data, draft, target); err != nil {
		return res, err
	}
	if res.URL == "" || in.NoLink {
		return res, nil
	}
	cfg, err := c.Config.Load()
	if err != nil {
		return res, err
	}
	if cfg.Catalog == "" {
		c.Out.Info("no root catalog to list it in: `multiversectl init`, then `multiversectl catalog add %s`", res.URL)
		return res, nil
	}
	res.Node, err = c.Add(ctx, AddInput{Repo: res.URL, At: in.At, AtGiven: in.AtGiven})
	return res, err
}

// projectName is asked unless given, once the repositories are known: what
// they share (fichero-api and fichero-ui are fichero) is the likeliest name.
// Failing that, the directory's; one called multiverse says nothing about the
// multiverse, so its parent's is used.
func (c *Catalogs) projectName(given, dir string, p *picks) (string, error) {
	name := given
	if name == "" {
		def := commonPrefix(p)
		if def == "" {
			def = filepath.Base(dir)
			if strings.HasPrefix(def, "multiverse") {
				def = filepath.Base(filepath.Dir(dir))
			}
		}
		def = strings.ToLower(nonName.ReplaceAllString(def, "-"))
		var err error
		if name, err = c.Prompt.Input("Multiverse name", def); errors.Is(err, ErrNotInteractive) {
			name, err = def, nil
		} else if err != nil {
			return "", err
		}
	}
	if err := domain.ValidateCatalogName(name); err != nil {
		return "", fmt.Errorf("%w (--name sets it)", err)
	}
	return name, nil
}

// commonPrefix is the first word, up to a dash, that most of the repositories'
// names start with (fichero-api, fichero-ui, fichero-admin): that is the likely
// name. A stray one such as .skills does not stop it. It needs two repositories
// that agree, and more than half.
func commonPrefix(p *picks) string {
	count := map[string]int{}
	for _, pk := range p.list {
		head, _, _ := strings.Cut(strings.ToLower(repoBase(pk.url)), "-")
		count[head]++
	}
	best, n := "", 0
	for head, k := range count {
		if k > n || (k == n && head < best) {
			best, n = head, k
		}
	}
	if n < 2 || n*2 <= len(p.list) || domain.ValidateCatalogName(best) != nil {
		return ""
	}
	return best
}

// repoKey turns a repository's name into a manifest key: lowercase letters,
// digits and dashes, starting with a letter (.skills is skills).
func repoKey(name string) string {
	key := strings.Trim(nonName.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if key == "" || key[0] < 'a' || key[0] > 'z' {
		key = "repo-" + key
	}
	return strings.TrimRight(key, "-")
}

var nonName = regexp.MustCompile(`[^a-zA-Z0-9-]+`)

// draft names each repository. A key drops the multiverse's own name when that
// leaves something (fichero-api in fichero is api), since it is typed often.
func (c *Catalogs) draft(project, base string, p *picks) (ProjectDraft, string) {
	d := ProjectDraft{Name: project}
	taken := map[string]bool{}
	root := ""
	for _, pk := range p.list {
		repo := repoBase(pk.url)
		key := repoKey(repo)
		if rest := strings.TrimPrefix(key, project+"-"); rest != "" && rest != key {
			key = rest
		}
		for n, k := 2, key; taken[key]; n++ {
			key = fmt.Sprintf("%s-%d", k, n)
		}
		taken[key] = true
		r := DraftRepo{Key: key, URL: pk.url, Dir: repo}
		if pk.co != nil {
			rel, err := filepath.Rel(base, pk.co.Dir)
			if err == nil {
				r.Dir, root = filepath.ToSlash(rel), base
			}
		}
		d.Repos = append(d.Repos, r)
	}
	return d, root
}

// repoBase is the repository's own name: the last part of its URL, without .git.
func repoBase(url string) string {
	u := strings.TrimSuffix(strings.TrimRight(url, "/"), ".git")
	if i := strings.LastIndexAny(u, "/:"); i >= 0 {
		u = u[i+1:]
	}
	return u
}

// sameRepo compares repositories by where they are, not by how they were
// written: the ssh and https forms of one repository are one.
func sameRepo(a, b string) bool { return canonical(a) == canonical(b) }

// samePath says two paths name the same directory. git reports a work tree's
// top level with symbolic links resolved, while a path from filepath.Abs is
// resolved logically, so comparing the text alone makes a directory reached
// through a link — macOS's /tmp, a symlinked home or work directory — look
// like a directory nested inside another repository.
func samePath(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ra, erra := filepath.EvalSymlinks(a)
	rb, errb := filepath.EvalSymlinks(b)
	return erra == nil && errb == nil && ra == rb
}

// relWithin is dir's path inside top, for the same reason: comparing the two
// forms directly would answer with a ../ path that leaves the repository.
func relWithin(top, dir string) (string, error) {
	rel, err := filepath.Rel(top, dir)
	if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return rel, nil
	}
	rt, terr := filepath.EvalSymlinks(top)
	rd, derr := filepath.EvalSymlinks(dir)
	if terr != nil || derr != nil {
		return rel, err
	}
	return filepath.Rel(rt, rd)
}

func canonical(url string) string {
	u := strings.ToLower(strings.TrimSpace(url))
	for _, p := range []string{"https://", "http://", "ssh://", "git://"} {
		u = strings.TrimPrefix(u, p)
	}
	if i := strings.Index(u, "@"); i >= 0 {
		u = u[i+1:]
	}
	u = strings.Replace(u, ":", "/", 1)
	return strings.TrimSuffix(strings.TrimRight(u, "/"), ".git")
}

// askRepos is the guide's repository step: pick from what is already cloned
// here, search an account on a hosting service, or paste URLs, until done.
func (c *Catalogs) askRepos(ctx context.Context, hosts *hostChecks, chosen *picks, found []FoundCheckout, base string) error {
	usedScan := false
	for {
		var opts []Option
		var run []func() error
		add := func(o Option, f func() error) { opts, run = append(opts, o), append(run, f) }

		if len(found) > 0 && !usedScan {
			add(Option{Label: fmt.Sprintf("Use the git checkouts found in %s", base), Note: fmt.Sprintf("%d found", len(found))}, func() error {
				usedScan = true
				return c.pickCheckouts(chosen, found)
			})
		}
		for _, h := range c.Hosts {
			h := h
			add(hosts.option(ctx, "Search my repositories on "+h.Name(), h), func() error {
				if err := hosts.ready(ctx, h); err != nil {
					return err
				}
				return c.searchRepos(ctx, h, chosen)
			})
		}
		add(Option{Label: "Paste repository URLs", Note: "git, https or owner/repo, separated by spaces"}, func() error { return c.pasteRepos(chosen) })
		done := len(opts)
		add(Option{Label: fmt.Sprintf("Done, with %d repositories", len(chosen.list))}, nil)

		def := 0
		if len(chosen.list) > 0 {
			def = done
		}
		i, err := c.Prompt.Select("Which repositories make up the multiverse?", opts, def)
		if err != nil {
			return err
		}
		if i == done {
			if len(chosen.list) == 0 {
				c.Out.Warn("choose at least one repository")
				continue
			}
			return nil
		}
		if err := run[i](); err != nil {
			if errors.Is(err, ErrNotInteractive) || errors.Is(err, context.Canceled) {
				return err
			}
			c.Out.Warn("%v", err)
		}
	}
}

func (c *Catalogs) pickCheckouts(chosen *picks, found []FoundCheckout) error {
	opts := make([]Option, len(found))
	for i, co := range found {
		note := co.URL
		if !co.Compose {
			note += "  (no compose file)"
		}
		opts[i] = Option{Label: filepath.Base(co.Dir), Note: note, Checked: true}
	}
	idx, err := c.Prompt.MultiSelect("Checkouts to include", opts)
	if err != nil {
		return err
	}
	for _, i := range idx {
		chosen.add(found[i].URL, &found[i])
	}
	return nil
}

func (c *Catalogs) pasteRepos(chosen *picks) error {
	text, err := c.Prompt.Input("Repositories", "")
	if err != nil {
		return err
	}
	for _, f := range strings.FieldsFunc(text, func(r rune) bool { return r == ' ' || r == ',' || r == '\n' || r == '\t' }) {
		chosen.add(c.expand(f), nil)
	}
	return nil
}

func (c *Catalogs) searchRepos(ctx context.Context, h RepoHost, chosen *picks) error {
	account, err := c.pickAccount(ctx, h, "Search which account or organization on "+h.Name()+"?")
	if err != nil {
		return err
	}
	var repos []RemoteRepo
	err = c.Progress.Do("Loading the repositories of "+account, func(ctx context.Context) (err error) {
		repos, err = h.Repos(ctx, account)
		return err
	})
	if err != nil {
		return err
	}
	if len(repos) == 0 {
		return fmt.Errorf("%s has no repository you can see", account)
	}
	match := repos
	opts := make([]Option, len(match))
	for i, r := range match {
		note := r.Description
		if r.Private {
			note = strings.TrimSpace("private  " + note)
		}
		opts[i] = Option{Label: r.Name, Note: note}
	}
	idx, err := c.Prompt.MultiSelect(fmt.Sprintf("Repositories of %s to include  (type / to filter)", account), opts)
	if err != nil {
		return err
	}
	for _, i := range idx {
		chosen.add(match[i].URL, nil)
	}
	return nil
}

// pickAccount lets a person choose where on a service, defaulting to their own.
func (c *Catalogs) pickAccount(ctx context.Context, h RepoHost, label string) (string, error) {
	var accounts []Account
	err := c.Progress.Do("Loading your accounts on "+h.Name(), func(ctx context.Context) (err error) {
		accounts, err = h.Accounts(ctx)
		return err
	})
	if err != nil {
		return "", fmt.Errorf("%s: %w", h.Name(), err)
	}
	if len(accounts) == 0 {
		return "", fmt.Errorf("%s reports no account", h.Name())
	}
	opts := make([]Option, len(accounts))
	for i, a := range accounts {
		opts[i] = Option{Label: a.Name, Note: map[bool]string{true: "organization", false: "you"}[a.Org]}
	}
	i, err := c.Prompt.Select(label, opts, 0)
	if err != nil {
		return "", err
	}
	return accounts[i].Name, nil
}

// projectHome is where the multiverse's own repository lives.
type projectHome struct {
	url    string   // an existing repository, or the origin Dir already has
	create string   // owner/name of one to make
	host   RepoHost // the service create is on
	public bool
	local  bool
}

func (t projectHome) describe() string {
	switch {
	case t.local:
		return "only on this machine for now"
	case t.create != "":
		vis := "private"
		if t.public {
			vis = "PUBLIC"
		}
		return fmt.Sprintf("a new %s repository `%s` on %s", vis, t.create, t.host.Name())
	}
	return "`" + t.url + "`"
}

func (t projectHome) question(dir string) string {
	switch {
	case t.local:
		return fmt.Sprintf("Write multiverse.yaml and AGENTS.md in %s and commit them?", dir)
	case t.create != "":
		return fmt.Sprintf("Create %s on %s, write multiverse.yaml and AGENTS.md in %s, commit and push them?", t.create, t.host.Name(), dir)
	}
	return fmt.Sprintf("Write multiverse.yaml and AGENTS.md in %s, commit them and push to %s?", dir, t.url)
}

// projectTarget decides where the multiverse's repository lives, from what was
// said, from the origin Dir already has, or by asking.
func (c *Catalogs) projectTarget(ctx context.Context, hosts *hostChecks, in ProjectInput, dir string) (projectHome, error) {
	switch {
	case in.Local:
		return projectHome{local: true}, nil
	case in.URL != "":
		return projectHome{url: c.expand(in.URL)}, nil
	case in.Create != "":
		h, err := c.hostNamed(in.Provider)
		return projectHome{create: in.Create, host: h, public: in.Public}, err
	}

	origin := ""
	if top, err := c.Git.Toplevel(ctx, dir); err == nil && samePath(top, dir) {
		origin, _ = c.Git.RemoteURL(ctx, dir)
	}
	var opts []Option
	var homes []func() (projectHome, error)
	add := func(o Option, f func() (projectHome, error)) { opts, homes = append(opts, o), append(homes, f) }
	if origin != "" {
		add(Option{Label: "This directory's own repository", Note: origin}, func() (projectHome, error) { return projectHome{url: origin}, nil })
	}
	for _, h := range c.Hosts {
		h := h
		add(hosts.option(ctx, "A new private repository on "+h.Name(), h), func() (projectHome, error) {
			if err := hosts.ready(ctx, h); err != nil {
				return projectHome{}, err
			}
			account, err := c.pickAccount(ctx, h, "Create it under which account or organization?")
			if err != nil {
				return projectHome{}, err
			}
			repo, err := c.Prompt.Input("Repository name", "multiverse")
			if err != nil {
				return projectHome{}, err
			}
			return projectHome{create: account + "/" + repo, host: h, public: in.Public}, nil
		})
	}
	add(Option{Label: "An existing repository", Note: "a URL, or owner/repo on GitHub"}, func() (projectHome, error) {
		url, err := c.Prompt.Input("Repository", "")
		if err != nil || url == "" {
			return projectHome{}, firstErr(err, errors.New("a repository is required"))
		}
		return projectHome{url: c.expand(url)}, nil
	})
	add(Option{Label: "Only on this machine for now", Note: "a git repository to push somewhere later"}, func() (projectHome, error) { return projectHome{local: true}, nil })

	i, err := c.Prompt.Select("Where should the multiverse's own repository live?", opts, 0)
	if errors.Is(err, ErrNotInteractive) {
		if origin != "" {
			return projectHome{url: origin}, nil
		}
		return projectHome{}, errors.New("say where the multiverse's repository lives: --create owner/name, --url <repository>, or --local")
	}
	if err != nil {
		return projectHome{}, err
	}
	return homes[i]()
}

func (c *Catalogs) hostNamed(name string) (RepoHost, error) {
	if len(c.Hosts) == 0 {
		return nil, errors.New("no hosting service is configured")
	}
	if name == "" {
		return c.Hosts[0], nil
	}
	var names []string
	for _, h := range c.Hosts {
		if strings.EqualFold(h.Name(), name) {
			return h, nil
		}
		names = append(names, strings.ToLower(h.Name()))
	}
	return nil, fmt.Errorf("no hosting service %q; known: %s", name, strings.Join(names, ", "))
}

// publishProject makes the repository if asked, writes the two files, commits
// them and pushes. It returns the repository's URL, "" for a local-only one.
func (c *Catalogs) publishProject(ctx context.Context, dir, name string, manifest []byte, draft ProjectDraft, t projectHome) (string, error) {
	url := t.url
	if t.create != "" {
		if err := t.host.Available(ctx); err != nil {
			return "", fmt.Errorf("cannot create a repository on %s: %w; sign in with %s, or pass --url with one you made", t.host.Name(), err, signInCommand(t.host))
		}
		err := c.Progress.Do("Creating "+t.create+" on "+t.host.Name(), func(ctx context.Context) (err error) {
			url, err = t.host.Create(ctx, t.create, !t.public)
			return err
		})
		if err != nil {
			return "", err
		}
		c.Out.OK("created `%s`", url)
	}

	if err := c.Files.MkdirAll(dir); err != nil {
		return "", err
	}
	if top, err := c.Git.Toplevel(ctx, dir); err != nil {
		if err := c.Git.Init(ctx, dir); err != nil {
			return "", err
		}
	} else if !samePath(top, dir) {
		return "", fmt.Errorf("%s is inside the git repository %s; a multiverse's repository is its own directory", dir, top)
	}
	if url != "" {
		if have, err := c.Git.RemoteURL(ctx, dir); err == nil && have != "" {
			if !sameRepo(have, url) {
				return "", fmt.Errorf("%s already has origin %s, not %s", dir, have, url)
			}
		} else if err := c.Git.AddRemote(ctx, dir, "origin", url); err != nil {
			return "", err
		}
	}

	manifestFile := filepath.Join(dir, defaultManifestFile)
	if err := c.Files.WriteFile(manifestFile, manifest, 0o644); err != nil {
		return "", err
	}
	if _, err := c.Manifests.Load(manifestFile); err != nil {
		_ = c.Files.RemoveAll(manifestFile)
		return "", fmt.Errorf("the manifest written does not load, which is a bug: %w", err)
	}
	files := []string{manifestFile}
	agentsFile := filepath.Join(dir, "AGENTS.md")
	if c.Files.Exists(agentsFile) {
		c.Out.Info("AGENTS.md is already here; it is kept as it is")
	} else {
		agents, err := c.Writer.Agents(draft, firstOf(url, dir))
		if err != nil {
			return "", err
		}
		if err := c.Files.WriteFile(agentsFile, agents, 0o644); err != nil {
			return "", err
		}
		files = append(files, agentsFile)
	}
	if err := c.Git.Commit(ctx, dir, "Start the "+name+" multiverse with multiversectl", files...); err != nil {
		return "", fmt.Errorf("the files are written in %s but could not be committed; commit and push them yourself: %w", dir, err)
	}
	c.Out.OK("wrote %s and AGENTS.md", defaultManifestFile)
	if url == "" {
		c.Out.Info("only on this machine: `git -C %s remote add origin <url> && git -C %s push -u origin HEAD`", dir, dir)
		return "", nil
	}
	branch, err := c.Git.CurrentBranch(ctx, dir)
	if err != nil {
		return "", err
	}
	if err := c.Progress.Do("Pushing to "+url, func(ctx context.Context) error { return c.Git.Push(ctx, dir, branch) }); err != nil {
		return "", fmt.Errorf("push to %s: %w\n  the files are committed in %s; once access is sorted: `git -C %s push -u origin %s`", url, err, dir, dir, branch)
	}
	c.Out.OK("pushed to `%s`", url)
	return url, nil
}

// signInCommand is what a person types to sign in to the service themselves.
func signInCommand(h RepoHost) string {
	if strings.EqualFold(h.Name(), "GitLab") {
		return "glab auth login"
	}
	return "gh auth login"
}
