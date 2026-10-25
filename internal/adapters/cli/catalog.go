package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cristiansteib/multiversectl/internal/app"
	"github.com/cristiansteib/multiversectl/internal/domain"
)

func (r runner) catalogCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "catalog",
		Short: "Browse the index of workspaces this machine can install",
	}
	add := &cobra.Command{Use: "add", Short: "List a workspace or another catalog in a catalog"}
	add.AddCommand(r.catalogAddWorkspaceCmd(), r.catalogAddCatalogCmd())
	cmd.AddCommand(r.catalogTreeCmd(), r.catalogSyncCmd(), r.catalogUseCmd(), add, r.catalogCreateCmd(), r.catalogRmCmd())
	return cmd
}

func (r runner) catalogTreeCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "tree [git-url-or-file]",
		Short:   "Show the catalog as a tree; defaults to the root catalog",
		Example: "  multiversectl catalog tree\n  multiversectl catalog tree ./catalog.yaml",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.f.Catalogs(*r.g, r.out)
			if err != nil {
				return err
			}
			node, err := c.Tree(cmd.Context(), localOrURL(args), false)
			if err != nil {
				return err
			}
			for _, line := range renderTree(node) {
				r.out.Plain("%s", line)
			}
			return nil
		},
	}
}

func (r runner) catalogSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Fetch every repository the root catalog refers to, then show what is unavailable",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := r.f.Catalogs(*r.g, r.out)
			if err != nil {
				return err
			}
			node, err := c.Tree(cmd.Context(), "", true)
			if err != nil {
				return err
			}
			bad := unavailable(node)
			for _, n := range bad {
				r.out.Warn("%s: %s", n.Path, n.Unavailable)
			}
			r.out.OK("synced %d workspaces, %d unavailable", len(node.Leaves()), len(bad))
			return nil
		},
	}
}

func (r runner) catalogUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "use <git-url-or-file>",
		Short:   "Record the root catalog for this machine",
		Example: "  multiversectl catalog use git@github.com:me/multiverse-index.git",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.f.Catalogs(*r.g, r.out)
			if err != nil {
				return err
			}
			return c.Use(localOrURL(args))
		},
	}
}

// localOrURL makes a file argument absolute so the saved root still works
// from another directory; URLs and the empty argument pass through.
func localOrURL(args []string) string {
	if len(args) == 0 {
		return ""
	}
	if strings.Contains(args[0], "://") || strings.HasPrefix(args[0], "git@") {
		return args[0]
	}
	if abs, err := filepath.Abs(args[0]); err == nil {
		return abs
	}
	return args[0]
}

func renderTree(root domain.Node) []string {
	lines := []string{root.Name}
	var walk func(n domain.Node, depth int)
	walk = func(n domain.Node, depth int) {
		for _, c := range n.Children {
			line := fmt.Sprintf("%s%-*s %s", strings.Repeat("  ", depth), 24-2*depth, c.Name, c.Kind)
			switch {
			case c.Unavailable != "":
				line += "  unavailable: " + c.Unavailable
			case c.Kind == domain.WorkspaceNode:
				line += "  " + sourceLabel(c.Source)
			}
			lines = append(lines, "  "+strings.TrimRight(line, " "))
			walk(c, depth+1)
		}
	}
	walk(root, 0)
	return lines
}

func sourceLabel(s domain.Source) string {
	label := s.Repo
	if label == "" {
		return s.Path
	}
	if s.Ref != "" {
		label += "@" + s.Ref
	}
	return label + " " + s.Path
}

func unavailable(n domain.Node) []domain.Node {
	var out []domain.Node
	for _, c := range n.Children {
		if c.Unavailable != "" {
			out = append(out, c)
		}
		out = append(out, unavailable(c)...)
	}
	return out
}

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
func atFlag(cmd *cobra.Command, at *string) {
	cmd.Flags().StringVar(at, "at", "", "catalog to put it in, by path (/ is the root); asked when omitted")
}

