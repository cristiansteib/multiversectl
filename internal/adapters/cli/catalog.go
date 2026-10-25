package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cristiansteib/multiversectl/internal/app"
	"github.com/cristiansteib/multiversectl/internal/domain"
)

// catalogs builds the catalog service and applies --yes.
func (r runner) catalogs(yes bool) (*app.Catalogs, error) {
	c, err := r.f.Catalogs(*r.g, r.out)
	if err != nil {
		return nil, err
	}
	c.Yes = yes
	return c, nil
}

// declined turns a "no" at a confirmation into a calm exit: nothing was
// written, so it is not a failure.
func (r runner) declined(err error) error {
	if errors.Is(err, app.ErrCancelled) {
		r.out.Info("cancelled; nothing was changed")
		return nil
	}
	return err
}

// atFlag registers --at; "/" names the root catalog.
func (r runner) atFlag(cmd *cobra.Command, at *string) {
	cmd.Flags().StringVar(at, "at", "", "catalog to put it in, by path (/ is the root); asked when omitted")
	_ = cmd.RegisterFlagCompletionFunc("at", r.completeNodes(true))
}

func (r runner) catalogCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "catalog",
		Short: "Browse and edit the index of projects: list the tree, add, create and remove entries",
		Long: "The catalog is the tree of projects and other catalogs your root points at.\n" +
			"`list` shows it, with what is installed on this machine marked; `add`, `create`\n" +
			"and `rm` edit it. Installing what it lists is done with `project install`.",
		Example: "  multiversectl catalog list\n  multiversectl catalog add acme/multiverse\n  multiversectl catalog create acme --new-repo acme-index",
		Args:    cobra.NoArgs,
		RunE:    func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(r.catalogListCmd(), r.catalogAddCmd(), r.catalogCreateCmd(), r.catalogRmCmd())
	return cmd
}

func (r runner) catalogListCmd() *cobra.Command {
	var fetch bool
	var format, from string
	cmd := &cobra.Command{
		Use:     "list [node-path]",
		Aliases: []string{"ls", "tree"},
		Short:   "Show the catalog as a tree, or the part under a node",
		Long: "Shows the root catalog as a tree: each node's name, what it is (root, catalog\n" +
			"or project) and where it lives, with the projects already installed on this\n" +
			"machine marked. Give a node's path to show only what is under it. It reads\n" +
			"what was last fetched unless --fetch.",
		Example:           "  multiversectl catalog list\n  multiversectl catalog ls acme/backend\n  multiversectl catalog list --fetch -o json",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: r.completeNodes(false),
	}
	cmd.Flags().BoolVar(&fetch, "fetch", false, "fetch every catalog repository first")
	cmd.Flags().StringVarP(&format, "output", "o", "tree", "output: tree or json")
	cmd.Flags().StringVar(&from, "from", "", "show another catalog instead: a git URL, owner/repo or a file")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return r.showTree(cmd, args, fetch, format, from)
	}
	return cmd
}

func (r runner) showTree(cmd *cobra.Command, args []string, fetch bool, format, from string) error {
	if format != "tree" && format != "json" {
		return fmt.Errorf("--output must be tree or json, not %q", format)
	}
	c, err := r.catalogs(false)
	if err != nil {
		return err
	}
	var tree domain.Node
	err = r.spin(fetch, "Fetching the catalogs", func() (err error) {
		tree, err = c.Tree(cmd.Context(), localOrURL(from), fetch)
		return err
	})
	if err != nil {
		return err
	}
	node, path := tree, ""
	if len(args) == 1 {
		path = strings.Trim(args[0], "/")
		var ok bool
		if node, ok = tree.Find(path); !ok {
			return fmt.Errorf("nothing at %q; run `multiversectl catalog list` to see the tree", args[0])
		}
	}
	installed, err := c.InstalledIn(tree)
	if err != nil {
		return err
	}
	if format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(toJSON(node, installed))
	}
	for _, line := range r.renderTree(node, installed) {
		r.out.Plain("%s", line)
	}
	if len(node.Children) == 0 && node.Kind == domain.CatalogNode {
		r.out.Info("empty: `multiversectl catalog add <owner/repo>`")
	}
	return nil
}

