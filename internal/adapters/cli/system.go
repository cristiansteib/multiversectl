package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cristiansteib/multiversectl/internal/app"
)

// systemCmd is the machine-wide view: every installed workspace, not the one
// commands act on by default, which is what makes a prune across them possible.
func (r runner) systemCmd() *cobra.Command {
	var prune, all, force, yes bool
	cmd := &cobra.Command{
		Use:   "system",
		Short: "What universes take up on this machine, across every workspace; --prune removes them",
		Long: "With no flags, lists every installed workspace with how many universes it has,\n" +
			"how many containers run, and how many universes hold uncommitted work.\n\n" +
			"--prune removes universes the way `universe rm` does (containers, volumes,\n" +
			"worktrees, directory and state) for every installed workspace, or for the one\n" +
			"named with -w. It keeps, and says why:\n" +
			"  - universes with containers running, unless --all\n" +
			"  - universes with uncommitted work in a worktree, unless --force\n" +
			"It leaves the shared clones, the proxy and the installed workspaces alone. A\n" +
			"branch a universe started is deleted only when every commit on it is already\n" +
			"on the base branch or a remote; one with anything else is kept and named, so\n" +
			"committed work survives. It lists what it will remove and asks, unless --yes.",
		Example: "  multiversectl system\n  multiversectl system --prune\n  multiversectl system --prune --all --yes\n  multiversectl system --prune --all --force -w acme",
		Args:    cobra.NoArgs,
	}
	cmd.Flags().BoolVar(&prune, "prune", false, "remove universes (see above for which)")
	cmd.Flags().BoolVar(&all, "all", false, "with --prune: include universes that are running")
	cmd.Flags().BoolVar(&force, "force", false, "with --prune: include universes with uncommitted work, which is discarded")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "with --prune: do not ask")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if !prune && (all || force || yes) {
			return errors.New("--all, --force and --yes only apply with --prune")
		}
		return r.system(cmd.Context(), prune, app.PruneOptions{All: all, Force: force}, yes)
	}
	return cmd
}

// workspacePlan is one installed workspace and what a prune would do there.
type workspacePlan struct {
	name  string
	svc   *app.Service
	items []app.PruneItem
}

func (r runner) system(ctx context.Context, prune bool, o app.PruneOptions, yes bool) error {
	bare, err := r.f.Bare(*r.g, r.out)
	if err != nil {
		return err
	}
	list, err := bare.Workspaces()
	if err != nil {
		return err
	}
	if r.g.Workspace != "" {
		w, err := bare.WorkspaceNamed(r.g.Workspace)
		if err != nil {
			return err
		}
		list = []app.WorkspaceInfo{w}
	}
	if len(list) == 0 {
		r.out.Info("nothing installed: `multiversectl catalog install <owner/repo>`")
		return nil
	}

	var plans []workspacePlan
	for _, w := range list {
		svc, err := r.f.At(*r.g, r.out, w.Root, w.Manifest)
		if err != nil {
			r.out.Warn("%s: cannot read its manifest, so its universes are not counted: %v", w.Name, err)
			continue
		}
		items, err := svc.PrunePlan(ctx, o)
		if err != nil {
			r.out.Warn("%s: %v", w.Name, err)
			continue
		}
		plans = append(plans, workspacePlan{name: w.Name, svc: svc, items: items})
	}
	if !prune {
		r.systemOverview(plans)
		return nil
	}
	return r.systemPrune(ctx, plans, yes)
}

func (r runner) systemOverview(plans []workspacePlan) {
	rows := make([][]string, 0, len(plans))
	var universes int
	for _, p := range plans {
		running, dirty := 0, 0
		for _, it := range p.items {
			running += it.Running
			if len(it.Dirty) > 0 {
				dirty++
			}
		}
		universes += len(p.items)
		rows = append(rows, []string{p.name, fmt.Sprint(len(p.items)), fmt.Sprint(running), fmt.Sprint(dirty)})
	}
	r.out.Table([]string{"WORKSPACE", "UNIVERSES", "CONTAINERS UP", "WITH UNCOMMITTED WORK"}, rows)
	if universes > 0 {
		r.out.Plain("")
		r.out.Info("remove the idle ones: `multiversectl system --prune`   (--all includes running, --force uncommitted work)")
	}
}

func (r runner) systemPrune(ctx context.Context, plans []workspacePlan, yes bool) error {
	out := r.out
	remove, keep := 0, 0
	for _, p := range plans {
		if len(p.items) == 0 {
			continue
		}
		rows := make([][]Cell, 0, len(p.items))
		for _, it := range p.items {
			if it.Skip == "" {
				remove++
				rows = append(rows, []Cell{{Text: it.Name}, {Text: fmt.Sprint(it.Running)}, {Text: "remove", Style: red}})
				continue
			}
			keep++
			rows = append(rows, []Cell{{Text: it.Name}, {Text: fmt.Sprint(it.Running)}, {Text: "keep: " + it.Skip, Style: dim}})
		}
		out.Plain("")
		out.Plain("%s", out.Bold(p.name))
		out.Cells([]string{"UNIVERSE", "RUNNING", "WHAT HAPPENS"}, rows)
	}
	if remove == 0 {
		out.Plain("")
		out.Info("nothing to remove (%d kept)", keep)
		return nil
	}
	if !yes {
		out.Plain("")
		out.Plain("%s", out.Red(out.Bold(fmt.Sprintf("!! THIS DESTROYS %s: THEIR CONTAINERS, VOLUMES AND WORKTREES, AND CANNOT BE UNDONE !!", strings.ToUpper(plural(remove, "universe"))))))
		out.Plain("  a branch holding commits that are on no remote is kept; the proxy and the workspaces are not touched")
		fmt.Fprintf(os.Stdout, "Type %s to confirm, anything else aborts: ", out.Bold("prune"))
		answer, err := readLine(ctx)
		if ctx.Err() != nil {
			fmt.Fprintln(os.Stdout)
			return ctx.Err()
		}
		if err != nil || strings.TrimSpace(answer) != "prune" {
			fmt.Fprintln(os.Stdout)
			return errors.New("aborted: nothing was removed (without a terminal, pass --yes)")
		}
	}

	var failed []string
	removed := 0
	for _, p := range plans {
		n, err := p.svc.Prune(ctx, p.items)
		removed += n
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", p.name, err))
		}
	}
	out.OK("removed %s, kept %d", plural(removed, "universe"), keep)
	if len(failed) > 0 {
		return fmt.Errorf("some universes could not be removed:\n  %s", strings.Join(failed, "\n  "))
	}
	return nil
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
