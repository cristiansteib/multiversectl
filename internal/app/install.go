package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// InstallOptions say where multiverses go and how to read the arguments.
type InstallOptions struct {
	Base  string // directory multiverses go under, each in <Base>/<name>
	Root  string // the exact directory, for a single multiverse
	Name  string // the name to record, for a single multiverse; refused if taken
	Fetch bool   // refresh catalogs first
	// Exclude are catalog paths left out of what a node's argument installs: a
	// multiverse, or a whole branch of the tree.
	Exclude []string
}

// InstallTarget is one multiverse to set up. Skip says why it is not.
type InstallTarget struct {
	Name, Root     string
	ManifestSource string // a git URL, or an absolute manifest file
	Ref            string
	ManifestPath   string
	From           string // its path in the root catalog, when it came from there
	Origin         Origin
	How            string // how its argument was understood, shown to the user
	Skip           string
	// Generated is the manifest for a repository that defines none, and Renamed
	// says why it took a longer name than its own: the short one was someone's.
	Generated []byte
	Renamed   string
}

// PlanInstall turns what the user typed into multiverses to set up. Each
// argument is read in a fixed order and the way it was read is reported:
//  1. a node in the root catalog (a multiverse, or everything under a catalog);
//  2. a path on this machine: a manifest, a catalog, or a directory holding one;
//  3. a git URL;
//  4. owner/repo, meaning GitHub.
//
// A repository or path that holds a catalog is read as one, so installing
// the multiverse a teammate pointed at needs no root catalog of your own.
func (c *Catalogs) PlanInstall(ctx context.Context, args []string, o InstallOptions) ([]InstallTarget, error) {
	cfg, err := c.Config.Load()
	if err != nil {
		return nil, err
	}
	var root *domain.Node
	var rootErr error
	if cfg.Catalog != "" {
		if n, err := c.Tree(ctx, "", o.Fetch); err != nil {
			rootErr = err
		} else {
			root = &n
			for _, bad := range unreadable(n) {
				c.Out.Warn("%s is unavailable: %s", bad.Path, bad.Unavailable)
			}
		}
	}

	for _, x := range o.Exclude {
		if root == nil {
			return nil, fmt.Errorf("--exclude %q needs a root catalog to be read from: %w", x, firstErr(rootErr, errors.New("none is set")))
		}
		if _, ok := root.Find(strings.Trim(x, "/")); !ok || strings.Trim(x, "/") == "" {
			return nil, fmt.Errorf("--exclude %q is not in your catalog; `multiversectl catalog list` shows the tree", x)
		}
	}

	var found []found
	if len(args) == 0 {
		if root == nil {
			return nil, firstErr(rootErr, errors.New("no root catalog to pick from: `multiversectl start <owner/repo | url | path>`, or `multiversectl init`"))
		}
		picked, err := c.pickLeaves(*root, cfg)
		if err != nil {
			return nil, err
		}
		found = append(found, picked...)
	}
	for _, arg := range args {
		got, err := c.resolveSource(ctx, arg, root, rootErr, o)
		if err != nil {
			return nil, err
		}
		found = append(found, got...)
	}

	explicit := len(found)
	if o.Root != "" && explicit != 1 {
		return nil, errors.New("--root names one directory; this installs several (give --base instead)")
	}

	taken := map[string]string{} // name -> where it comes from
	for name, e := range cfg.Multiverses {
		taken[name] = firstOf(shortRepo(e.Source.Repo), e.Manifest)
	}
	seen := map[Origin]bool{}
	var out []InstallTarget
	for _, f := range found {
		t := InstallTarget{How: f.how, Ref: f.leaf.Source.Ref, From: f.from, Origin: originOf(f.leaf.Source)}
		t.ManifestSource, t.ManifestPath = f.leaf.Source.Repo, f.leaf.Source.Path
		if t.ManifestSource == "" { // a file on this machine
			t.ManifestSource, t.ManifestPath, t.Ref = f.leaf.Source.Path, "", ""
		}
		if name, ok := installedAs(cfg, f.from, t.Origin); ok {
			t.Name, t.Skip = name, "already installed as "+name
			out = append(out, t)
			continue
		}
		if seen[t.Origin] {
			continue
		}
		seen[t.Origin] = true
		name := firstOf(o.Name, f.leaf.Name)
		if have, clash := taken[name]; clash {
			if !f.leaf.Implied || o.Name != "" {
				return nil, clashError(name, have, t)
			}
			// No one chose this name, it is only what the repository is called,
			// so it takes the next level of its address that is free.
			if name = freeName(f.leaf.Source.Repo, taken); name == "" {
				return nil, fmt.Errorf("every name %s could take is used (%s): give it one with --as", shortRepo(f.leaf.Source.Repo), strings.Join(nameCandidates(f.leaf.Source.Repo), ", "))
			}
			t.Renamed = fmt.Sprintf("`%s` is taken by %s, so this one is `%s`", f.leaf.Name, have, name)
		}
		if err := validMultiverseName(name); err != nil {
			return nil, err
		}
		taken[name] = firstOf(shortRepo(t.ManifestSource), t.ManifestSource)
		t.Name = name
		if f.leaf.Implied {
			if t.Generated, err = c.impliedManifest(name, f.leaf); err != nil {
				return nil, err
			}
		}
		t.Root = firstOf(o.Root, filepath.Join(o.Base, name))
		out = append(out, t)
	}
	return out, nil
}

