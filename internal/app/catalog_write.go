package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// ErrCancelled is returned when the user declines a confirmation. Nothing has
// been written by then.
var ErrCancelled = errors.New("cancelled")

const (
	defaultManifestFile = "multiverse.yaml"
	catalogsDir         = "catalogs"
	fallbackBranch      = "main"
)

func (c *Catalogs) resolver(ctx context.Context, refresh bool) *resolver {
	return &resolver{Catalogs: c, ctx: ctx, refresh: refresh, fetched: map[string]bool{}}
}

func (c *Catalogs) confirm(label string, def bool) (bool, error) {
	if c.Yes {
		return true, nil
	}
	ok, err := c.Prompt.Confirm(label, def)
	if errors.Is(err, ErrNotInteractive) {
		return false, fmt.Errorf("%w (--yes accepts it)", err)
	}
	return ok, err
}

// pickCatalog chooses the catalog a new entry goes into. A tree with one
// readable catalog needs no question.
func (c *Catalogs) pickCatalog(tree domain.Node, label string) (domain.Node, error) {
	var options []domain.Node
	var walk func(n domain.Node)
	walk = func(n domain.Node) {
		if n.Kind != domain.CatalogNode || n.Unavailable != "" {
			return
		}
		options = append(options, n)
		for _, ch := range n.Children {
			walk(ch)
		}
	}
	walk(tree)
	// --yes means the defaults, and the root is the default home for an entry.
	if len(options) == 1 || c.Yes {
		return options[0], nil
	}
	labels := make([]string, len(options))
	for i, n := range options {
		labels[i] = "(root) " + n.Name
		if n.Path != "" {
			labels[i] = n.Path
		}
	}
	i, err := c.Prompt.Select(label, labels, 0)
	if errors.Is(err, ErrNotInteractive) {
		return domain.Node{}, fmt.Errorf("%w (--at <path> names the catalog)", err)
	}
	if err != nil {
		return domain.Node{}, err
	}
	return options[i], nil
}

// parentCatalog finds the catalog node an entry is added to: the one at, or
// one picked from the tree. "/" is the root.
func (c *Catalogs) parentCatalog(tree domain.Node, at string, atGiven bool) (domain.Node, error) {
	if !atGiven {
		return c.pickCatalog(tree, "Which catalog should it go in?")
	}
	n, ok := tree.Find(strings.Trim(at, "/"))
	switch {
	case !ok:
		return domain.Node{}, fmt.Errorf("no catalog at %q; see `catalog tree`", at)
	case n.Kind != domain.CatalogNode:
		return domain.Node{}, fmt.Errorf("%q is a workspace, not a catalog", at)
	case n.Unavailable != "":
		return domain.Node{}, fmt.Errorf("catalog %q is unavailable: %s", at, n.Unavailable)
	}
	return n, nil
}

// fileChange is one file an edit writes.
type fileChange struct {
	File string
	Data []byte
}

