package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
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

// catalogsOnly is the tree without its projects: where a new entry can go.
func catalogsOnly(n domain.Node) domain.Node {
	out := n
	out.Children = nil
	for _, ch := range n.Children {
		if ch.Kind == domain.CatalogNode {
			out.Children = append(out.Children, catalogsOnly(ch))
		}
	}
	return out
}

// pickCatalog chooses the catalog a new entry goes into, from a drawing of the
// tree that says which repository each choice writes to. The default is the
// deepest catalog kept in a repository owned by the same owner as the source,
// since that is usually where a person would put it; else the root. Layers that
// could not be read are listed but cannot be chosen.
func (c *Catalogs) pickCatalog(tree domain.Node, label, sourceRepo string) (domain.Node, error) {
	rows := catalogsOnly(tree).Rows()
	usable := 0
	for _, r := range rows {
		if r.Node.Unavailable == "" {
			usable++
		}
	}
	// --yes means the defaults, and the root is the default home for an entry
	// in a script: it never writes to a repository nobody named.
	if usable == 1 || c.Yes {
		return tree, nil
	}
	owner := repoOwner(sourceRepo)
	opts, def, deepest := make([]Option, len(rows)), 0, -1
	for i, r := range rows {
		n := r.Node
		o := Option{Label: r.Prefix + n.Name}
		if n.Path == "" {
			o.Label = n.Name + " (root)"
		}
		if n.Unavailable != "" {
			o.Disabled, o.Note = true, n.Unavailable
		} else {
			o.Note = "writes to " + shortRepo(n.Source.Repo)
			if n.Source.Repo != tree.Source.Repo {
				o.Note += " · another repository"
			}
			if d := strings.Count(n.Path, "/"); owner != "" && repoOwner(n.Source.Repo) == owner && n.Path != "" && d > deepest {
				def, deepest = i, d
			}
		}
		opts[i] = o
	}
	i, err := c.Prompt.Select(label, opts, def)
	if errors.Is(err, ErrNotInteractive) {
		return domain.Node{}, fmt.Errorf("%w (--at <path> names the catalog, / is the root)", err)
	}
	if err != nil {
		return domain.Node{}, err
	}
	n, _ := tree.Find(rows[i].Node.Path)
	return n, nil
}

// repoOwner is the account a GitHub-style URL belongs to; empty for anything
// else, including a repository on this machine.
func repoOwner(url string) string {
	s := strings.TrimSuffix(url, ".git")
	switch {
	case strings.HasPrefix(s, "file://"):
		return ""
	case strings.Contains(s, "://"):
		s = s[strings.Index(s, "://")+3:]
		k := strings.Index(s, "/")
		if k < 0 {
			return ""
		}
		s = s[k+1:]
	case strings.Contains(s, "@") && strings.Contains(s, ":"):
		s = s[strings.Index(s, ":")+1:]
	default:
		return ""
	}
	if k := strings.Index(s, "/"); k > 0 {
		return s[:k]
	}
	return ""
}

// parentCatalog finds the catalog node an entry is added to: the one at, or
// one picked from the tree. "/" is the root.
func (c *Catalogs) parentCatalog(tree domain.Node, at string, atGiven bool, sourceRepo string) (domain.Node, error) {
	if !atGiven {
		return c.pickCatalog(tree, "Where should it go?", sourceRepo)
	}
	n, ok := tree.Find(strings.Trim(at, "/"))
	var problem string
	switch {
	case !ok:
		problem = fmt.Sprintf("no catalog at %q", at)
	case n.Kind != domain.CatalogNode:
		problem = fmt.Sprintf("%q is a project, not a catalog", at)
	case n.Unavailable != "":
		problem = fmt.Sprintf("catalog %q is unavailable: %s", at, n.Unavailable)
	default:
		return n, nil
	}
	// A place that does not exist is the moment to show the ones that do. With
	// someone to ask, say what was wrong and let them choose; in a script, or with
	// --yes, which must never guess where to write, fail and name the choices.
	if !c.Yes {
		picked, err := c.pickCatalog(tree, problem+". Where should it go?", sourceRepo)
		if !errors.Is(err, ErrNotInteractive) {
			return picked, err
		}
	}
	return domain.Node{}, fmt.Errorf("%s; the places are: %s", problem, Places(tree))
}

// Places lists the catalogs an entry can go in, by the path --at takes: / is the
// root. A layer that cannot be read is left out, since nothing can be written there.
func Places(tree domain.Node) string {
	var out []string
	for _, r := range catalogsOnly(tree).Rows() {
		switch {
		case r.Node.Unavailable != "":
			continue
		case r.Node.Path == "":
			out = append(out, "/ (the root)")
		default:
			out = append(out, r.Node.Path)
		}
	}
	return strings.Join(out, ", ")
}