type jsonNode struct {
	Name        string     `json:"name"`
	Path        string     `json:"path"`
	Kind        string     `json:"kind"`
	Repo        string     `json:"repo,omitempty"`
	Ref         string     `json:"ref,omitempty"`
	File        string     `json:"file,omitempty"`
	InstalledAs string     `json:"installedAs,omitempty"`
	Unavailable string     `json:"unavailable,omitempty"`
	Children    []jsonNode `json:"children,omitempty"`
}

func toJSON(n domain.Node, installed map[string]string) jsonNode {
	j := jsonNode{Name: n.Name, Path: n.Path, Kind: string(n.Kind), Repo: n.Source.Repo, Ref: n.Source.Ref,
		File: n.Source.Path, InstalledAs: installed[n.Path], Unavailable: n.Unavailable}
	for _, c := range n.Children {
		j.Children = append(j.Children, toJSON(c, installed))
	}
	return j
}

// renderTree draws a node and what is under it: its name, what kind of node it
// is (the root, a catalog, a project), where it lives, and the file within that
// place when it shares the place with another node.
func (r runner) renderTree(root domain.Node, installed map[string]string) []string {
	rows := root.Rows()
	width := 0
	for _, row := range rows {
		if w := len([]rune(row.Prefix + row.Node.Name)); w > width {
			width = w
		}
	}
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		n := row.Node
		kind, name := "catalog", r.out.Accent(n.Name)
		switch {
		case n.Path == "":
			kind, name = "root", r.out.Bold(r.out.Accent(n.Name))
		case n.Kind == domain.WorkspaceNode:
			kind, name = "project", n.Name
		}
		label := row.Prefix + n.Name
		parts := []string{
			row.Prefix + name + strings.Repeat(" ", width-len([]rune(label))),
			r.out.Dim(kind + strings.Repeat(" ", 9-len(kind))),
		}
		if loc := r.out.Place(n.Location()); loc != "" {
			if f := n.FileWithin(); f != "" {
				loc += " " + r.out.Dim(f)
			}
			parts = append(parts, loc)
		}
		if n.Unavailable != "" {
			parts = append(parts, r.out.Red("✗ "+n.Unavailable))
		}
		switch {
		case n.Kind == domain.WorkspaceNode && n.Unavailable == "":
			if name, ok := installed[n.Path]; ok {
				parts = append(parts, r.out.Green("✓ installed as "+name))
			} else {
				parts = append(parts, r.out.Dim("○ not installed"))
			}
		case n.Kind == domain.CatalogNode && n.Unavailable == "":
			// How much of what a catalog lists is on this machine, so a group can be
			// judged without opening it.
			if leaves := n.Leaves(); len(leaves) > 0 {
				have := 0
				for _, l := range leaves {
					if _, ok := installed[l.Path]; ok {
						have++
					}
				}
				parts = append(parts, r.out.Dim(fmt.Sprintf("%d/%d installed", have, len(leaves))))
			}
		}
		lines = append(lines, strings.TrimRight(strings.Join(parts, "  "), " "))
	}
	return lines
}

// localOrURL makes a file argument absolute so a saved path still works from
// another directory; URLs, owner/repo and the empty argument pass through.
func localOrURL(arg string) string {
	if arg == "" || strings.Contains(arg, "://") || strings.HasPrefix(arg, "git@") {
		return arg
	}
	if _, err := os.Stat(arg); err != nil {
		return arg
	}
	if abs, err := filepath.Abs(arg); err == nil {
		return abs
	}
	return arg
}