// edit applies fn to the catalog file of parent, writes what it returns, and
// commits it. In a remote repository it then pushes; the user is asked once,
// before anything is written, so declining leaves no half-made commit behind.
func (c *Catalogs) edit(ctx context.Context, parent domain.Node, summary string, fn func(data []byte) ([]fileChange, error)) error {
	data, err := c.Files.ReadFile(parent.File)
	if err != nil {
		return err
	}
	changes, err := fn(data)
	if err != nil {
		return err
	}

	remote := parent.Source.Repo != ""
	var branch, where string
	if remote {
		if branch, err = c.writableBranch(ctx, parent); err != nil {
			return err
		}
		if clean, err := c.Git.IsClean(ctx, parent.Dir); err != nil || !clean {
			return fmt.Errorf("the local copy of %s has uncommitted changes (%s); resolve them first", parent.Source.Repo, parent.Dir)
		}
		where = fmt.Sprintf("commit and push to %s (%s)", parent.Source.Repo, branch)
	} else if _, terr := c.Git.Toplevel(ctx, parent.Dir); terr == nil {
		where = "write and commit in " + parent.Dir
	} else {
		where = "write " + parent.File + " (not in a git repository, so nothing is committed)"
	}
	if ok, err := c.confirm(fmt.Sprintf("%s: %s?", summary, where), true); err != nil {
		return err
	} else if !ok {
		return ErrCancelled
	}

	paths := make([]string, 0, len(changes))
	for _, ch := range changes {
		if err := c.Files.MkdirAll(filepath.Dir(ch.File)); err != nil {
			return err
		}
		if err := c.Files.WriteFile(ch.File, ch.Data, 0o644); err != nil {
			return err
		}
		paths = append(paths, ch.File)
	}

	if !remote {
		if _, terr := c.Git.Toplevel(ctx, parent.Dir); terr != nil {
			c.Out.OK("wrote %s", parent.File)
			return nil
		}
		if err := c.Git.Commit(ctx, parent.Dir, "fix: "+summary, paths...); err != nil {
			return fmt.Errorf("written, but not committed: %w", err)
		}
		c.Out.OK("%s", summary)
		if c.Git.HasRemote(ctx, parent.Dir) {
			c.Out.Info("committed locally; push %s when you are ready", parent.Dir)
		}
		return nil
	}

	if err := c.Git.Commit(ctx, parent.Dir, "fix: "+summary, paths...); err != nil {
		return err
	}
	if err := c.Git.Push(ctx, parent.Dir, branch); err != nil {
		// Put the clone back on what the remote has, or the next write would
		// build on a commit that was never accepted.
		_ = c.Git.Fetch(ctx, parent.Dir)
		_ = c.Git.Checkout(ctx, parent.Dir, branch)
		return fmt.Errorf("push to %s was refused (someone else may have pushed first); run the command again: %w", parent.Source.Repo, err)
	}
	c.Out.OK("%s", summary)
	return nil
}

// writableBranch is the branch a write to a remote catalog lands on. A tag or
// commit cannot be written to, and a reference pinned to one is read-only.
func (c *Catalogs) writableBranch(ctx context.Context, n domain.Node) (string, error) {
	if n.Source.Ref == "" {
		return c.Git.DefaultBranch(ctx, n.Dir)
	}
	kind, err := c.Git.RefExists(ctx, n.Dir, n.Source.Ref)
	if err != nil {
		return "", err
	}
	if kind == RefNone {
		return "", fmt.Errorf("%s is pinned to %q, which is not a branch, so it is read-only", n.Source.Repo, n.Source.Ref)
	}
	return n.Source.Ref, nil
}

// Clone brings a repository into the cache at ref, ready to be read.
func (c *Catalogs) Clone(ctx context.Context, repo, ref string) (string, error) {
	return c.resolver(ctx, true).checkout(repo, ref)
}

// Remove drops the entry at nodePath from the catalog that holds it. The
// repository or file it pointed at is left alone.
func (c *Catalogs) Remove(ctx context.Context, nodePath string) error {
	tree, err := c.Tree(ctx, "", true)
	if err != nil {
		return err
	}
	nodePath = strings.Trim(nodePath, "/")
	if _, ok := tree.Find(nodePath); !ok || nodePath == "" {
		return fmt.Errorf("no entry at %q; see `catalog tree` (the root itself cannot be removed)", nodePath)
	}
	parentPath := path.Dir(nodePath)
	if parentPath == "." {
		parentPath = ""
	}
	parent, ok := tree.Find(parentPath)
	if !ok || parent.Unavailable != "" {
		return fmt.Errorf("the catalog that holds %q cannot be read", nodePath)
	}
	name := path.Base(nodePath)
	return c.edit(ctx, parent, fmt.Sprintf("remove %s from the catalog", nodePath), func(data []byte) ([]fileChange, error) {
		out, err := c.Editor.RemoveRef(data, name)
		return []fileChange{{parent.File, out}}, err
	})
}

// AddWorkspaceInput is a repository to list in a catalog.
type AddWorkspaceInput struct {
	Repo, Ref, Path string
	Name            string
	At              string
	AtGiven         bool
}