// found is a multiverse an argument led to, with where it sits in the root
// catalog if it came from there.
type found struct {
	leaf domain.Node
	from string
	how  string
}

func originOf(s domain.Source) Origin { return Origin{Repo: s.Repo, Ref: s.Ref, Path: s.Path} }

// installedAs reports a multiverse already recorded for this catalog path or
// this source, so installing twice is not an error and a renamed catalog node
// does not install it a second time.
func installedAs(cfg Config, from string, o Origin) (string, bool) {
	for name, e := range cfg.Multiverses {
		if from != "" && e.From == from {
			return name, true
		}
		if o != (Origin{}) && e.Source == o {
			return name, true
		}
	}
	return "", false
}

// clashError refuses a second multiverse under a name already in use: the
// names are what -m and every command take, so two cannot share one.
func clashError(name, have string, t InstallTarget) error {
	other := firstOf(t.ManifestPath, t.ManifestSource)
	if t.ManifestPath != "" {
		other = shortRepo(t.ManifestSource) + " " + t.ManifestPath
	}
	return fmt.Errorf("a multiverse named %q is already taken (by %s); %s defines another with the same name, and names are unique on a machine: rename it there, or `multiversectl system forget %s` if the first is the one to drop", name, have, other, name)
}

// freeName is the first name a repository can take that nothing else has, or
// empty when they are all used.
func freeName(url string, taken map[string]string) string {
	for _, n := range nameCandidates(url) {
		if _, used := taken[n]; !used {
			return n
		}
	}
	return ""
}

// impliedManifest is the definition of a repository that has none: itself, as
// the one repository, run from its own compose files.
func (c *Catalogs) impliedManifest(name string, leaf domain.Node) ([]byte, error) {
	if c.Writer == nil {
		return nil, errors.New("cannot write a manifest for " + leaf.Name)
	}
	body, err := c.Writer.Multiverse(ProjectDraft{Name: name, Repos: []DraftRepo{{Key: leaf.Name, URL: leaf.Source.Repo}}})
	if err != nil {
		return nil, err
	}
	head := "# Made by multiversectl: " + shortRepo(leaf.Source.Repo) + " has compose files and no multiverse.yaml,\n" +
		"# so it is run as it is. To say more about how it runs (services, secrets, optional\n" +
		"# groups), add a .multiverse.yaml to the repository; a multiverse.yaml there would\n" +
		"# take the place of this file the next time it is installed.\n"
	return append([]byte(head), body...), nil
}

func validMultiverseName(name string) error {
	if !multiverseNameRE.MatchString(name) {
		return fmt.Errorf("multiverse name %q must match %s", name, multiverseNameRE)
	}
	return nil
}