func (r runner) catalogAddCmd() *cobra.Command {
	var in app.AddInput
	var yes bool
	cmd := &cobra.Command{
		Use:   "add <owner/repo | url | path>",
		Short: "Put a project in the catalog, in the place you pick",
		Long: "The source can be owner/repo (GitHub), any git URL, or a path. A repository\n" +
			"with a catalog.yaml is linked as a catalog (and may list its own project\n" +
			"and deeper catalogs); one with only a multiverse.yaml is listed as a\n" +
			"project. A path to a git checkout is added by its origin URL, since other\n" +
			"machines must be able to fetch it. A manifest file has no repository, so it\n" +
			"goes into one: the catalog's own, or a new or existing one of its own, which\n" +
			"makes a project repository.",
		Example: "  multiversectl catalog add acme/multiverse\n" +
			"  multiversectl catalog add ~/code/billing --at acme/backend\n" +
			"  multiversectl catalog add ./acme.yaml                  # asks where the manifest lives\n" +
			"  multiversectl catalog add ./acme.yaml --new-repo acme-org/multiverse",
		Args: cobra.MaximumNArgs(1),
	}
	r.atFlag(cmd, &in.At)
	cmd.Flags().StringVar(&in.Name, "name", "", "name in the catalog (default: the one the source gives itself)")
	cmd.Flags().StringVar(&in.Ref, "ref", "", "branch, tag or commit of the repository")
	cmd.Flags().StringVar(&in.Path, "path", "", "catalog or manifest path in the repository")
	cmd.Flags().BoolVar(&in.Same, "same-repo", false, "for a manifest file: copy it into the catalog's own repository")
	cmd.Flags().StringVar(&in.Where.URL, "repo", "", "for a manifest file: put it in this existing repository")
	cmd.Flags().StringVar(&in.Where.Create, "new-repo", "", "for a manifest file: create a new repository with this name for it")
	cmd.Flags().BoolVar(&in.Where.Public, "public", false, "make a --new-repo public (private by default)")
	cmd.Flags().BoolVar(&yes, "yes", false, "accept every default and confirmation (entries go in the root unless --at says otherwise)")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		source, err := r.textArg(cmd, args, "Repository, URL or path to add  (owner/repo means GitHub)", "")
		if err != nil {
			return err
		}
		c, err := r.catalogs(yes)
		if err != nil {
			return err
		}
		in.Repo, in.AtGiven = source, cmd.Flags().Changed("at")
		node, err := c.Add(cmd.Context(), in)
		if err != nil {
			return r.declined(err)
		}
		r.out.Info("next: `multiversectl project install %s`", node)
		return nil
	}
	return cmd
}

func (r runner) catalogCreateCmd() *cobra.Command {
	var in app.CreateCatalogInput
	var yes bool
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create an empty catalog node to group projects under",
		Long: "Create a catalog and link it from a parent. It can live in the parent's own\n" +
			"repository, in a repository you already have, or in a new private one.",
		Example: "  multiversectl catalog create acme --new-repo acme-index\n  multiversectl catalog create personal --same-repo --at /",
		Args:    cobra.MaximumNArgs(1),
	}
	r.atFlag(cmd, &in.At)
	cmd.Flags().BoolVar(&in.Same, "same-repo", false, "keep it as a file in the parent's repository")
	cmd.Flags().StringVar(&in.Where.URL, "repo", "", "put it in this existing repository")
	cmd.Flags().StringVar(&in.Where.Create, "new-repo", "", "create a new repository with this name for it")
	cmd.Flags().BoolVar(&in.Where.Public, "public", false, "make a --new-repo public (private by default)")
	cmd.Flags().BoolVar(&yes, "yes", false, "accept every default and confirmation")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		name, err := r.textArg(cmd, args, "Name of the catalog", "")
		if err != nil {
			return err
		}
		c, err := r.catalogs(yes)
		if err != nil {
			return err
		}
		in.Name, in.AtGiven = name, cmd.Flags().Changed("at")
		node, err := c.CreateCatalog(cmd.Context(), in)
		if err != nil {
			return r.declined(err)
		}
		r.out.Info("next: `multiversectl catalog add <owner/repo> --at %s`", node)
		return nil
	}
	return cmd
}

