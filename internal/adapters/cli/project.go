package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cristiansteib/multiversectl/internal/app"
)

func (r runner) projectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Start a project: the repository that says what it is made of",
		Long: "A project is one repository that is both its index and its own workspace\n" +
			"definition, so anyone can join it with `catalog install owner/repo`.",
		Example: "  multiversectl project init",
		Args:    cobra.NoArgs,
		RunE:    func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(r.projectInitCmd())
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