func (c *Catalogs) resolveSource(ctx context.Context, arg string, root *domain.Node, rootErr error, o InstallOptions) ([]found, error) {
	if root != nil {
		if n, ok := root.Find(strings.Trim(arg, "/")); ok && strings.Trim(arg, "/") != "" {
			if n.Unavailable != "" {
				return nil, fmt.Errorf("%q is unavailable: %s", arg, n.Unavailable)
			}
			if n.Ignored {
				return nil, fmt.Errorf("%q is ignored on this machine: `multiversectl catalog unignore %s` brings it back", arg, n.Path)
			}
			leaves := n.Leaves()
			if len(leaves) == 0 {
				return nil, fmt.Errorf("%q has no multiverses under it", arg)
			}
			if kept := slices.DeleteFunc(slices.Clone(leaves), func(l domain.Node) bool { return l.Ignored }); len(kept) < len(leaves) {
				if len(kept) == 0 {
					return nil, fmt.Errorf("everything under %q is ignored on this machine", arg)
				}
				leaves = kept
			}
			if kept := withoutExcluded(leaves, o.Exclude); len(kept) == 0 {
				return nil, fmt.Errorf("everything under %q is excluded", arg)
			} else if len(kept) < len(leaves) {
				c.Out.Info("leaving out %d of %d multiverses under %s", len(leaves)-len(kept), len(leaves), arg)
				leaves = kept
			}
			out := make([]found, len(leaves))
			for i, l := range leaves {
				out[i] = found{leaf: l, from: l.Path, how: arg + " is in your catalog"}
			}
			return out, nil
		}
	}
	var leaves []domain.Node
	var how string
	var err error
	arg, pick := splitAddress(arg)
	switch {
	case c.Files.Exists(arg):
		leaves, how, err = c.openLocal(ctx, arg, o)
	case isGitURL(arg) || shorthandRE.MatchString(arg):
		leaves, how, err = c.openRemote(ctx, c.expand(arg), o)
	default:
		err = fmt.Errorf("%q is not a catalog entry, a path, or a repository", arg)
		if rootErr != nil {
			err = fmt.Errorf("%w (and your root catalog could not be read: %v)", err, rootErr)
		}
	}
	if err != nil {
		return nil, err
	}
	if pick != "" {
		leaves, err = only(leaves, pick, arg)
	} else {
		leaves, err = c.chooseAmong(leaves, arg)
	}
	if err != nil {
		return nil, err
	}
	out := make([]found, len(leaves))
	for i, l := range leaves {
		out[i] = found{leaf: l, how: how}
	}
	return out, nil
}

// chooseAmong narrows what an outside source lists to the one multiverse to
// start: a single one is taken as it is, several are a choice at a terminal and
// an error naming them in a script.
func (c *Catalogs) chooseAmong(leaves []domain.Node, source string) ([]domain.Node, error) {
	if len(leaves) <= 1 {
		return leaves, nil
	}
	opts := make([]Option, len(leaves))
	for i, l := range leaves {
		opts[i] = Option{Label: l.Name, Note: firstOf(l.Source.Path, shortRepo(l.Source.Repo))}
	}
	i, err := c.Prompt.Select(fmt.Sprintf("%s defines %d multiverses. Which one?", source, len(leaves)), opts, 0)
	if errors.Is(err, ErrNotInteractive) {
		return nil, fmt.Errorf("%s defines %d multiverses (%s): name one, as %s:<name>", source, len(leaves), leafNames(leaves), source)
	}
	if err != nil {
		return nil, err
	}
	return []domain.Node{leaves[i]}, nil
}

func leafNames(nodes []domain.Node) string {
	names := make([]string, len(nodes))
	for i, n := range nodes {
		names[i] = n.Name
	}
	return strings.Join(names, ", ")
}

// openLocal reads a path: a catalog file, a multiverse file, or a directory
// holding either.
func (c *Catalogs) openLocal(ctx context.Context, arg string, o InstallOptions) ([]domain.Node, string, error) {
	abs, err := filepath.Abs(arg)
	if err != nil {
		return nil, "", err
	}
	how := arg + " is a path on this machine"
	file := abs
	if _, derr := c.Files.ListDirs(abs); derr == nil {
		if c.Files.Exists(filepath.Join(abs, defaultCatalogFile)) {
			file = filepath.Join(abs, defaultCatalogFile)
		} else {
			nodes, err := c.definedIn(abs, "")
			if err != nil {
				return nil, "", err
			}
			if len(nodes) == 0 && hasCompose(c.Files, abs) {
				// A checkout of a repository that is only compose files runs as
				// itself, from wherever it came from.
				url, err := c.Git.RemoteURL(ctx, abs)
				if err != nil || url == "" {
					return nil, "", fmt.Errorf("%s has compose files but no remote to clone them from; `git remote add origin <url>` first", arg)
				}
				return []domain.Node{impliedFrom(url)}, how + ", a repository with compose files and no multiverse.yaml", nil
			}
			if len(nodes) == 0 {
				return nil, "", fmt.Errorf("%s has no %s, no multiverse.yaml and no compose file", arg, defaultCatalogFile)
			}
			return nodes, how + ", with a multiverse", nil
		}
	}
	data, err := c.Files.ReadFile(file)
	if err != nil {
		return nil, "", err
	}
	if _, err := c.Decoder.Decode(data); err == nil {
		tree, err := c.Tree(ctx, file, o.Fetch)
		if err != nil {
			return nil, "", err
		}
		return tree.Leaves(), how + ", a catalog", nil
	}
	name, err := c.Editor.MultiverseName(data)
	if err != nil {
		return nil, "", fmt.Errorf("%s is neither a catalog nor a multiverse: %w", file, err)
	}
	return []domain.Node{{Name: name, Path: name, Kind: domain.MultiverseNode, Source: domain.Source{Path: file}}}, how + ", a multiverse", nil
}