func (r runner) catalogRmCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:               "rm <path>",
		Short:             "Remove an entry from the catalog that holds it (what it points at is untouched)",
		Example:           "  multiversectl catalog rm acme/backend/payments",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: r.completeNodes(false),
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "accept every confirmation")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		c, err := r.catalogs(yes)
		if err != nil {
			return err
		}
		node, err := r.nodeArg(cmd, args, c, "Which entry should come out of the catalog?")
		if err != nil {
			return err
		}
		return r.declined(c.Remove(cmd.Context(), node))
	}
	return cmd
}

func (r runner) initCmd() *cobra.Command {
	var in app.InitInput
	var yes bool
	cmd := &cobra.Command{
		Use:   "init [owner/repo | url | path]",
		Short: "Set up this machine's root catalog, in an existing repository or a new private one",
		Long: "The root catalog is the index every other catalog and project hangs from.\n" +
			"Give a repository or a path to use one you have (it is checked, and a repository\n" +
			"with no catalog gets one), or none to be asked. Pointing it at another\n" +
			"source replaces the current root, after asking.",
		Example: "  multiversectl init\n  multiversectl init me/multiverse-index\n  multiversectl init --create multiverse-index\n  multiversectl init --local ~/multiverse-index",
		Args:    cobra.MaximumNArgs(1),
	}
	cmd.Flags().StringVar(&in.Where.Create, "create", "", "create a new private repository with this name")
	cmd.Flags().BoolVar(&in.Where.Public, "public", false, "make a --create repository public (private by default)")
	cmd.Flags().StringVar(&in.Local, "local", "", "use a directory or catalog file on this machine")
	cmd.Flags().StringVar(&in.Name, "name", "", "the catalog's metadata.name (default: from the repository)")
	cmd.Flags().BoolVar(&yes, "yes", false, "accept every default and confirmation")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		c, err := r.catalogs(yes)
		if err != nil {
			return err
		}
		if len(args) == 1 {
			in.Source = args[0]
		}
		return r.declined(c.Init(cmd.Context(), in))
	}
	return cmd
}

func (r runner) installCmd() *cobra.Command {
	var base, name string
	var yes, all, fetch bool
	cmd := &cobra.Command{
		Use:   "install [source ...]",
		Short: "Put a project on this machine: from your catalog, a repository or a path",
		Long: "Each source is read in this order, and what it was taken for is said:\n" +
			"  1. a node in your catalog (a project, or everything under a catalog)\n" +
			"  2. a path on this machine: a manifest, a catalog, or a directory holding one\n" +
			"  3. a git URL\n" +
			"  4. owner/repo, meaning GitHub\n" +
			"A repository with a catalog is read as one, so a project a teammate points at\n" +
			"installs without a root catalog of your own. With no source it shows your\n" +
			"catalog to pick from. A project goes in <base>/<its name> and is recorded\n" +
			"so use and -w reach it; installing never writes to a catalog.",
		Example:           "  multiversectl project install acme/multiverse\n  multiversectl project install acme/backend/billing\n  multiversectl project install ./multiverse.yaml --root ~/acme\n  multiversectl project install",
		ValidArgsFunction: r.completeNodes(false),
	}
	cmd.Flags().StringVar(&base, "base", "", "directory projects go under (default $MULTIVERSECTL_ROOT or ~/multiverse)")
	cmd.Flags().StringVar(&name, "name", "", "name to record a single project under (default: its own name)")
	cmd.Flags().BoolVar(&all, "all", false, "take every project a repository's catalog lists")
	cmd.Flags().BoolVar(&fetch, "fetch", false, "fetch the catalogs first")
	cmd.Flags().BoolVar(&yes, "yes", false, "accept every default and confirmation")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		// Where it goes is asked only when nothing has said: not --root, --base
		// or $MULTIVERSECTL_ROOT, and not --yes.
		ask := !yes && r.g.Root == "" && base == "" && os.Getenv("MULTIVERSECTL_ROOT") == ""
		return r.install(cmd, args, app.InstallOptions{
			Base: firstNonEmpty(base, os.Getenv("MULTIVERSECTL_ROOT"), "~/multiverse"),
			Root: r.g.Root, Name: name, All: all, Fetch: fetch,
		}, yes, ask)
	}
	return cmd
}