func (r runner) catalogAddWorkspaceCmd() *cobra.Command {
	var in app.AddWorkspaceInput
	var yes bool
	cmd := &cobra.Command{
		Use:     "workspace <repo-url>",
		Short:   "List a repository's multiverse.yaml in a catalog",
		Example: "  multiversectl catalog add workspace git@github.com:acme/api-workspace.git --at acme/backend",
		Args:    cobra.ExactArgs(1),
	}
	atFlag(cmd, &in.At)
	cmd.Flags().StringVar(&in.Name, "name", "", "name in the catalog (default: the manifest's metadata.name)")
	cmd.Flags().StringVar(&in.Ref, "ref", "", "branch, tag or commit of the repository")
	cmd.Flags().StringVar(&in.Path, "path", "", "manifest path in the repository (default multiverse.yaml)")
	cmd.Flags().BoolVar(&yes, "yes", false, "accept every default and confirmation")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		c, err := r.catalogs(yes)
		if err != nil {
			return err
		}
		in.Repo, in.AtGiven = args[0], cmd.Flags().Changed("at")
		_, err = c.AddWorkspace(cmd.Context(), in)
		return r.declined(err)
	}
	return cmd
}

func (r runner) catalogAddCatalogCmd() *cobra.Command {
	var in app.AddCatalogInput
	var yes bool
	cmd := &cobra.Command{
		Use:     "catalog <repo-url>",
		Short:   "Link a catalog that lives in another repository",
		Example: "  multiversectl catalog add catalog git@github.com:acme/multiverse-index.git --name acme --at /",
		Args:    cobra.ExactArgs(1),
	}
	atFlag(cmd, &in.At)
	cmd.Flags().StringVar(&in.Name, "name", "", "name in the catalog (default: the linked catalog's metadata.name)")
	cmd.Flags().StringVar(&in.Ref, "ref", "", "branch, tag or commit of the repository")
	cmd.Flags().StringVar(&in.Path, "path", "", "catalog path in the repository (default catalog.yaml)")
	cmd.Flags().BoolVar(&yes, "yes", false, "accept every default and confirmation")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		c, err := r.catalogs(yes)
		if err != nil {
			return err
		}
		in.Repo, in.AtGiven = args[0], cmd.Flags().Changed("at")
		_, err = c.AddCatalog(cmd.Context(), in)
		return r.declined(err)
	}
	return cmd
}

func (r runner) catalogCreateCmd() *cobra.Command {
	var in app.CreateCatalogInput
	var yes bool
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a catalog inside another one",
		Long: "Create a catalog and link it from a parent. It can live in the parent's own\n" +
			"repository, in a repository you already have, or in a new private one.",
		Example: "  multiversectl catalog create acme --new-repo acme-index\n  multiversectl catalog create personal --same-repo --at /",
		Args:    cobra.ExactArgs(1),
	}
	atFlag(cmd, &in.At)
	cmd.Flags().BoolVar(&in.Same, "same-repo", false, "keep it as a file in the parent's repository")
	cmd.Flags().StringVar(&in.Where.URL, "repo", "", "put it in this existing repository")
	cmd.Flags().StringVar(&in.Where.Create, "new-repo", "", "create a new repository with this name for it")
	cmd.Flags().BoolVar(&in.Where.Public, "public", false, "make a --new-repo public (private by default)")
	cmd.Flags().BoolVar(&yes, "yes", false, "accept every default and confirmation")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		c, err := r.catalogs(yes)
		if err != nil {
			return err
		}
		in.Name, in.AtGiven = args[0], cmd.Flags().Changed("at")
		return r.declined(c.CreateCatalog(cmd.Context(), in))
	}
	return cmd
}

func (r runner) catalogRmCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:     "rm <path>",
		Short:   "Remove an entry from the catalog that holds it (what it points at is untouched)",
		Example: "  multiversectl catalog rm acme/backend/payments",
		Args:    cobra.ExactArgs(1),
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "accept every confirmation")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		c, err := r.catalogs(yes)
		if err != nil {
			return err
		}
		return r.declined(c.Remove(cmd.Context(), args[0]))
	}
	return cmd
}

