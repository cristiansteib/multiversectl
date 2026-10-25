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
)

func (r runner) projectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "project",
		Aliases: []string{"p"},
		Short:   "List, start and add projects: the repositories that say what a project is made of",
		Long: "A project is one repository that is both its index and its own workspace\n" +
			"definition, so anyone can join it with `catalog install owner/repo`. `list` shows\n" +
			"the projects in your catalog and which are installed here, `init` starts a new one,\n" +
			"and `add` puts an existing one in your catalog.",
		Example: "  multiversectl project list\n  multiversectl p init\n  multiversectl p add acme/multiverse",
		Args:    cobra.NoArgs,
		RunE:    func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(r.projectListCmd(), r.projectInitCmd(), r.projectAddCmd())
	return cmd
}

func (r runner) projectInitCmd() *cobra.Command {
	var in app.ProjectInput
	var repos []string
	var yes, noInstall bool
	cmd := &cobra.Command{
		Use:   "init [dir]",
		Short: "Guide the setup of a project: pick its repositories, where it lives, and where to list it",
		Long: "Run it in the directory the project's repository will be (an empty one, or a\n" +
			"checkout of a repository you made for it). It asks, in turn:\n" +
			"  1. the project's name\n" +
			"  2. its repositories: the git checkouts already beside it, a search through\n" +
			"     your repositories on GitHub or GitLab, or URLs you paste\n" +
			"  3. where the project's own repository lives: a new private one under any\n" +
			"     account or organization you can create in, an existing one, or only here\n" +
			"  4. where to list it in your root catalog\n" +
			"  5. whether to install it on this machine now\n" +
			"It then writes multiverse.yaml and catalog.yaml, commits them and pushes. Nothing\n" +
			"is created or pushed until you have seen the plan and said yes.\n\n" +
			"Every answer has a flag, so with all of them it asks nothing and runs in a script.",
		Example: "  multiversectl project init\n" +
			"  multiversectl project init ~/acme/multiverse --scan --create acme-org/multiverse\n" +
			"  multiversectl project init --name acme --repo acme-org/api --repo https://gitlab.com/acme/web.git --local",
		Args: cobra.MaximumNArgs(1),
	}
	f := cmd.Flags()
	f.StringVar(&in.Name, "name", "", "the project's name (default: from the directory)")
	f.StringArrayVar(&repos, "repo", nil, "a repository to include: a git URL or owner/repo (repeatable)")
	f.BoolVar(&in.Scan, "scan", false, "include the git checkouts found beside it, without asking")
	f.StringVar(&in.Create, "create", "", "create the project's repository, as owner/name (private)")
	f.StringVar(&in.Provider, "provider", "", "the service --create is on: github or gitlab (default: github)")
	f.BoolVar(&in.Public, "public", false, "make a --create repository public (private by default)")
	f.StringVar(&in.URL, "url", "", "use an existing repository for the project")
	f.BoolVar(&in.Local, "local", false, "keep the project on this machine for now: commit, but do not push")
	r.atFlag(cmd, &in.At)
	f.BoolVar(&in.NoLink, "no-link", false, "do not list it in the root catalog")
	f.BoolVar(&noInstall, "no-install", false, "do not offer to install it on this machine")
	f.BoolVar(&yes, "yes", false, "accept every confirmation (it is listed in the root unless --at says otherwise)")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) == 1 {
			in.Dir = args[0]
		}
		in.Repos = repos
		in.AtGiven = cmd.Flags().Changed("at")
		c, err := r.catalogs(yes)
		if err != nil {
			return err
		}
		res, err := c.InitProject(cmd.Context(), in)
		if err != nil {
			return r.declined(err)
		}
		r.out.OK("project `%s` is ready in `%s`", res.Name, res.Dir)
		if noInstall {
			return nil
		}
		return r.offerInstall(cmd, c, res, yes)
	}
	return cmd
}

// offerInstall is the guide's last step: put the project on this machine, so
// the next command can be universe new.
func (r runner) offerInstall(cmd *cobra.Command, c *app.Catalogs, res app.ProjectResult, yes bool) error {
	bare, err := r.f.Bare(*r.g, r.out)
	if err != nil {
		return err
	}
	if list, err := bare.Workspaces(); err == nil {
		for _, w := range list {
			if w.Name == res.Name {
				r.out.Info("a workspace named `%s` is already installed (`%s`); to follow the new repository instead: `multiversectl catalog uninstall %s`, then `multiversectl catalog install %s`",
					res.Name, firstNonEmpty(w.From, w.Manifest), res.Name, installSource(res))
				return nil
			}
		}
	}
	src := installSource(res)
	where := "under the default directory"
	if res.Root != "" {
		where = "in " + res.Root + ", where its checkouts already are"
	}
	ok := yes
	if !yes {
		var err error
		ok, err = c.Prompt.Confirm(fmt.Sprintf("Install %s on this machine now, %s?", res.Name, where), true)
		if errors.Is(err, app.ErrNotInteractive) {
			ok, err = false, nil
		}
		if err != nil {
			return err
		}
	}
	if !ok {
		hint := "multiversectl catalog install " + src
		if res.Root != "" {
			hint += " --root " + res.Root
		}
		r.out.Info("install it later with: `%s`", hint)
		return nil
	}
	ask := !yes && r.g.Root == "" && res.Root == "" && os.Getenv("MULTIVERSECTL_ROOT") == ""
	return r.install(cmd, []string{src}, app.InstallOptions{
		Base: firstNonEmpty(os.Getenv("MULTIVERSECTL_ROOT"), "~/multiverse"), Root: firstNonEmpty(r.g.Root, res.Root),
	}, yes, ask)
}