func (r runner) install(cmd *cobra.Command, args []string, o app.InstallOptions, yes, askRoot bool) error {
	ctx := cmd.Context()
	c, err := r.catalogs(yes)
	if err != nil {
		return err
	}
	plan, err := c.PlanInstall(ctx, args, o)
	if err != nil {
		return r.declined(err)
	}
	bare, err := r.f.Bare(*r.g, r.out)
	if err != nil {
		return err
	}

	said := map[string]bool{}
	var failures []error
	var added []app.InstallTarget
	var manifests []namedManifest
	for _, t := range plan {
		if t.How != "" && !said[t.How] {
			said[t.How] = true
			r.out.Info("%s", t.How)
		}
		if t.Skip != "" {
			r.out.Info("%s: %s; `multiversectl project update %s` refreshes it", t.Name, t.Skip, t.Name)
			continue
		}
		if askRoot {
			root, err := r.f.Prompter().Input(fmt.Sprintf("Where should %s live?  (its repositories and universes go here)", t.Name), t.Root)
			if errors.Is(err, context.Canceled) {
				return err
			}
			if err == nil && root != "" {
				t.Root = root
			}
		}
		r.out.Info("installing `%s` into `%s`", t.Name, t.Root)
		in := app.BootstrapInput{Name: t.Name, Root: t.Root, ManifestSource: t.ManifestSource, Ref: t.Ref,
			ManifestPath: t.ManifestPath, From: t.From, Origin: t.Origin}
		entry, err := bare.Bootstrap(ctx, in)
		if err == nil {
			var s *app.Service
			if s, err = r.f.At(*r.g, r.out, entry.Root, entry.Manifest); err == nil {
				if err = s.CloneRepos(ctx); err == nil {
					added = append(added, t)
					manifests = append(manifests, namedManifest{t.Name, s.Manifest})
				}
			}
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", t.Name, err))
			r.out.Warn("%s: %v", t.Name, err)
		}
	}

	r.warnClashes(bare, manifests)
	if len(failures) > 0 {
		return fmt.Errorf("%d of %d projects failed: %w", len(failures), len(plan), errors.Join(failures...))
	}
	r.installedHint(bare, added, args)
	return nil
}