func (r runner) initCmd() *cobra.Command {
	var in app.InitInput
	var yes bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Set up this machine's root catalog, in an existing repository or a new private one",
		Long: "The root catalog is the index every other catalog and workspace hangs from.\n" +
			"Without flags this asks where it should live.",
		Example: "  multiversectl init\n  multiversectl init --create multiverse-index\n  multiversectl init --repo git@github.com:me/multiverse-index.git\n  multiversectl init --local ~/multiverse-index",
		Args:    cobra.NoArgs,
	}
	cmd.Flags().StringVar(&in.Where.URL, "repo", "", "use this existing repository")
	cmd.Flags().StringVar(&in.Where.Create, "create", "", "create a new repository with this name")
	cmd.Flags().BoolVar(&in.Where.Public, "public", false, "make a --create repository public (private by default)")
	cmd.Flags().StringVar(&in.Local, "local", "", "use a directory or catalog file on this machine")
	cmd.Flags().StringVar(&in.Name, "name", "", "the catalog's metadata.name (default: from the repository)")
	cmd.Flags().BoolVar(&yes, "yes", false, "accept every default and confirmation")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		c, err := r.catalogs(yes)
		if err != nil {
			return err
		}
		return r.declined(c.Init(cmd.Context(), in))
	}
	return cmd
}

func (r runner) installCmd() *cobra.Command {
	var base string
	var yes, sync bool
	cmd := &cobra.Command{
		Use:   "install [path...]",
		Short: "Set up workspaces from the catalog: a workspace, or everything under a catalog",
		Long: "Each workspace is cloned under <base>/<its catalog path> and recorded, so\n" +
			"`use` and -w can reach it. Without paths this asks which ones.",
		Example: "  multiversectl install acme\n  multiversectl install personal/fichero acme/backend",
	}
	cmd.Flags().StringVar(&base, "base", "", "directory the workspaces go under (default $MULTIVERSECTL_ROOT or ~/multiverse)")
	cmd.Flags().BoolVar(&sync, "sync", false, "fetch the catalogs first")
	cmd.Flags().BoolVar(&yes, "yes", false, "accept every default and confirmation")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return r.install(cmd, args, firstNonEmpty(base, os.Getenv("MULTIVERSECTL_ROOT"), "~/multiverse"), yes, sync)
	}
	return cmd
}

func (r runner) install(cmd *cobra.Command, paths []string, base string, yes, sync bool) error {
	ctx := cmd.Context()
	c, err := r.catalogs(yes)
	if err != nil {
		return err
	}
	tree, err := c.Tree(ctx, "", sync)
	if err != nil {
		return err
	}
	leaves, err := c.Leaves(tree, paths)
	if err != nil {
		return err
	}
	bare, err := r.f.Bare(*r.g, r.out)
	if err != nil {
		return err
	}
	known, err := bare.Workspaces()
	if err != nil {
		return err
	}
	before, taken := "", map[string]string{}
	for _, w := range known {
		taken[w.Name] = w.Root
		if w.Current {
			before = w.Name
		}
	}

	var failures []error
	var added []*domain.Manifest
	for _, leaf := range leaves {
		name := app.WorkspaceSlug(leaf.Path)
		root := filepath.ToSlash(filepath.Join(base, filepath.FromSlash(app.WorkspacePath(leaf.Path))))
		if _, ok := taken[name]; ok {
			r.out.Info("%s is already installed", name)
			continue
		}
		in := app.BootstrapInput{Name: name, Root: root, ManifestSource: leaf.Source.Repo, Ref: leaf.Source.Ref, ManifestPath: leaf.Source.Path}
		if leaf.Source.Repo == "" {
			in.ManifestSource, in.Ref, in.ManifestPath = leaf.Source.Path, "", ""
		}
		entry, err := bare.Bootstrap(ctx, in)
		if err == nil {
			var s *app.Service
			if s, err = r.f.At(*r.g, r.out, entry.Root, entry.Manifest); err == nil {
				if err = s.CloneRepos(ctx); err == nil {
					added = append(added, s.Manifest)
				}
			}
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", leaf.Path, err))
			r.out.Warn("%s: %v", leaf.Path, err)
		}
	}

	if before != "" && len(added) > 0 {
		// Installing is not switching: keep working where the user was.
		if err := bare.Use(before); err != nil {
			return err
		}
	}
	r.warnClashes(known, added)
	if len(failures) > 0 {
		return fmt.Errorf("%d of %d workspaces failed: %w", len(failures), len(leaves), errors.Join(failures...))
	}
	r.out.OK("installed %d workspaces; switch with: multiversectl use <name>", len(added))
	return nil
}