// AddWorkspace lists a repository's multiverse.yaml in a catalog, taking its
// name from the manifest when none is given.
func (c *Catalogs) AddWorkspace(ctx context.Context, in AddWorkspaceInput) (string, error) {
	if in.Repo == "" {
		return "", errors.New("add workspace: a repository URL is required")
	}
	in.Repo = c.expand(in.Repo)
	tree, err := c.Tree(ctx, "", true)
	if err != nil {
		return "", err
	}
	dir, err := c.Clone(ctx, in.Repo, in.Ref)
	if err != nil {
		return "", err
	}
	file, err := findManifest(c.Files, dir, in.Path)
	if err != nil {
		return "", err
	}
	data, err := c.Files.ReadFile(filepath.Join(dir, filepath.FromSlash(file)))
	if err != nil {
		return "", err
	}
	name := in.Name
	if name == "" {
		detected, err := c.Editor.WorkspaceName(data)
		if err != nil {
			return "", fmt.Errorf("%s in %s: %w", file, in.Repo, err)
		}
		name = detected
		if !c.Yes {
			if name, err = c.Prompt.Input("Name in the catalog", detected); err != nil {
				return "", err
			}
		}
	}
	parent, err := c.parentCatalog(tree, in.At, in.AtGiven)
	if err != nil {
		return "", err
	}
	if p, ok := existingRef(parent, name, in.Repo); ok {
		c.Out.Info("%s is already in the catalog", p)
		return p, nil
	}
	src := domain.Source{Repo: in.Repo, Ref: in.Ref, Path: file}
	err = c.edit(ctx, parent, fmt.Sprintf("add workspace %s", name), func(cat []byte) ([]fileChange, error) {
		out, err := c.Editor.AddWorkspaceRef(cat, domain.WorkspaceRef{Name: name, Source: src})
		return []fileChange{{parent.File, out}}, err
	})
	return domain.ChildPath(parent.Path, name), err
}

// findManifest looks for the manifest the user named, or the conventional
// file names at the repository root.
func findManifest(files Files, dir, wanted string) (string, error) {
	candidates := []string{wanted}
	if wanted == "" {
		candidates = []string{"multiverse.yaml", "multiverse.yml"}
	}
	for _, name := range candidates {
		if files.Exists(filepath.Join(dir, filepath.FromSlash(name))) {
			return name, nil
		}
	}
	return "", fmt.Errorf("no %s in the repository; pass --path if the manifest lives elsewhere", strings.Join(candidates, " or "))
}

// AddCatalogInput is another catalog to link into this one.
type AddCatalogInput struct {
	Repo, Ref, Path string
	Name            string
	At              string
	AtGiven         bool
}

// AddCatalog links a catalog from another repository, after checking that it
// really is one.
func (c *Catalogs) AddCatalog(ctx context.Context, in AddCatalogInput) (string, error) {
	if in.Repo == "" {
		return "", errors.New("add catalog: a repository URL is required")
	}
	in.Repo = c.expand(in.Repo)
	tree, err := c.Tree(ctx, "", true)
	if err != nil {
		return "", err
	}
	dir, err := c.Clone(ctx, in.Repo, in.Ref)
	if err != nil {
		return "", err
	}
	file := firstOf(in.Path, defaultCatalogFile)
	data, err := c.Files.ReadFile(filepath.Join(dir, filepath.FromSlash(file)))
	if err != nil {
		return "", fmt.Errorf("no %s in %s: %w", file, in.Repo, err)
	}
	cat, err := c.Decoder.Decode(data)
	if err != nil {
		return "", fmt.Errorf("%s in %s: %w", file, in.Repo, err)
	}
	name := firstOf(in.Name, cat.Name)
	parent, err := c.parentCatalog(tree, in.At, in.AtGiven)
	if err != nil {
		return "", err
	}
	if p, ok := existingRef(parent, name, in.Repo); ok {
		c.Out.Info("%s is already in the catalog", p)
		return p, nil
	}
	src := domain.Source{Repo: in.Repo, Ref: in.Ref, Path: file}
	err = c.edit(ctx, parent, fmt.Sprintf("link catalog %s", name), func(raw []byte) ([]fileChange, error) {
		out, err := c.Editor.AddCatalogRef(raw, domain.CatalogRef{Name: name, Source: src})
		return []fileChange{{parent.File, out}}, err
	})
	return domain.ChildPath(parent.Path, name), err
}