// installedHint says what happened to the current project and what to do
// next, with the real names: the first project on a machine becomes the
// current one, later ones sit beside it.
func (r runner) installedHint(bare *app.Service, added []app.InstallTarget, args []string) {
	if len(added) == 0 {
		return
	}
	names := make([]string, len(added))
	for i, t := range added {
		names[i] = t.Name
	}
	current := ""
	if list, err := bare.Workspaces(); err == nil {
		for _, w := range list {
			if w.Current {
				current = w.Name
			}
		}
	}
	switch {
	case current == added[0].Name && len(added) == 1:
		r.out.OK("installed `%s` and now using it (the first on this machine)", added[0].Name)
		r.out.Info("next: `multiversectl universe new 1 && multiversectl universe up 1`")
	case current != "" && !contains(names, current):
		r.out.OK("installed `%s` (still using `%s`)", strings.Join(names, "`, `"), current)
		r.out.Info("switch: `multiversectl project use %s`", names[0])
	default:
		r.out.OK("installed `%s`; now using `%s`", strings.Join(names, "`, `"), current)
		r.out.Info("next: `multiversectl universe new 1 && multiversectl universe up 1`")
	}
	if added[0].From == "" && len(args) == 1 {
		r.out.Info("keep it in your index: `multiversectl catalog add %s`", args[0])
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

type namedManifest struct {
	name string
	m    *domain.Manifest
}

// warnClashes compares what was just installed with each other and with the
// projects already on this machine. Manifests that cannot be read are left
// out: status is where a broken project is reported.
func (r runner) warnClashes(bare *app.Service, added []namedManifest) {
	if len(added) == 0 {
		return
	}
	var others []namedManifest
	skip := map[string]bool{}
	for _, a := range added {
		skip[a.name] = true
	}
	if known, err := bare.Workspaces(); err == nil {
		for _, w := range known {
			if skip[w.Name] {
				continue
			}
			if s, err := r.f.At(*r.g, r.out, w.Root, w.Manifest); err == nil && s.Manifest != nil {
				others = append(others, namedManifest{w.Name, s.Manifest})
			}
		}
	}
	for i, a := range added {
		for _, b := range append(append([]namedManifest{}, added[i+1:]...), others...) {
			for _, clash := range domain.ProxyClashes(a.name, a.m, b.name, b.m) {
				r.out.Warn("%s", clash)
			}
		}
	}
}

func (r runner) uninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "uninstall <project>",
		Short:             "Forget a project; its files stay on disk",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: r.completeWorkspaces,
		RunE: func(cmd *cobra.Command, args []string) error {
			bare, err := r.f.Bare(*r.g, r.out)
			if err != nil {
				return err
			}
			name, err := r.workspaceArg(cmd, args, bare, "Which project should be forgotten?  (its files stay)")
			if err != nil {
				return err
			}
			return bare.Forget(name)
		},
	}
}

func (r runner) useCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "use <project>",
		Short:             "Make a project the one commands act on by default (a name, or its catalog path)",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: r.completeWorkspaces,
		RunE: func(cmd *cobra.Command, args []string) error {
			bare, err := r.f.Bare(*r.g, r.out)
			if err != nil {
				return err
			}
			name, err := r.workspaceArg(cmd, args, bare, "Which project should commands act on?")
			if err != nil {
				return err
			}
			return bare.Use(name)
		},
	}
}

func (r runner) statusCmd() *cobra.Command {
	var fetch bool
	var format string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "The root catalog, the projects installed here, and what has drifted",
		Long: "Compares each project with where it was installed from: a manifest behind its\n" +
			"remote, repositories its manifest lists that are not cloned, an entry your\n" +
			"catalog dropped or repointed. It uses what was last fetched unless --fetch.",
		Args: cobra.NoArgs,
	}
	cmd.Flags().BoolVar(&fetch, "fetch", false, "fetch first, so it compares with the remotes as they are now")
	cmd.Flags().StringVarP(&format, "output", "o", "table", "output: table or json")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if format != "table" && format != "json" {
			return fmt.Errorf("--output must be table or json, not %q", format)
		}
		c, err := r.catalogs(false)
		if err != nil {
			return err
		}
		var rep app.StatusReport
		err = r.spin(fetch, "Fetching the catalogs", func() (err error) {
			rep, err = c.Status(cmd.Context(), fetch)
			return err
		})
		if err != nil {
			return err
		}
		if format == "json" {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(rep)
		}
		r.printStatus(rep)
		return nil
	}
	return cmd
}