// warnClashes compares what was just installed with each other and with the
// workspaces already on this machine. Manifests that cannot be read are left
// out: doctor is where a broken workspace is reported.
func (r runner) warnClashes(known []app.WorkspaceInfo, added []*domain.Manifest) {
	var others []*domain.Manifest
	for _, w := range known {
		if s, err := r.f.At(*r.g, r.out, w.Root, w.Manifest); err == nil && s.Manifest != nil {
			others = append(others, s.Manifest)
		}
	}
	for i, a := range added {
		for _, b := range added[i+1:] {
			r.warnPair(a, b)
		}
		for _, b := range others {
			r.warnPair(a, b)
		}
	}
}

func (r runner) warnPair(a, b *domain.Manifest) {
	for _, clash := range domain.ProxyClashes(a, b) {
		r.out.Warn("%s", clash)
	}
}

func (r runner) uninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall <workspace>",
		Short: "Forget a workspace; its files stay on disk",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			bare, err := r.f.Bare(*r.g, r.out)
			if err != nil {
				return err
			}
			return bare.Forget(args[0])
		},
	}
}

func (r runner) addCmd() *cobra.Command {
	var in app.AddInput
	var base string
	var yes, install bool
	cmd := &cobra.Command{
		Use:   "add <owner/repo | url>",
		Short: "Join a project: link its catalog, or list its workspace, and optionally install it",
		Long: "The repository is looked at: one with a catalog.yaml is linked as a catalog\n" +
			"(and may list its own workspace and deeper catalogs), one with only a\n" +
			"multiverse.yaml is listed as a workspace. With no root catalog on this\n" +
			"machine yet, init runs first.",
		Example: "  multiversectl add fichero-app/multiverse --install\n  multiversectl add git@github.com:acme/multiverse.git --at personal",
		Args:    cobra.ExactArgs(1),
	}
	atFlag(cmd, &in.At)
	cmd.Flags().StringVar(&in.Name, "name", "", "name in the catalog (default: the one the repository gives itself)")
	cmd.Flags().StringVar(&in.Ref, "ref", "", "branch, tag or commit of the repository")
	cmd.Flags().StringVar(&in.Path, "path", "", "catalog or manifest path in the repository")
	cmd.Flags().BoolVar(&install, "install", false, "install what was just added")
	cmd.Flags().StringVar(&base, "base", "", "with --install: directory the workspaces go under (default $MULTIVERSECTL_ROOT or ~/multiverse)")
	cmd.Flags().BoolVar(&yes, "yes", false, "accept every default and confirmation")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		c, err := r.catalogs(yes)
		if err != nil {
			return err
		}
		in.Repo, in.AtGiven = args[0], cmd.Flags().Changed("at")
		node, err := c.Add(cmd.Context(), in)
		if err != nil {
			return r.declined(err)
		}
		if !install {
			r.out.Info("install it with: multiversectl install %s", node)
			return nil
		}
		return r.install(cmd, []string{node}, firstNonEmpty(base, os.Getenv("MULTIVERSECTL_ROOT"), "~/multiverse"), yes, false)
	}
	return cmd
}