// RepoChoice says where a new catalog's file lives when it is not in the
// repository of the catalog above it.
type RepoChoice struct {
	URL    string // an existing repository
	Create string // or the name of a new one
	Public bool
}

// provision makes sure a repository holds a catalog.yaml and returns its URL.
// A repository that already has one is reused as it is.
func (c *Catalogs) provision(ctx context.Context, ch RepoChoice, catalogName string, sd *seed) (string, error) {
	url := ch.URL
	if ch.Create != "" {
		if err := c.Host.Available(ctx); err != nil {
			return "", fmt.Errorf("cannot create a repository: %w (or give --repo with one you made)", err)
		}
		visibility := "private"
		if ch.Public {
			visibility = "PUBLIC"
		}
		if ok, err := c.confirm(fmt.Sprintf("Create a %s repository %q on GitHub?", visibility, ch.Create), true); err != nil {
			return "", err
		} else if !ok {
			return "", ErrCancelled
		}
		var err error
		if url, err = c.Host.Create(ctx, ch.Create, !ch.Public); err != nil {
			return "", err
		}
		c.Out.OK("created %s", url)
	}

	dir, fresh, err := c.resolver(ctx, true).ensure(url, "")
	if err != nil {
		return "", err
	}
	file := filepath.Join(dir, defaultCatalogFile)
	if c.Files.Exists(file) {
		data, err := c.Files.ReadFile(file)
		if err != nil {
			return "", err
		}
		if _, err := c.Decoder.Decode(data); err != nil {
			return "", fmt.Errorf("%s in %s exists but is not a catalog: %w", defaultCatalogFile, url, err)
		}
		if sd != nil {
			return "", fmt.Errorf("%s already has a catalog; list the manifest in it with `catalog add workspace` instead", url)
		}
		c.Out.Info("%s already has a catalog; using it", url)
		return url, nil
	}

	branch, err := c.Git.DefaultBranch(ctx, dir)
	if err != nil {
		if !fresh {
			return "", err
		}
		branch = fallbackBranch // an empty repository has none yet
	}
	if err := domain.ValidateCatalogName(catalogName); err != nil {
		return "", fmt.Errorf("%w (--name sets it)", err)
	}
	data, paths, what := c.Editor.Scaffold(catalogName), []string{file}, defaultCatalogFile
	if sd != nil {
		manifestFile := filepath.Join(dir, defaultManifestFile)
		if c.Files.Exists(manifestFile) {
			return "", fmt.Errorf("%s already has a %s; it is not overwritten", url, defaultManifestFile)
		}
		var err error
		if data, err = c.Editor.AddWorkspaceRef(data, domain.WorkspaceRef{Name: sd.name, Source: domain.Source{Path: defaultManifestFile}}); err != nil {
			return "", err
		}
		if err := c.Files.WriteFile(manifestFile, sd.manifest, 0o644); err != nil {
			return "", err
		}
		paths, what = append(paths, manifestFile), defaultCatalogFile+" and "+defaultManifestFile
	}
	if ok, err := c.confirm(fmt.Sprintf("Add %s to %s and push it to %s?", what, url, branch), true); err != nil {
		return "", err
	} else if !ok {
		return "", ErrCancelled
	}
	if err := c.Files.WriteFile(file, data, 0o644); err != nil {
		return "", err
	}
	if err := c.Git.Commit(ctx, dir, "fix: start a catalog", paths...); err != nil {
		return "", err
	}
	if err := c.Git.Push(ctx, dir, branch); err != nil {
		return "", fmt.Errorf("push to %s: %w", url, err)
	}
	return url, nil
}

// CreateCatalogInput describes a new catalog and where it goes.
type CreateCatalogInput struct {
	Name    string
	At      string
	AtGiven bool
	Where   RepoChoice // zero value: ask, or the same repository when not interactive
	Same    bool       // keep it in the parent's repository

	// Manifest is a workspace manifest on this machine to start the catalog
	// with: copied in as multiverse.yaml and listed, so the project is its own
	// index and its own workspace.
	Manifest string
}