// installSource is the best way to name the project to install: its place in
// the root catalog, else its repository, else the directory it is in.
func installSource(res app.ProjectResult) string {
	return strings.TrimSpace(firstNonEmpty(res.Node, res.URL, res.Dir))
}

// projectAddCmd is `catalog add` under the name people look for: putting a
// project they already have into their catalog.
func (r runner) projectAddCmd() *cobra.Command {
	cmd := r.catalogAddCmd()
	cmd.Use = "add [owner/repo | url | path]"
	cmd.Short = "Put an existing project in your catalog, in the place you pick"
	cmd.Example = "  multiversectl project add acme/multiverse\n" +
		"  multiversectl project add ~/code/billing --at acme/backend\n" +
		"  multiversectl project add ./acme.yaml --new-repo acme-org/multiverse"
	return cmd
}

// projectRow is one project as `project list` shows it.
type projectRow struct {
	Project     string `json:"project"`
	Repository  string `json:"repository,omitempty"`
	InstalledAs string `json:"installedAs,omitempty"`
	Current     bool   `json:"current,omitempty"`
	State       string `json:"state,omitempty"`
	InCatalog   bool   `json:"inCatalog"`
}

func (r runner) projectListCmd() *cobra.Command {
	var fetch bool
	var format string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the projects in your catalog, and which are installed here",
		Long: "Every workspace in your root catalog, with the repository it lives in, the name\n" +
			"it is installed under on this machine and whether it has drifted. Projects\n" +
			"installed here from outside the catalog are listed too. It reads what was last\n" +
			"fetched unless --fetch.",
		Example: "  multiversectl project list\n  multiversectl p ls --fetch\n  multiversectl p ls -o json",
		Args:    cobra.NoArgs,
	}
	cmd.Flags().BoolVar(&fetch, "fetch", false, "fetch the catalogs first")
	cmd.Flags().StringVarP(&format, "output", "o", "table", "output: table or json")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if format != "table" && format != "json" {
			return fmt.Errorf("--output must be table or json, not %q", format)
		}
		c, err := r.catalogs(false)
		if err != nil {
			return err
		}
		var rows []projectRow
		err = r.spin(fetch, "Fetching the catalogs", func() (err error) { rows, err = r.projectRows(cmd.Context(), c, fetch); return err })
		if err != nil {
			return err
		}
		if format == "json" {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(rows)
		}
		if len(rows) == 0 {
			r.out.Info("no projects yet: `multiversectl project init` starts one, `multiversectl project add <owner/repo>` adds one you have")
			return nil
		}
		cells := make([][]Cell, len(rows))
		for i, p := range rows {
			installed := Cell{Text: "—", Style: dim}
			if p.InstalledAs != "" {
				mark := "✓ " + p.InstalledAs
				if p.Current {
					mark += " (current)"
				}
				installed = Cell{Text: mark, Style: green}
			}
			state := Cell{Text: p.State}
			switch {
			case !p.InCatalog:
				state = Cell{Text: "not in your catalog", Style: yellow}
			case p.State != "" && p.State != "up to date":
				state.Style = yellow
			case p.State == "up to date":
				state.Style = dim
			}
			cells[i] = []Cell{{Text: p.Project}, {Text: firstNonEmpty(p.Repository, "—"), Style: dim}, installed, state}
		}
		r.out.Cells([]string{"PROJECT", "REPOSITORY", "INSTALLED", "STATE"}, cells)
		return nil
	}
	return cmd
}

// projectRows joins what the catalog lists with what is installed. A root that
// cannot be read is not an error here: what is installed is still worth listing.
func (r runner) projectRows(ctx context.Context, c *app.Catalogs, fetch bool) ([]projectRow, error) {
	rep, err := c.Status(ctx, fetch)
	if err != nil {
		return nil, err
	}
	byName := map[string]app.WorkspaceStatus{}
	for _, w := range rep.Workspaces {
		byName[w.Name] = w
	}
	var rows []projectRow
	listed := map[string]bool{}
	if tree, err := c.Tree(ctx, "", fetch); err == nil {
		installed, _ := c.InstalledIn(tree)
		for _, leaf := range tree.Leaves() {
			p := projectRow{Project: leaf.Path, Repository: shortRepoName(leaf.Source.Repo), InCatalog: true}
			if p.Repository == "" {
				p.Repository = filepath.Base(leaf.Source.Path)
			}
			if name := installed[leaf.Path]; name != "" {
				w := byName[name]
				p.InstalledAs, p.Current, p.State = name, w.Current, stateOf(w)
				listed[name] = true
			}
			rows = append(rows, p)
		}
	}
	for _, w := range rep.Workspaces {
		if !listed[w.Name] {
			rows = append(rows, projectRow{Project: w.Name, Repository: firstNonEmpty(w.Source, "a local manifest"), InstalledAs: w.Name, Current: w.Current, State: stateOf(w)})
		}
	}
	return rows, nil
}

func stateOf(w app.WorkspaceStatus) string {
	if len(w.Notes) > 0 {
		return strings.Join(w.Notes, " · ")
	}
	return "up to date"
}