// openRemote reads a repository: its catalog when it has one, else the
// multiverses it defines.
func (c *Catalogs) openRemote(ctx context.Context, url string, o InstallOptions) ([]domain.Node, string, error) {
	how := shortRepo(url) + " is a repository"
	dir, err := c.Clone(ctx, url, "")
	if err != nil {
		return nil, "", fmt.Errorf("%s is not in your catalog, and %s cannot be reached: %s", shortRepo(url), url, reason(err, url))
	}
	if c.Files.Exists(filepath.Join(dir, defaultCatalogFile)) {
		tree, err := c.Tree(ctx, url, false)
		if err != nil {
			return nil, "", err
		}
		return tree.Leaves(), how + " with a catalog", nil
	}
	nodes, err := c.definedIn(dir, url)
	if err != nil {
		return nil, "", fmt.Errorf("%s: %w", shortRepo(url), err)
	}
	if len(nodes) == 0 && hasCompose(c.Files, dir) {
		return []domain.Node{impliedFrom(url)}, how + " with compose files and no multiverse.yaml, so it runs as itself", nil
	}
	if len(nodes) == 0 {
		return nil, "", fmt.Errorf("%s has no %s, no multiverse.yaml and no compose file", shortRepo(url), defaultCatalogFile)
	}
	return nodes, how + " that defines a multiverse", nil
}

// pickLeaves is the menu for `install` with no argument: every multiverse in
// the root catalog, as the tree, with those already here shown as such.
func (c *Catalogs) pickLeaves(root domain.Node, cfg Config) ([]found, error) {
	var leaves []domain.Node
	var opts []Option
	for _, row := range root.Rows() {
		n := row.Node
		switch {
		case n.Kind == domain.MultiverseNode:
			opt := Option{Label: row.Prefix + n.Name, Note: shortRepo(n.Source.Repo)}
			if name, ok := installedAs(cfg, n.Path, originOf(n.Source)); ok {
				opt.Disabled, opt.Note = true, "installed as "+name
			}
			opts = append(opts, opt)
			leaves = append(leaves, n)
		default:
			opts = append(opts, Option{Label: row.Prefix + n.Name, Disabled: true, Note: n.Unavailable})
			leaves = append(leaves, domain.Node{})
		}
	}
	idx, err := c.Prompt.MultiSelect("Which multiverses should be installed?", opts)
	if errors.Is(err, ErrNotInteractive) {
		return nil, fmt.Errorf("%w (name one: `multiversectl start <path>`)", err)
	}
	if err != nil {
		return nil, err
	}
	if len(idx) == 0 {
		return nil, ErrCancelled
	}
	out := make([]found, 0, len(idx))
	for _, i := range idx {
		out = append(out, found{leaf: leaves[i], from: leaves[i].Path, how: leaves[i].Path + " is in your catalog"})
	}
	return out, nil
}

// unreadable lists the layers that could not be read, at any depth.
// withoutExcluded drops the leaves at or under any excluded path.
func withoutExcluded(leaves []domain.Node, exclude []string) []domain.Node {
	var kept []domain.Node
next:
	for _, l := range leaves {
		for _, x := range exclude {
			x = strings.Trim(x, "/")
			if l.Path == x || strings.HasPrefix(l.Path, x+"/") {
				continue next
			}
		}
		kept = append(kept, l)
	}
	return kept
}

func unreadable(n domain.Node) []domain.Node {
	var out []domain.Node
	for _, ch := range n.Children {
		if ch.Unavailable != "" {
			out = append(out, ch)
		}
		out = append(out, unreadable(ch)...)
	}
	return out
}

// shortRepo is a repository in the notation used everywhere it is shown, such as
// github:owner/name, so a line says where it is and stays short.
func shortRepo(url string) string {
	if url == "" {
		return "this machine"
	}
	return domain.Locate(url, "").Short("")
}