// settle makes the entry's name free in parent. The same source already there
// under that name is returned as existing, so adding twice changes nothing; a
// different one under that name gets a suggested name, asked about only then.
// Listing a source somewhere else in the tree is asked about, since it is
// rarely meant.
func (c *Catalogs) settle(tree, parent domain.Node, name string, src domain.Source, named bool) (string, string, error) {
	for _, ch := range parent.Children {
		if ch.Name != name {
			continue
		}
		if sameSource(parent, ch.Source, src) {
			return name, ch.Path, nil
		}
		if named {
			return "", "", fmt.Errorf("%q already exists in %s (%s); pick another --name", name, firstOf(parent.Path, "the root catalog"), shortRepo(ch.Source.Repo))
		}
		suggestion := name
		for n := 2; hasChild(parent, suggestion); n++ {
			suggestion = fmt.Sprintf("%s-%d", name, n)
		}
		if c.Yes {
			c.Out.Info("%s already exists there (%s); using %s", name, shortRepo(ch.Source.Repo), suggestion)
			name = suggestion
			break
		}
		got, err := c.Prompt.Input(fmt.Sprintf("%q exists here (%s). Name", name, shortRepo(ch.Source.Repo)), suggestion)
		if errors.Is(err, ErrNotInteractive) {
			return "", "", fmt.Errorf("%q already exists in %s (%s): --name picks another", name, firstOf(parent.Path, "the root catalog"), shortRepo(ch.Source.Repo))
		}
		if err != nil {
			return "", "", err
		}
		if err := domain.ValidateCatalogName(got); err != nil {
			return "", "", err
		}
		if hasChild(parent, got) {
			return "", "", fmt.Errorf("%q exists here too", got)
		}
		name = got
		break
	}
	if at := findSource(tree, src); at != "" && src.Repo != "" {
		if c.Yes {
			c.Out.Warn("%s is already listed at %s; adding a second entry", shortRepo(src.Repo), at)
		} else if ok, err := c.Prompt.Confirm(fmt.Sprintf("%s is already listed at %s. Add a second entry?", shortRepo(src.Repo), at), false); err != nil {
			if errors.Is(err, ErrNotInteractive) {
				return "", "", fmt.Errorf("%s is already listed at %s; --yes adds a second entry anyway", shortRepo(src.Repo), at)
			}
			return "", "", err
		} else if !ok {
			return "", "", ErrCancelled
		}
	}
	return name, "", nil
}

// sameSource compares a listed entry with a source being added. In a catalog
// on this machine an entry's path is the absolute file, while the new source
// is relative to the catalog's repository, so that is worked out first.
func sameSource(parent domain.Node, listed, adding domain.Source) bool {
	if listed.Repo != adding.Repo {
		return false
	}
	if listed.Path == adding.Path {
		return true
	}
	return adding.Repo == "" && listed.Path == filepath.Join(parent.Dir, filepath.FromSlash(adding.Path))
}

func hasChild(n domain.Node, name string) bool {
	for _, ch := range n.Children {
		if ch.Name == name {
			return true
		}
	}
	return false
}

// findSource returns the path of a node already pointing at src, or "".
func findSource(tree domain.Node, src domain.Source) string {
	for _, r := range tree.Rows() {
		n := r.Node
		if n.Path != "" && n.Unavailable == "" && n.Source.Repo == src.Repo && n.Source.Path == src.Path {
			return n.Path
		}
	}
	return ""
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
	return c.editPlanned(ctx, parent, summary, editPlan{}, fn)
}

// editPlan is what an edit says about itself before it asks.
//
// An addition is easy to take back and a one-line question is enough for it. A
// removal from a catalog other people read is not: it is a commit pushed to a
// branch they update from, and what it changes for them is not in the question.
// So a careful edit says what goes, where it goes, what it leaves alone and who
// will notice — and the answer it takes by default is no.
type editPlan struct {
	// Careful puts the plan in front of the person and makes no the default.
	Careful bool
	// Lines are what this edit removes and leaves, in the words of the one
	// asking. Where it is written is added after them: only the edit knows.
	Lines []string
}