// CreateCatalog makes a catalog and links it from a parent: as a file in the
// parent's own repository, in an existing repository, or in a new one.
func (c *Catalogs) CreateCatalog(ctx context.Context, in CreateCatalogInput) error {
	if err := domain.ValidateCatalogName(in.Name); err != nil {
		return err
	}
	sd, err := c.loadSeed(in.Manifest)
	if err != nil {
		return err
	}
	tree, err := c.Tree(ctx, "", true)
	if err != nil {
		return err
	}
	parent, err := c.parentCatalog(tree, in.At, in.AtGiven)
	if err != nil {
		return err
	}
	for _, ch := range parent.Children {
		if ch.Name == in.Name {
			return fmt.Errorf("%q already exists in %s", in.Name, firstOf(parent.Path, "the root catalog"))
		}
	}

	where := in.Where
	where.URL = c.expand(where.URL)
	if !in.Same && where.URL == "" && where.Create == "" {
		if where, in.Same, err = c.askWhere(in.Name); err != nil {
			return err
		}
	}

	if in.Same || (where.URL == "" && where.Create == "") {
		rel := path.Join(catalogsDir, in.Name+".yaml")
		if sd != nil {
			// A project keeps its catalog and manifest side by side.
			rel = path.Join(catalogsDir, in.Name, defaultCatalogFile)
		}
		target := filepath.Join(parent.Dir, filepath.FromSlash(rel))
		if c.Files.Exists(target) {
			return fmt.Errorf("%s already exists", rel)
		}
		return c.edit(ctx, parent, fmt.Sprintf("add catalog %s", in.Name), func(data []byte) ([]fileChange, error) {
			out, err := c.Editor.AddCatalogRef(data, domain.CatalogRef{Name: in.Name, Source: domain.Source{Path: rel}})
			if err != nil {
				return nil, err
			}
			child := c.Editor.Scaffold(in.Name)
			changes := []fileChange{{parent.File, out}}
			if sd != nil {
				manifestRel := path.Join(catalogsDir, in.Name, defaultManifestFile)
				if child, err = c.Editor.AddWorkspaceRef(child, domain.WorkspaceRef{Name: sd.name, Source: domain.Source{Path: manifestRel}}); err != nil {
					return nil, err
				}
				changes = append(changes, fileChange{filepath.Join(parent.Dir, filepath.FromSlash(manifestRel)), sd.manifest})
			}
			return append(changes, fileChange{target, child}), nil
		})
	}

	url, err := c.provision(ctx, where, in.Name, sd)
	if err != nil {
		return err
	}
	return c.edit(ctx, parent, fmt.Sprintf("link catalog %s", in.Name), func(data []byte) ([]fileChange, error) {
		out, err := c.Editor.AddCatalogRef(data, domain.CatalogRef{Name: in.Name, Source: domain.Source{Repo: url}})
		return []fileChange{{parent.File, out}}, err
	})
}

