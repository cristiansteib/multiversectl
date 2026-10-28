package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cristiansteib/multiversectl/internal/app"
)

// catalogNewCmd makes one more entry for the catalog and lists it: a
// multiverse, defined by a new repository of its own, or a catalog that groups
// others. They are asked apart because they are different files, but they are
// one verb: both end up in the tree.
func (r runner) catalogNewCmd() *cobra.Command {
	var in app.ProjectInput
	var group app.CreateCatalogInput
	var repos []string
	var kind string
	var yes, noInstall bool
	cmd := &cobra.Command{
		Use:   "new [dir | name]",
		Short: "Make a multiverse, or a catalog to group them, and list it",
		Long: "Asks which: a multiverse (which repositories are needed and how to run\n" +
			"universes of them) or a catalog (an index that groups multiverses and other\n" +
			"catalogs). Pass --type to skip the question.\n\n" +
			"For a multiverse, run it in the directory its repository will be (an empty\n" +
			"one, or a checkout of a repository you made for it). It asks, in turn:\n" +
			"  1. its name\n" +
			"  2. its repositories: the git checkouts already beside it, a search through\n" +
			"     your repositories on GitHub or GitLab, or URLs you paste\n" +
			"  3. where its own repository lives: a new private one under any account or\n" +
			"     organization you can create in, an existing one, or only here\n" +
			"  4. where to list it in your catalog\n" +
			"  5. whether to start it on this machine now\n" +
			"It then writes multiverse.yaml, commits it and pushes. Nothing is created or\n" +
			"pushed until you have seen the plan and said yes.\n\n" +
			"For a catalog the argument is its name; it can be a file in its parent's\n" +
			"repository (--same-repo), or live in an existing (--url) or new (--create)\n" +
			"repository.\n\n" +
			"Every answer has a flag, so with all of them it asks nothing and runs in a script.\n\n" +
			"A repository is named in the manifest after its own name, lowercased, without the\n" +
			"multiverse's name and a dash in front (acme-api in acme is api); --repo name=url\n" +
			"says the name instead. A self-hosted GitLab or GitHub: --create group/name\n" +
			"--provider gitlab --host gitlab.example.test, or --create with the full URL.",
		Example: "  multiversectl catalog new\n" +
			"  multiversectl catalog new ~/acme/multiverse --scan --create acme-org/multiverse\n" +
			"  multiversectl catalog new --name acme --repo acme-org/api --repo web=https://gitlab.com/acme/web.git --local\n" +
			"  multiversectl catalog new . --repo git@gitlab.example.test:acme/api.git --create https://gitlab.example.test/acme/multiverse --yes\n" +
			"  multiversectl catalog new backend --type catalog --same-repo --at /",
		Args: cobra.MaximumNArgs(1),
	}
	f := cmd.Flags()
	f.StringVar(&kind, "type", "", "what to make: multiverse or catalog (asked when omitted)")
	f.StringVar(&in.Name, "name", "", "the multiverse's name (default: from the directory)")
	f.StringArrayVar(&repos, "repo", nil, "a repository to include: a git URL or owner/repo, or name=url to set the name it has in the manifest (repeatable)")
	f.BoolVar(&in.Scan, "scan", false, "include the git checkouts found beside it, without asking")
	f.StringVar(&in.Create, "create", "", "create the repository for it, as owner/name (private)")
	f.StringVar(&in.Provider, "provider", "", "the service --create is on: github or gitlab (default: github)")
	f.StringVar(&in.Host, "host", "", "a self-hosted instance of --provider, such as gitlab.example.test (also inferred from a full --create URL)")
	f.BoolVar(&in.Public, "public", false, "make a --create repository public (private by default)")
	f.StringVar(&in.URL, "url", "", "use an existing repository for it")
	f.BoolVar(&in.Local, "local", false, "keep it on this machine for now: commit, but do not push")
	f.BoolVar(&group.Same, "same-repo", false, "a catalog: keep it as a file in its parent's repository")
	r.atFlag(cmd, &in.At)
	f.BoolVar(&in.NoLink, "no-link", false, "do not list it in the catalog")
	f.BoolVar(&noInstall, "no-install", false, "do not offer to start it on this machine")
	f.BoolVar(&yes, "yes", false, "accept every confirmation (it is listed in the root unless --at says otherwise)")
	_ = cmd.RegisterFlagCompletionFunc("type", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return []string{"multiverse", "catalog"}, cobra.ShellCompDirectiveNoFileComp
	})
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if kind == "" {
			if len(repos) > 0 || in.Scan {
				kind = "multiverse"
			} else {
				i, err := r.f.Prompter().Select("What do you want to make?", []app.Option{
					{Label: "multiverse", Note: "which repositories are needed, and how to run universes of them"},
					{Label: "catalog", Note: "an index that groups multiverses and other catalogs"},
				}, 0)
				if err = asked(cmd, args, err); err != nil {
					return fmt.Errorf("%w; pass --type multiverse or --type catalog", err)
				}
				kind = []string{"multiverse", "catalog"}[i]
			}
		}
		c, err := r.catalogs(yes)
		if err != nil {
			return err
		}
		switch kind {
		case "catalog":
			name, err := r.textArg(cmd, args, "Name of the catalog", "")
			if err != nil {
				return err
			}
			group.Name, group.At, group.AtGiven = name, in.At, cmd.Flags().Changed("at")
			group.Where.URL, group.Where.Create, group.Where.Public = in.URL, in.Create, in.Public
			node, err := c.CreateCatalog(cmd.Context(), group)
			if err != nil {
				return r.declined(err)
			}
			r.out.Info("next: `multiversectl catalog add <owner/repo> --at %s`", node)
			return nil
		case "multiverse":
		default:
			return fmt.Errorf("--type must be multiverse or catalog, not %q", kind)
		}
		if len(args) == 1 {
			in.Dir = args[0]
		}
		in.Repos = repos
		in.AtGiven = cmd.Flags().Changed("at")
		res, err := c.InitProject(cmd.Context(), in)
		if err != nil {
			return r.declined(err)
		}
		r.out.OK("multiverse `%s` is ready in `%s`", res.Name, res.Dir)
		r.summarize(res)
		if noInstall {
			return nil
		}
		return r.offerInstall(cmd, c, res, yes)
	}
	return cmd
}