func (c *Catalogs) editPlanned(ctx context.Context, parent domain.Node, summary string, plan editPlan, fn func(data []byte) ([]fileChange, error)) error {
	remote := parent.Source.Repo != ""
	var branch, where string
	var err error
	if remote {
		if branch, err = c.writableBranch(ctx, parent); err != nil {
			return err
		}
		clean, err := c.Git.IsClean(ctx, parent.Dir)
		if err != nil {
			// Not the same thing as "it has changes": saying so sends the user
			// looking for edits that are not there, with the real cause gone.
			return fmt.Errorf("read the local copy of %s (%s): %w", parent.Source.Repo, parent.Dir, err)
		}
		if !clean {
			return fmt.Errorf("the local copy of %s has uncommitted changes (%s); resolve them first", parent.Source.Repo, parent.Dir)
		}
		// Only the repository being written to is brought up to date: the rest
		// of the tree is read from what was fetched earlier, so an edit does not
		// wait on every layer, or fail because one is unreachable.
		if err := c.Git.Fetch(ctx, parent.Dir); err != nil {
			return fmt.Errorf("fetch %s: %w", parent.Source.Repo, err)
		}
		if err := c.Git.Checkout(ctx, parent.Dir, branch); err != nil {
			return err
		}
		where = fmt.Sprintf("commit and push to %s (%s)", shortRepo(parent.Source.Repo), branch)
	} else if _, terr := c.Git.Toplevel(ctx, parent.Dir); terr == nil {
		where = "write and commit in " + parent.Dir
	} else {
		where = "write " + parent.File + " (not in a git repository, so nothing is committed)"
	}
	data, err := c.Files.ReadFile(parent.File)
	if err != nil {
		return err
	}
	changes, err := fn(data)
	if err != nil {
		return err
	}
	if plan.Careful {
		c.Out.Info("%s", summary)
		for _, l := range plan.Lines {
			c.Out.Info("  %s", l)
		}
		c.Out.Info("  %s", c.consequence(ctx, parent, remote))
	}
	if ok, err := c.confirm(fmt.Sprintf("%s: %s?", summary, where), !plan.Careful); err != nil {
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
		return fmt.Errorf("push to %s (%s) was rejected: %w\n  if someone pushed first, run the command again; if the branch is protected, ask its owner", shortRepo(parent.Source.Repo), branch, err)
	}
	c.Out.OK("%s", summary)
	return nil
}

// consequence is what writing there means for anybody else, in one sentence.
func (c *Catalogs) consequence(ctx context.Context, parent domain.Node, remote bool) string {
	switch {
	case remote:
		branch, _ := c.writableBranch(ctx, parent)
		return fmt.Sprintf("it is committed and pushed straight to %s of %s, not through a pull request, so "+
			"everyone who updates their catalog sees it gone; `git revert` on that commit brings it back",
			branch, shortRepo(parent.Source.Repo))
	}
	if _, err := c.Git.Toplevel(ctx, parent.Dir); err == nil {
		return "it is committed in " + parent.Dir + " and not pushed; `git revert` brings it back"
	}
	return "it is written to " + parent.File + ", which is not in a git repository, so nothing can bring it back"
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
	tree, err := c.Tree(ctx, "", false)
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
	target, _ := tree.Find(nodePath)
	plan := editPlan{Careful: true, Lines: removalLines(target)}
	var installed []string
	if cfg, err := c.Config.Load(); err == nil {
		for name, e := range cfg.Workspaces {
			if e.From == nodePath || strings.HasPrefix(e.From, nodePath+"/") {
				installed = append(installed, name)
			}
		}
		sort.Strings(installed)
	}
	if len(installed) > 0 {
		plan.Lines = append(plan.Lines, "installed here from it, and staying installed: "+strings.Join(installed, ", "))
	}
	if err := c.editPlanned(ctx, parent, fmt.Sprintf("remove %s from the catalog", nodePath), plan, func(data []byte) ([]fileChange, error) {
		out, err := c.Editor.RemoveRef(data, name)
		return []fileChange{{parent.File, out}}, err
	}); err != nil {
		return err
	}
	// Removing an entry does not touch what is installed from it.
	for _, name := range installed {
		c.Out.Info("still installed here: `multiversectl catalog uninstall %s`", name)
	}
	return nil
}

// removalLines is what taking a node out of a catalog removes, and what it does
// not. A catalog takes everything under it along, which is the part that is
// easy to miss when the name on the command line is one word.
func removalLines(n domain.Node) []string {
	if n.Kind != domain.CatalogNode {
		return []string{
			"removes: the entry " + n.Path + " (a project); the repository it points at is not touched",
		}
	}
	leaves := n.Leaves()
	names := make([]string, len(leaves))
	for i, l := range leaves {
		names[i] = l.Path
	}
	if len(names) == 0 {
		return []string{
			"removes: the entry " + n.Path + " (a catalog that lists no projects)",
			"leaves alone: the catalog file; only the link to it goes",
		}
	}
	list := ""
	if len(names) > 0 {
		shown := names
		more := ""
		if len(shown) > 5 {
			shown, more = shown[:5], fmt.Sprintf(", and %d more", len(names)-5)
		}
		noun := "projects"
		if len(names) == 1 {
			noun = "project"
		}
		list = fmt.Sprintf("%d %s: %s%s", len(names), noun, strings.Join(shown, ", "), more)
	}
	return []string{
		"removes: the entry " + n.Path + " (a catalog), and with it everything listed under it — " + list,
		"leaves alone: the catalog file and the repositories it points at; only the link to them goes",
	}
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
		return "", errors.New("add project: a repository URL is required")
	}
	in.Repo = c.expand(in.Repo)
	tree, err := c.Tree(ctx, "", false)
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
		if name, err = c.Editor.WorkspaceName(data); err != nil {
			return "", fmt.Errorf("%s in %s: %w", file, in.Repo, err)
		}
	}
	parent, err := c.parentCatalog(tree, in.At, in.AtGiven, in.Repo)
	if err != nil {
		return "", err
	}
	src := domain.Source{Repo: in.Repo, Ref: in.Ref, Path: file}
	name, existing, err := c.settle(tree, parent, name, src, in.Name != "")
	if err != nil {
		return "", err
	}
	if existing != "" {
		c.Out.Info("%s is already in the catalog", existing)
		return existing, nil
	}
	err = c.edit(ctx, parent, fmt.Sprintf("add project %s", name), func(cat []byte) ([]fileChange, error) {
		out, err := c.Editor.AddWorkspaceRef(cat, domain.WorkspaceRef{Name: name, Source: src})
		return []fileChange{{parent.File, out}}, err
	})
	return domain.ChildPath(parent.Path, name), err
}