func (c *Catalogs) askWhere(name string) (RepoChoice, bool, error) {
	i, err := c.Prompt.Select("Where should the new catalog live?", []string{
		"In the same repository as its parent",
		"In an existing repository",
		"In a new private repository on GitHub",
	}, 0)
	if errors.Is(err, ErrNotInteractive) {
		return RepoChoice{}, true, nil // scripts get the same repository, the least surprising place
	}
	if err != nil {
		return RepoChoice{}, false, err
	}
	switch i {
	case 1:
		url, err := c.Prompt.Input("Repository URL", "")
		if err != nil || url == "" {
			return RepoChoice{}, false, firstErr(err, errors.New("a repository URL is required"))
		}
		return RepoChoice{URL: url}, false, nil
	case 2:
		repo, err := c.Prompt.Input("New repository name", "multiverse-"+name)
		return RepoChoice{Create: repo}, false, err
	}
	return RepoChoice{}, true, nil
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

// InitInput says where this machine's root catalog comes from.
type InitInput struct {
	Name  string
	Where RepoChoice
	Local string // a directory or catalog file on this machine
	Only  bool   // keep it local and git-only, with no remote yet
}

// Init sets up the root catalog: an existing repository or file, a new
// private repository, or a local-only git repository to push somewhere later.
func (c *Catalogs) Init(ctx context.Context, in InitInput) error {
	cfg, err := c.Config.Load()
	if err != nil {
		return err
	}
	in.Where.URL = c.expand(in.Where.URL)
	if in.Where.URL == "" && in.Where.Create == "" && in.Local == "" && !in.Only {
		if in, err = c.askInit(in); err != nil {
			return err
		}
	}
	if cfg.Catalog != "" {
		if ok, err := c.confirm(fmt.Sprintf("This machine already uses %s as its root catalog. Replace it?", cfg.Catalog), false); err != nil {
			return err
		} else if !ok {
			return ErrCancelled
		}
	}

	name := in.Name
	var source string
	switch {
	case in.Where.URL != "" || in.Where.Create != "":
		name = firstOf(name, in.Where.Create, "index")
		if source, err = c.provision(ctx, in.Where, name, nil); err != nil {
			return err
		}
	case in.Local != "":
		if source, err = c.localRoot(ctx, in.Local, firstOf(name, "index")); err != nil {
			return err
		}
	default:
		dir := filepath.Join(filepath.Dir(c.Config.Path()), "index")
		if source, err = c.localRoot(ctx, dir, firstOf(name, "index")); err != nil {
			return err
		}
		c.Out.Info("to keep it elsewhere: git -C %s remote add origin <url> && git -C %s push -u origin HEAD", dir, dir)
	}
	return c.Use(source)
}

func (c *Catalogs) askInit(in InitInput) (InitInput, error) {
	i, err := c.Prompt.Select("Where does your root catalog live?", []string{
		"In an existing repository or file",
		"In a new private repository on GitHub",
		"Only on this machine for now (a local git repository)",
	}, 1)
	if errors.Is(err, ErrNotInteractive) {
		return in, fmt.Errorf("%w: use --repo <url>, --create <name> or --local", err)
	}
	if err != nil {
		return in, err
	}
	switch i {
	case 0:
		where, err := c.Prompt.Input("Repository URL or path to a catalog", "")
		if err != nil || where == "" {
			return in, firstErr(err, errors.New("a location is required"))
		}
		if isGitURL(where) {
			in.Where.URL = where
		} else {
			in.Local = where
		}
	case 1:
		in.Where.Create, err = c.Prompt.Input("New repository name", "multiverse-index")
		if err != nil {
			return in, err
		}
	default:
		in.Only = true
	}
	return in, nil
}

// localRoot makes a catalog file out of a directory or a path, starting a git
// repository for it when it is new so later edits can be committed.
func (c *Catalogs) localRoot(ctx context.Context, p, name string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	file := abs
	if info, serr := os.Stat(abs); serr != nil || info.IsDir() {
		file = filepath.Join(abs, defaultCatalogFile)
	}
	dir := filepath.Dir(file)
	if c.Files.Exists(file) {
		data, err := c.Files.ReadFile(file)
		if err != nil {
			return "", err
		}
		if _, err := c.Decoder.Decode(data); err != nil {
			return "", fmt.Errorf("%s is not a catalog: %w", file, err)
		}
		return file, nil
	}
	if err := domain.ValidateCatalogName(name); err != nil {
		return "", fmt.Errorf("%w (--name sets it)", err)
	}
	if ok, err := c.confirm(fmt.Sprintf("Create %s?", file), true); err != nil {
		return "", err
	} else if !ok {
		return "", ErrCancelled
	}
	if err := c.Files.MkdirAll(dir); err != nil {
		return "", err
	}
	if _, err := c.Git.Toplevel(ctx, dir); err != nil {
		if err := c.Git.Init(ctx, dir); err != nil {
			return "", err
		}
	}
	if err := c.Files.WriteFile(file, c.Editor.Scaffold(name), 0o644); err != nil {
		return "", err
	}
	if err := c.Git.Commit(ctx, dir, "fix: start a catalog", file); err != nil {
		c.Out.Warn("created %s but could not commit it: %v", file, err)
	}
	return file, nil
}

// existingRef finds an entry already listing this repository under the name,
// so adding the same thing twice is a no-op and not an error.
func existingRef(parent domain.Node, name, repo string) (string, bool) {
	for _, ch := range parent.Children {
		if ch.Name == name && ch.Source.Repo == repo {
			return ch.Path, true
		}
	}
	return "", false
}

var shorthandRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*/[A-Za-z0-9_.-]+$`)

// expand turns owner/repo into a GitHub URL, which is how people say it out
// loud. A path that exists on this machine stays a path.
func (c *Catalogs) expand(repo string) string {
	return expandShorthand(repo, c.Files.Exists(repo))
}

func expandShorthand(repo string, localPath bool) string {
	if localPath || isGitURL(repo) || !shorthandRE.MatchString(repo) {
		return repo
	}
	return "https://github.com/" + strings.TrimSuffix(repo, ".git") + ".git"
}

// AddInput is a repository to join, whatever it turns out to hold.
type AddInput struct {
	Repo, Ref, Path, Name string
	At                    string
	AtGiven               bool
}

// Add links a source into the catalog: a repository (owner/repo or a URL) or a
// path on this machine. A catalog.yaml makes it a catalog, otherwise it is a
// workspace. With no root catalog yet it sets
// one up first, so joining a project is one command on a fresh machine.
func (c *Catalogs) Add(ctx context.Context, in AddInput) (string, error) {
	if in.Repo == "" {
		return "", errors.New("add: a repository (owner/repo or a URL) or a path is required")
	}
	if c.Files.Exists(in.Repo) {
		return c.addLocal(ctx, in)
	}
	return c.addRemote(ctx, in)
}

// addRemote adds a repository by name. A checkout's origin can itself be a
// path, so this must not look at the disk again to decide what it was given.
func (c *Catalogs) addRemote(ctx context.Context, in AddInput) (string, error) {
	repo := c.expand(in.Repo)
	cfg, err := c.Config.Load()
	if err != nil {
		return "", err
	}
	if cfg.Catalog == "" {
		c.Out.Info("no root catalog on this machine yet; setting one up first")
		if err := c.Init(ctx, InitInput{}); err != nil {
			return "", err
		}
	}
	dir, err := c.Clone(ctx, repo, in.Ref)
	if err != nil {
		return "", err
	}
	if c.holdsCatalog(dir, in.Path) {
		return c.AddCatalog(ctx, AddCatalogInput{Repo: repo, Ref: in.Ref, Path: in.Path, Name: in.Name, At: in.At, AtGiven: in.AtGiven})
	}
	return c.AddWorkspace(ctx, AddWorkspaceInput{Repo: repo, Ref: in.Ref, Path: in.Path, Name: in.Name, At: in.At, AtGiven: in.AtGiven})
}

// holdsCatalog tells a catalog from a workspace manifest by what the file is,
// so --path needs no second flag to say which kind it points at.
func (c *Catalogs) holdsCatalog(dir, path string) bool {
	file := filepath.Join(dir, filepath.FromSlash(firstOf(path, defaultCatalogFile)))
	if !c.Files.Exists(file) {
		return false
	}
	if path == "" {
		return true
	}
	data, err := c.Files.ReadFile(file)
	if err != nil {
		return false
	}
	_, err = c.Decoder.Decode(data)
	return err == nil
}

// seed is a manifest a new catalog starts out listing.
type seed struct {
	name     string
	manifest []byte
}

// loadSeed reads the manifest to start a catalog with. Anything beside it
// that the manifest may rely on is not copied, and saying so now is better
// than a workspace that fails to start on someone else's machine.
func (c *Catalogs) loadSeed(file string) (*seed, error) {
	if file == "" {
		return nil, nil
	}
	data, err := c.Files.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	name, err := c.Editor.WorkspaceName(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	if err := domain.ValidateCatalogName(name); err != nil {
		return nil, fmt.Errorf("%s: metadata.name: %w", file, err)
	}
	if root := c.Editor.WorkspaceRoot(data); root != "" {
		c.Out.Warn("%s sets spec.root (%s), a path on this machine that install ignores; consider removing it", file, root)
	}
	dir, stem := filepath.Dir(file), strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
	for _, sibling := range []string{"overlays", stem} {
		if c.Files.Exists(filepath.Join(dir, sibling)) {
			c.Out.Warn("%s/ sits next to the manifest and is not copied; add it to the repository by hand", sibling)
		}
	}
	return &seed{name: name, manifest: data}, nil
}

// addLocal handles a path. A catalog entry is read by other machines, so a
// directory or a catalog file is named by the repository it belongs to; a
// workspace manifest file has no repository of its own to point at, so it is
// copied into the repository of the catalog it is listed in.
func (c *Catalogs) addLocal(ctx context.Context, in AddInput) (string, error) {
	abs, err := filepath.Abs(in.Repo)
	if err != nil {
		return "", err
	}
	if _, derr := c.Files.ListDirs(abs); derr != nil { // a file
		data, err := c.Files.ReadFile(abs)
		if err != nil {
			return "", err
		}
		if _, err := c.Decoder.Decode(data); err == nil {
			return c.addCheckout(ctx, in, filepath.Dir(abs), filepath.Base(abs))
		}
		return c.vendorManifest(ctx, in, abs)
	}
	return c.addCheckout(ctx, in, abs, "")
}

// addCheckout adds a directory of a local git checkout by its origin URL, with
// the path inside the repository worked out from where on disk it is.
func (c *Catalogs) addCheckout(ctx context.Context, in AddInput, dir, file string) (string, error) {
	top, err := c.Git.Toplevel(ctx, dir)
	if err != nil {
		return "", fmt.Errorf("%s is not in a git repository, so other machines could not fetch it; put it in one and push it", dir)
	}
	url, err := c.Git.RemoteURL(ctx, top)
	if err != nil || url == "" {
		return "", fmt.Errorf("%s has no origin remote, so other machines could not fetch it; publish it first (for example gh repo create --source %s --push) and add its URL", top, top)
	}
	if clean, err := c.Git.IsClean(ctx, top); err == nil && !clean {
		c.Out.Warn("%s has uncommitted changes; the catalog points at what is pushed to %s", top, url)
	}
	rel, err := filepath.Rel(top, dir)
	if err != nil {
		return "", err
	}
	path := in.Path
	if file != "" {
		path = file
	}
	if rel != "." {
		path = filepath.ToSlash(filepath.Join(rel, path))
	}
	c.Out.Info("%s is %s; adding that", in.Repo, url)
	next := in
	next.Repo, next.Path = url, path
	return c.addRemote(ctx, next)
}

// vendorManifest lists a manifest file by copying it into the repository of
// the parent catalog, next to nothing else it may depend on.
func (c *Catalogs) vendorManifest(ctx context.Context, in AddInput, file string) (string, error) {
	sd, err := c.loadSeed(file)
	if err != nil {
		return "", fmt.Errorf("%s is neither a catalog nor a workspace manifest: %w", file, err)
	}
	name := firstOf(in.Name, sd.name)
	if err := domain.ValidateCatalogName(name); err != nil {
		return "", err
	}
	cfg, err := c.Config.Load()
	if err != nil {
		return "", err
	}
	if cfg.Catalog == "" {
		c.Out.Info("no root catalog on this machine yet; setting one up first")
		if err := c.Init(ctx, InitInput{}); err != nil {
			return "", err
		}
	}
	tree, err := c.Tree(ctx, "", true)
	if err != nil {
		return "", err
	}
	parent, err := c.parentCatalog(tree, in.At, in.AtGiven)
	if err != nil {
		return "", err
	}
	for _, ch := range parent.Children {
		if ch.Name == name {
			c.Out.Info("%s is already in the catalog", ch.Path)
			return ch.Path, nil
		}
	}
	rel := path.Join("workspaces", name, defaultManifestFile)
	target := filepath.Join(parent.Dir, filepath.FromSlash(rel))
	if c.Files.Exists(target) {
		return "", fmt.Errorf("%s already exists in the catalog's repository", rel)
	}
	err = c.edit(ctx, parent, fmt.Sprintf("add workspace %s", name), func(data []byte) ([]fileChange, error) {
		out, err := c.Editor.AddWorkspaceRef(data, domain.WorkspaceRef{Name: name, Source: domain.Source{Path: rel}})
		return []fileChange{{parent.File, out}, {target, sd.manifest}}, err
	})
	return domain.ChildPath(parent.Path, name), err
}