func (r runner) printStatus(rep app.StatusReport) {
	switch {
	case rep.Root.Source == "":
		r.out.Plain("%s  none (install from sources only; `multiversectl init` makes one)", r.out.Bold("root catalog"))
	case rep.Root.Unavailable != "":
		r.out.Plain("%s  %s  %s", r.out.Bold("root catalog"), r.out.Place(domain.Locate(rep.Root.Source, "")), r.out.Red("✗ "+rep.Root.Unavailable))
	default:
		r.out.Plain("%s  %s", r.out.Bold("root catalog"), r.out.Place(domain.Locate(rep.Root.Source, "")))
	}
	if rep.Root.Legacy {
		r.out.Info("the root is a plain Catalog, which a new machine cannot tell from a project's: `multiversectl init` marks it as your root")
	}
	if rep.Root.NoRemote {
		r.out.Warn("the root catalog is local only: it is lost with this machine, and nobody can use it. `git -C %s remote add origin <url>`", filepath.Dir(rep.Root.Source))
	}
	if len(rep.Workspaces) == 0 {
		r.out.Plain("")
		r.out.Plain("nothing installed")
	} else {
		r.out.Plain("")
		rows := make([][]string, len(rep.Workspaces))
		for i, w := range rep.Workspaces {
			name := "  " + w.Name
			if w.Current {
				name = "* " + w.Name
			}
			from := firstNonEmpty(w.From, w.Source, "-")
			state := r.out.Green("up to date")
			if len(w.Notes) > 0 {
				state = r.out.Yellow(strings.Join(w.Notes, " · "))
			}
			rows[i] = []string{name, from, state}
		}
		r.out.Table([]string{"PROJECT", "FROM", "STATE"}, rows)
	}
	if rep.Next != "" {
		r.out.Plain("")
		// Next holds commands joined by a middle dot; each is marked on its own.
		r.out.Info("next: `%s`", strings.Join(strings.Split(rep.Next, "  ·  "), "`  ·  `"))
	}
}

func (r runner) updateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "update [project ...]",
		Short: "Bring installed projects up to date: fetch, move their manifests forward, clone new repositories",
		Long: "Fetches the catalogs, fast-forwards each project's manifest from where it was\n" +
			"installed, and clones any repository the new manifest lists. It refuses a\n" +
			"manifest with local changes, and leaves a pinned one alone. With no names it\n" +
			"updates every project.",
		Example:           "  multiversectl project update\n  multiversectl project update acme",
		ValidArgsFunction: r.completeWorkspaces,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			bare, err := r.f.Bare(*r.g, r.out)
			if err != nil {
				return err
			}
			c, err := r.catalogs(false)
			if err != nil {
				return err
			}
			var list []app.WorkspaceInfo
			if len(args) == 0 {
				if list, err = bare.Workspaces(); err != nil {
					return err
				}
			}
			for _, a := range args {
				w, err := bare.WorkspaceNamed(a)
				if err != nil {
					return err
				}
				list = append(list, w)
			}
			if len(list) == 0 {
				r.out.Info("nothing installed: `multiversectl project install <owner/repo>`")
				return nil
			}
			if _, err := c.Tree(ctx, "", true); err != nil && !strings.Contains(err.Error(), "no root catalog") {
				r.out.Warn("could not fetch your catalog: %v", err)
			}
			var failures []error
			for _, w := range list {
				if err := r.updateOne(ctx, c, w); err != nil {
					failures = append(failures, err)
					r.out.Warn("%v", err)
				}
			}
			if len(failures) > 0 {
				return fmt.Errorf("%d of %d projects failed to update", len(failures), len(list))
			}
			return nil
		},
	}
}

func (r runner) updateOne(ctx context.Context, c *app.Catalogs, w app.WorkspaceInfo) error {
	res, err := c.RefreshWorkspace(ctx, w.Name, w.WorkspaceEntry)
	if err != nil {
		return err
	}
	if res.Skipped != "" {
		r.out.Info("%s: %s", w.Name, res.Skipped)
		return nil
	}
	s, err := r.f.At(*r.g, r.out, w.Root, w.Manifest)
	if err != nil {
		return fmt.Errorf("%s: %w", w.Name, err)
	}
	missing := c.MissingRepos(s.Manifest, w.Root)
	if len(missing) > 0 {
		if err := s.CloneMissing(ctx); err != nil {
			return fmt.Errorf("%s: %w", w.Name, err)
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
		r.out.OK("%s: up to date", w.Name)
		return nil
	}
	r.out.OK("%s: %s", w.Name, strings.Join(did, " · "))
	return nil
}