// isDir says whether a path is a directory. Files has no stat of its own, and
// listing is the one call that tells a directory from a file.
func isDir(files Files, path string) bool {
	_, err := files.ListDirs(path)
	return err == nil
}

// findManifest looks for the manifest the user named, or the conventional
// file names at the repository root. What was named may be the directory the
// manifest sits in — --path, or a checkout added by one of its subdirectories,
// which becomes that subdirectory's path inside the repository — and reading a
// directory as the manifest failed with a raw "is a directory".
func findManifest(files Files, dir, wanted string) (string, error) {
	candidates := []string{wanted}
	switch {
	case wanted == "":
		candidates = []string{"multiverse.yaml", "multiverse.yml"}
	case isDir(files, filepath.Join(dir, filepath.FromSlash(wanted))):
		candidates = []string{path.Join(wanted, "multiverse.yaml"), path.Join(wanted, "multiverse.yml")}
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
	tree, err := c.Tree(ctx, "", false)
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
	parent, err := c.parentCatalog(tree, in.At, in.AtGiven, in.Repo)
	if err != nil {
		return "", err
	}
	src := domain.Source{Repo: in.Repo, Ref: in.Ref, Path: file}
	name, existing, err := c.settle(tree, parent, firstOf(in.Name, cat.Name), src, in.Name != "")
	if err != nil {
		return "", err
	}
	if existing != "" {
		c.Out.Info("%s is already in the catalog", existing)
		return existing, nil
	}
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
func (c *Catalogs) provision(ctx context.Context, ch RepoChoice, catalogName string, sd *seed, root bool) (string, error) {
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
			return "", fmt.Errorf("%s already has a catalog; list the manifest in it with `catalog add` instead", url)
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
	scaffold := c.Editor.Scaffold
	if root {
		scaffold = c.Editor.ScaffoldRoot
	}
	data, paths, what := scaffold(catalogName), []string{file}, defaultCatalogFile
	var writes []fileChange
	if sd != nil {
		manifestFile := filepath.Join(dir, defaultManifestFile)
		if c.Files.Exists(manifestFile) {
			return "", fmt.Errorf("%s already has a %s; it is not overwritten", url, defaultManifestFile)
		}
		var err error
		if data, err = c.Editor.AddWorkspaceRef(data, domain.WorkspaceRef{Name: sd.name, Source: domain.Source{Path: defaultManifestFile}}); err != nil {
			return "", err
		}
		writes = append(writes, fileChange{manifestFile, sd.manifest})
		paths, what = append(paths, manifestFile), defaultCatalogFile+" and "+defaultManifestFile
	}
	if ok, err := c.confirm(fmt.Sprintf("Add %s to %s and push it to %s?", what, url, branch), true); err != nil {
		return "", err
	} else if !ok {
		return "", ErrCancelled
	}
	// Written only now. Declining has to leave nothing behind: an untracked
	// multiverse.yaml in the cached clone survives every later refresh, which
	// made this command claim the repository "already has a multiverse.yaml"
	// for good and failed the clean-checkout gate of every later edit.
	if err := c.Files.WriteFile(file, data, 0o644); err != nil {
		return "", err
	}
	for _, w := range writes {
		if err := c.Files.WriteFile(w.File, w.Data, 0o644); err != nil {
			return "", err
		}
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

	// Manifest is a project manifest on this machine to start the catalog
	// with: copied in as multiverse.yaml and listed, so the repository is both
	// the catalog that lists the project and where its manifest lives.
	Manifest string
}

// CreateCatalog makes a catalog and links it from a parent: as a file in the
// parent's own repository, in an existing repository, or in a new one.
func (c *Catalogs) CreateCatalog(ctx context.Context, in CreateCatalogInput) (string, error) {
	if err := domain.ValidateCatalogName(in.Name); err != nil {
		return "", err
	}
	sd, err := c.loadSeed(in.Manifest)
	if err != nil {
		return "", err
	}
	tree, err := c.Tree(ctx, "", false)
	if err != nil {
		return "", err
	}
	parent, err := c.parentCatalog(tree, in.At, in.AtGiven, in.Where.URL)
	if err != nil {
		return "", err
	}
	for _, ch := range parent.Children {
		if ch.Name == in.Name {
			return "", fmt.Errorf("%q already exists in %s", in.Name, firstOf(parent.Path, "the root catalog"))
		}
	}
	made := domain.ChildPath(parent.Path, in.Name)

	where := in.Where
	where.URL = c.expand(where.URL)
	if !in.Same && where.URL == "" && where.Create == "" {
		what := "catalog"
		if sd != nil {
			what = "project"
		}
		if where, in.Same, err = c.askWhere(in.Name, what); err != nil {
			return "", err
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
			return "", fmt.Errorf("%s already exists", rel)
		}
		return made, c.edit(ctx, parent, fmt.Sprintf("add catalog %s", in.Name), func(data []byte) ([]fileChange, error) {
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

	url, err := c.provision(ctx, where, in.Name, sd, false)
	if err != nil {
		return "", err
	}
	return made, c.edit(ctx, parent, fmt.Sprintf("link catalog %s", in.Name), func(data []byte) ([]fileChange, error) {
		out, err := c.Editor.AddCatalogRef(data, domain.CatalogRef{Name: in.Name, Source: domain.Source{Repo: url}})
		return []fileChange{{parent.File, out}}, err
	})
}

func (c *Catalogs) askWhere(name, what string) (RepoChoice, bool, error) {
	own := "In a new private repository on GitHub"
	if what == "project" {
		own += ": a project repository, its own index and its own manifest"
	}
	i, err := c.Prompt.Select(fmt.Sprintf("Where should the %s live?", what), []Option{
		{Label: "In the same repository as the catalog it goes in"},
		{Label: "In an existing repository"},
		{Label: own},
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
	Name   string
	Source string // a repository (owner/repo or a URL) or a path: whichever it is
	Where  RepoChoice
	Local  string // a directory or catalog file on this machine
	Only   bool   // keep it local and git-only, with no remote yet
}

// RootTopic is the topic a root catalog's repository carries on its host, so a
// new machine can find it without cloning anything.
const RootTopic = "multiversectl-root"

// Init sets up the root catalog: one found on a hosting service, an existing
// repository or file, a new private repository, or a local-only git repository
// to push somewhere later. A machine that already has one is told so, and keeps
// it unless it is asked to switch; switching is refused while projects
// installed from the old root depend on it.
func (c *Catalogs) Init(ctx context.Context, in InitInput) error {
	cfg, err := c.Config.Load()
	if err != nil {
		return err
	}
	if in.Source != "" {
		if c.Files.Exists(in.Source) {
			in.Local = in.Source
		} else {
			in.Where.URL = in.Source
		}
	}
	in.Where.URL = c.expand(in.Where.URL)
	requested := in.Where.URL != "" || in.Where.Create != "" || in.Local != "" || in.Only

	if cfg.Catalog != "" {
		c.describeSetup(cfg)
	}
	if !requested {
		if cfg.Catalog != "" {
			switchRoot, err := c.askKeepOrSwitch(cfg)
			if err != nil {
				return err
			}
			if !switchRoot {
				return c.ensureRoot(ctx, cfg.Catalog)
			}
		}
		if in, err = c.askInit(ctx, in); err != nil {
			return err
		}
	}
	if cfg.Catalog != "" && c.sameRoot(cfg.Catalog, in) {
		c.Out.Info("already using `%s` as the root catalog", shortRepo(cfg.Catalog))
		return c.ensureRoot(ctx, cfg.Catalog)
	}
	if cfg.Catalog != "" {
		if n := dependents(cfg); n > 0 {
			return fmt.Errorf("%d installed projects come from `%s`, and would point into a root that is no longer used.\n  start over with `multiversectl reset` (it forgets the root and the projects, and deletes no file), or remove them one by one with `multiversectl catalog uninstall <name>`, then run init again", n, cfg.Catalog)
		}
		if ok, err := c.confirm(fmt.Sprintf("This machine already uses `%s` as its root catalog. Replace it?", shortRepo(cfg.Catalog)), false); err != nil {
			return err
		} else if !ok {
			return ErrCancelled
		}
	}

	name := in.Name
	var source string
	switch {
	case in.Where.URL != "" || in.Where.Create != "":
		name = firstOf(name, repoBase(in.Where.Create), "index")
		if source, err = c.provision(ctx, in.Where, name, nil, true); err != nil {
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
		c.Out.Info("to keep it elsewhere: `git -C %s remote add origin <url> && git -C %s push -u origin HEAD`", dir, dir)
	}
	if err := c.Use(source); err != nil {
		return err
	}
	if err := c.ensureRoot(ctx, source); err != nil {
		return err
	}
	c.Out.Info("next: `multiversectl catalog add <owner/repo>`  ·  `multiversectl catalog create <name>`")
	return nil
}

// dependents counts the installed projects that came from the root's tree,
// which a different root would leave pointing at nothing.
func dependents(cfg Config) int {
	n := 0
	for _, w := range cfg.Workspaces {
		if w.From != "" {
			n++
		}
	}
	return n
}

// describeSetup says what this machine has, so init starts from what is true.
func (c *Catalogs) describeSetup(cfg Config) {
	c.Out.Info("this machine uses `%s` as its root catalog", shortRepo(cfg.Catalog))
	if len(cfg.Workspaces) == 0 {
		c.Out.Info("nothing is installed from it yet")
		return
	}
	names := make([]string, 0, len(cfg.Workspaces))
	for n := range cfg.Workspaces {
		names = append(names, n)
	}
	sort.Strings(names)
	c.Out.Info("installed here: `%s`", strings.Join(names, "`, `"))
}

// askKeepOrSwitch is the question a machine that is already set up is asked.
// Without a terminal nothing is asked and the root is kept.
func (c *Catalogs) askKeepOrSwitch(cfg Config) (bool, error) {
	if c.Yes {
		return false, nil // --yes takes the default, which is to keep it
	}
	switchOpt := Option{Label: "Use a different root catalog"}
	if n := dependents(cfg); n > 0 {
		switchOpt.Disabled = true
		switchOpt.Note = fmt.Sprintf("%d installed projects come from this one; `multiversectl reset` first", n)
	}
	i, err := c.Prompt.Select("This machine is already set up. What now?", []Option{
		{Label: "Keep this root", Note: "check it, and mark it as the root if it is not yet"},
		switchOpt,
	}, 0)
	if errors.Is(err, ErrNotInteractive) {
		return false, nil
	}
	return i == 1, err
}

// ensureRoot checks the root catalog is marked as one: a CatalogRoot, and, on a
// hosting service, tagged with the topic a new machine searches by. A root that
// predates the kind is upgraded, after asking. Problems here never fail init:
// the root works either way, it is only harder to find.
func (c *Catalogs) ensureRoot(ctx context.Context, source string) error {
	tree, err := c.Tree(ctx, source, false)
	if err != nil {
		c.Out.Warn("could not read the root catalog to check it: %v", err)
		return nil
	}
	if !tree.Root {
		c.Out.Info("this root is a plain Catalog; as a CatalogRoot a new machine can tell it from a project's catalog")
		err := c.edit(ctx, tree, "mark the catalog as your root (kind: CatalogRoot)", func(data []byte) ([]fileChange, error) {
			out, err := c.Editor.MarkRoot(data)
			return []fileChange{{tree.File, out}}, err
		})
		if err != nil {
			if errors.Is(err, ErrCancelled) {
				return nil
			}
			c.Out.Warn("%v", err)
		}
	}
	c.markTopic(ctx, source)
	return nil
}

// markTopic tags the root's repository so it can be found from a machine that
// has nothing: best effort, since the root works without it.
func (c *Catalogs) markTopic(ctx context.Context, source string) {
	if !isGitURL(source) {
		return
	}
	for _, h := range c.Hosts {
		if !h.Owns(source) {
			continue
		}
		if err := h.Available(ctx); err != nil {
			c.Out.Info("to let a new machine find it, tag `%s` with the topic `%s` on %s", shortRepo(source), RootTopic, h.Name())
			return
		}
		err := c.Progress.Do("Tagging the repository with the topic "+RootTopic, func(ctx context.Context) error {
			return h.AddTopic(ctx, source, RootTopic)
		})
		if err != nil {
			c.Out.Warn("could not tag `%s` with the topic `%s`: %v", shortRepo(source), RootTopic, err)
			return
		}
		c.Out.OK("`%s` carries the topic `%s`, so a new machine can find it", shortRepo(source), RootTopic)
		return
	}
}

// rootSearch is what has been found, and where has been looked.
type rootSearch struct {
	searched map[string]bool
	found    []RemoteRepo
}

func (rs *rootSearch) add(repos []RemoteRepo) {
	for _, r := range repos {
		dup := false
		for _, have := range rs.found {
			dup = dup || sameRepo(have.URL, r.URL)
		}
		if !dup {
			rs.found = append(rs.found, r)
		}
	}
}

// search looks for roots on one service, behind a spinner.
func (c *Catalogs) searchRoots(ctx context.Context, h RepoHost, rs *rootSearch) {
	rs.searched[h.Name()] = true
	var repos []RemoteRepo
	err := c.Progress.Do("Looking for your root catalog on "+h.Name(), func(ctx context.Context) (err error) {
		repos, err = h.ReposWithTopic(ctx, RootTopic)
		return err
	})
	if err != nil {
		c.Out.Warn("could not search %s: %v", h.Name(), err)
		return
	}
	rs.add(repos)
}

// askInit asks where the root catalog is, offering first what a search of the
// services this person is signed in to turned up.
func (c *Catalogs) askInit(ctx context.Context, in InitInput) (InitInput, error) {
	hosts := c.newHostChecks()
	rs := &rootSearch{searched: map[string]bool{}}
	if len(c.Hosts) > 0 {
		hosts.probe(ctx)
		for _, h := range c.Hosts {
			if hosts.err[h.Name()] == nil {
				c.searchRoots(ctx, h, rs)
			}
		}
	}
	if c.Yes {
		if len(rs.found) == 0 {
			return in, errors.New("no root catalog to use: give a source (init <repo|path>), --create <name> or --local <dir>")
		}
		in.Where.URL = rs.found[0].URL
		return in, nil
	}
	for {
		var opts []Option
		var act []func() (done bool, err error)
		add := func(o Option, f func() (bool, error)) { opts, act = append(opts, o), append(act, f) }

		for _, r := range rs.found {
			note := "tagged " + RootTopic
			if r.Private {
				note = "private; " + note
			}
			add(Option{Label: "Use " + r.Name, Note: note}, func() (bool, error) { in.Where.URL = r.URL; return true, nil })
		}
		for _, h := range c.Hosts {
			if rs.searched[h.Name()] {
				continue
			}
			o := hosts.option(ctx, "Look for it on "+h.Name(), h)
			if o.Disabled {
				continue
			}
			add(o, func() (bool, error) {
				if err := hosts.ready(ctx, h); err != nil {
					return false, err
				}
				c.searchRoots(ctx, h, rs)
				if len(rs.found) == 0 {
					c.Out.Info("no repository on %s carries the topic `%s`", h.Name(), RootTopic)
				}
				return false, nil
			})
		}
		add(Option{Label: "In an existing repository or file"}, func() (bool, error) {
			where, err := c.Prompt.Input("Repository URL or path to a catalog", "")
			if err != nil || where == "" {
				return false, firstErr(err, errors.New("a location is required"))
			}
			if isGitURL(where) {
				in.Where.URL = c.expand(where)
			} else {
				in.Local = where
			}
			return true, nil
		})
		newIdx := len(opts)
		add(Option{Label: "In a new private repository on GitHub"}, func() (bool, error) {
			// Under which account is asked when GitHub can answer; a bare name
			// goes to the signed-in user's own.
			owner := ""
			if c.Host != nil && hosts.ready(ctx, c.Host) == nil {
				if account, err := c.pickAccount(ctx, c.Host, "Create it under which account or organization?"); err == nil {
					owner = account + "/"
				} else if errors.Is(err, context.Canceled) {
					return false, err
				}
			}
			name, err := c.Prompt.Input("New repository name", "multiverse-index")
			in.Where.Create = owner + name
			return err == nil, err
		})
		add(Option{Label: "Only on this machine for now", Note: "a local git repository with no remote yet"}, func() (bool, error) { in.Only = true; return true, nil })

		def := newIdx
		if len(rs.found) > 0 {
			def = 0
		}
		i, err := c.Prompt.Select("Where is your root catalog?", opts, def)
		if errors.Is(err, ErrNotInteractive) {
			return in, fmt.Errorf("%w: give a source (init <repo|path>), --create <name> or --local <dir>", err)
		}
		if err != nil {
			return in, err
		}
		done, err := act[i]()
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return in, err
			}
			c.Out.Warn("%v", err)
			continue
		}
		if done {
			return in, nil
		}
	}
}

// Reset forgets this machine's root catalog and every installed project, so
// it can start again from another root. It deletes no file, no checkout and no
// universe; it only clears what multiversectl remembers.
func (c *Catalogs) Reset() (forgot bool, err error) {
	cfg, err := c.Config.Load()
	if err != nil {
		return false, err
	}
	if cfg.Catalog == "" && len(cfg.Workspaces) == 0 {
		c.Out.Info("nothing to reset: this machine has no root catalog and no installed projects")
		return false, nil
	}
	if cfg.Catalog != "" {
		c.Out.Plain("  root catalog   `%s`", shortRepo(cfg.Catalog))
	}
	names := make([]string, 0, len(cfg.Workspaces))
	for n := range cfg.Workspaces {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		c.Out.Plain("  project      `%s`  (files stay in `%s`)", n, cfg.Workspaces[n].Root)
	}
	ok, err := c.confirm("Forget these? Nothing on disk is deleted.", false)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, ErrCancelled
	}
	if err := c.Config.Save(Config{}); err != nil {
		return false, fmt.Errorf("saving the configuration: %w", err)
	}
	c.Out.OK("this machine is reset: `multiversectl init` starts again")
	return true, nil
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
	if err := c.Files.WriteFile(file, c.Editor.ScaffoldRoot(name), 0o644); err != nil {
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

	// For a manifest file only: where its copy lives. Neither set means ask,
	// or the catalog's own repository when there is nobody to ask.
	Where RepoChoice
	Same  bool
}

// errNoRoot is what every command that writes to the index says when there is
// none: reading and installing do not need one, writing does.
var errNoRoot = errors.New("no root catalog to add to: `multiversectl init`  (installing a single project does not need one)")

// Add links a source into the catalog: a repository (owner/repo or a URL) or a
// path on this machine. A catalog.yaml makes it a catalog, otherwise it is a
// project. With no root catalog yet it sets
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
		return "", errNoRoot
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

// holdsCatalog tells a catalog from a project manifest by what the file is,
// so --path needs no second flag to say which kind it points at. The file is
// decoded rather than trusted by its name: a repository may hold a catalog.yaml
// of someone else's kind beside a real multiverse.yaml, and taking the name as
// the answer sent that project down the catalog path, where it could never be
// added. A catalog.yaml that does not decode still goes that way when there is
// no manifest to add instead, since the catalog path says what is wrong with
// the file while the project path would only report the manifest missing.
func (c *Catalogs) holdsCatalog(dir, path string) bool {
	file := filepath.Join(dir, filepath.FromSlash(firstOf(path, defaultCatalogFile)))
	if isDir(c.Files, file) {
		file = filepath.Join(file, defaultCatalogFile)
	}
	if !c.Files.Exists(file) {
		return false
	}
	if data, err := c.Files.ReadFile(file); err == nil {
		if _, err := c.Decoder.Decode(data); err == nil {
			return true
		}
	}
	if path != "" {
		return false
	}
	_, err := findManifest(c.Files, dir, "")
	return err != nil
}

// seed is a manifest a new catalog starts out listing.
type seed struct {
	name     string
	manifest []byte
}

// loadSeed reads the manifest to start a catalog with. Anything beside it
// that the manifest may rely on is not copied, and saying so now is better
// than a project that fails to start on someone else's machine.
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
// project manifest file has no repository of its own to point at, so it is
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
		return c.addManifestFile(ctx, in, abs)
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
	rel, err := relWithin(top, dir)
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

// addManifestFile lists a manifest file. It has no repository to point at, so
// it goes into one: a copy in the repository of the catalog it is listed in, or
// a repository of its own that becomes a project, its own index and project.
func (c *Catalogs) addManifestFile(ctx context.Context, in AddInput, file string) (string, error) {
	sd, err := c.loadSeed(file)
	if err != nil {
		return "", fmt.Errorf("%s is neither a catalog nor a project manifest: %w", file, err)
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
		return "", errNoRoot
	}

	where, same := in.Where, in.Same
	where.URL = c.expand(where.URL)
	if !same && where.URL == "" && where.Create == "" {
		if where, same, err = c.askWhere(name, "project"); err != nil {
			return "", err
		}
	}
	if !same && (where.URL != "" || where.Create != "") {
		return c.CreateCatalog(ctx, CreateCatalogInput{Name: name, At: in.At, AtGiven: in.AtGiven, Where: where, Manifest: file})
	}

	tree, err := c.Tree(ctx, "", false)
	if err != nil {
		return "", err
	}
	parent, err := c.parentCatalog(tree, in.At, in.AtGiven, "")
	if err != nil {
		return "", err
	}
	rel := path.Join("projects", name, defaultManifestFile)
	// A remote catalog's entries carry its repository, so the copy being added
	// must too, or adding the same file twice would look like two different ones.
	name, existing, err := c.settle(tree, parent, name, domain.Source{Repo: parent.Source.Repo, Path: rel}, in.Name != "")
	if err != nil {
		return "", err
	}
	if existing != "" {
		c.Out.Info("%s is already in the catalog", existing)
		return existing, nil
	}
	rel = path.Join("projects", name, defaultManifestFile)
	target := filepath.Join(parent.Dir, filepath.FromSlash(rel))
	if c.Files.Exists(target) {
		return "", fmt.Errorf("%s already exists in the catalog's repository", rel)
	}
	err = c.edit(ctx, parent, fmt.Sprintf("add project %s", name), func(data []byte) ([]fileChange, error) {
		out, err := c.Editor.AddWorkspaceRef(data, domain.WorkspaceRef{Name: name, Source: domain.Source{Path: rel}})
		return []fileChange{{parent.File, out}, {target, sd.manifest}}, err
	})
	return domain.ChildPath(parent.Path, name), err
}

// sameRoot reports that init was asked for the root already in use, which is
// nothing to do and nothing to confirm.
func (c *Catalogs) sameRoot(current string, in InitInput) bool {
	switch {
	case in.Where.URL != "":
		// The same repository written another way is the same root: ssh and
		// https, with and without .git. Comparing the text instead told people
		// to `reset` their whole machine to re-init the root they had.
		return sameRepo(in.Where.URL, current)
	case in.Local != "":
		abs, err := filepath.Abs(in.Local)
		if err != nil {
			return false
		}
		if _, derr := c.Files.ListDirs(abs); derr == nil {
			abs = filepath.Join(abs, defaultCatalogFile)
		}
		return abs == current
	}
	return false
}