// summarize says what a new multiverse is, for whoever has to check it without
// reading the files: the names its repositories have, what was written and
// where it went.
func (r runner) summarize(res app.ProjectResult) {
	for _, repo := range res.Repos {
		r.out.Info("repo %s = %s", repo.Key, repo.URL)
	}
	if len(res.Files) > 0 {
		r.out.Info("files: %s", strings.Join(res.Files, ", "))
	}
	switch {
	case res.Pushed != "":
		r.out.Info("pushed to %s (%s)", res.Pushed, res.Branch)
	case res.URL == "":
		r.out.Info("not pushed: only on this machine")
	}
	if res.Node != "" {
		r.out.Info("listed in the root catalog as `%s`; check with `multiversectl catalog list --json`", res.Node)
	}
	r.out.Info("next: multiverse.yaml only lists the repositories. A repository with a compose file runs as an app of its own name; to give an app its hosts (addresses), secrets or services, add `spec.apps` to it, commit, push and run `multiversectl catalog update` (reference: https://multiversectl.dev/reference/manifest.html)")
}

// offerInstall is the guide's last step: put the multiverse on this machine, so
// the next command can be universe new.
func (r runner) offerInstall(cmd *cobra.Command, c *app.Catalogs, res app.ProjectResult, yes bool) error {
	bare, err := r.f.Bare(*r.g, r.out)
	if err != nil {
		return err
	}
	if list, err := bare.Multiverses(); err == nil {
		for _, w := range list {
			if w.Name == res.Name {
				r.out.Info("a multiverse named `%s` is already installed (`%s`); to follow the new repository instead: `multiversectl system forget %s`, then `multiversectl start %s`",
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
		ok, err = c.Prompt.Confirm(fmt.Sprintf("Start %s on this machine now, %s?", res.Name, where), true)
		if errors.Is(err, app.ErrNotInteractive) {
			ok, err = false, nil
		}
		if err != nil {
			return err
		}
	}
	if !ok {
		hint := "multiversectl start " + src
		if res.Root != "" {
			hint += " --root " + res.Root
		}
		r.out.Info("start it later with: `%s`", hint)
		return nil
	}
	ask := !yes && r.g.Root == "" && res.Root == "" && os.Getenv("MULTIVERSECTL_ROOT") == ""
	_, err = r.install(cmd, []string{src}, app.InstallOptions{
		Base: firstNonEmpty(os.Getenv("MULTIVERSECTL_ROOT"), "~/multiverse"), Root: firstNonEmpty(r.g.Root, res.Root),
	}, yes, ask)
	return err
}

// installSource is the best way to name the multiverse to install: its place in
// the root catalog, else its repository, else the directory it is in.
func installSource(res app.ProjectResult) string {
	return strings.TrimSpace(firstNonEmpty(res.Node, res.URL, res.Dir))
}
