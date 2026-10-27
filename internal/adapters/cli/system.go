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

// systemCmd holds what acts on the machine rather than on one multiverse: the
// shared proxy there is one of, the universes of every installed multiverse, and
// what this tool has put on the disk.
//
// `universe` is for one multiverse, chosen with -p; these three were under it
// although none of them is. There is one proxy per machine, and prune and the
// footprint walk every installed multiverse.
func (r runner) systemCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "system",
		Short: "What acts on the whole machine: the shared proxy, pruning universes, what this tool left on disk",
		Long: "`universe` works on one multiverse. These work on the machine: the one proxy every\n" +
			"universe is reached through, universes across every installed multiverse, and\n" +
			"everything this tool has put on the disk.",
		Example: "  multiversectl system proxy status\n  multiversectl system prune --dry-run\n  multiversectl system footprint",
		Args:    cobra.NoArgs,
		RunE:    func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(r.proxyCmd(), r.pruneCmd(), r.footprintCmd(), r.forgetCmd(), r.resetCmd())
	return cmd
}

// forgetCmd stops tracking a multiverse; its files stay on disk.
func (r runner) forgetCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "forget <multiverse>",
		Short:             "Stop tracking a multiverse on this machine; its files stay on disk",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: r.completeMultiverses,
		RunE: func(cmd *cobra.Command, args []string) error {
			bare, err := r.f.Bare(*r.g, r.out)
			if err != nil {
				return err
			}
			name, err := r.multiverseArg(cmd, args, bare, "Which multiverse should be forgotten?  (its files stay)")
			if err != nil {
				return err
			}
			return bare.Forget(name)
		},
	}
}

func (r runner) pruneCmd() *cobra.Command {
	var all, force, yes, dry bool
	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Remove universes across every installed multiverse: the idle ones, or all of them",
		Long: "Removes universes the way `universe rm` does (containers, volumes, worktrees,\n" +
			"directory and state) in every installed multiverse, or in the one named with -w.\n" +
			"It lists what it will do, and keeps, saying why:\n" +
			"  - universes with containers running, unless --all\n" +
			"  - universes with uncommitted work in a worktree, unless --force\n" +
			"It leaves the shared clones, the proxy and the installed multiverses alone. A\n" +
			"branch a universe started is deleted only when every commit on it is already\n" +
			"on the base branch or a remote; one with anything else is kept and named, so\n" +
			"committed work survives. It asks before removing anything, unless --yes;\n" +
			"--dry-run shows the list and stops.",
		Example: "  multiversectl system prune --dry-run\n  multiversectl system prune\n  multiversectl system prune --all --yes\n  multiversectl system prune --all --force -w acme",
		Args:    cobra.NoArgs,
	}
	cmd.Flags().BoolVar(&all, "all", false, "include universes that are running")
	cmd.Flags().BoolVar(&force, "force", false, "include universes with uncommitted work, which is discarded")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask")
	cmd.Flags().BoolVarP(&dry, "dry-run", "n", false, "show what would be removed and stop")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return r.prune(cmd.Context(), app.PruneOptions{All: all, Force: force}, yes, dry)
	}
	return cmd
}

// multiversePlan is one installed multiverse and what a prune would do there.
type multiversePlan struct {
	name  string
	svc   *app.Service
	items []app.PruneItem
}

func (r runner) prune(ctx context.Context, o app.PruneOptions, yes, dry bool) error {
	bare, err := r.f.Bare(*r.g, r.out)
	if err != nil {
		return err
	}
	list, err := bare.Multiverses()
	if err != nil {
		return err
	}
	if r.g.Multiverse != "" {
		w, err := bare.MultiverseNamed(r.g.Multiverse)
		if err != nil {
			return err
		}
		list = []app.MultiverseInfo{w}
	}
	if len(list) == 0 {
		r.out.Info("nothing installed: `multiversectl start <owner/repo>`")
		return nil
	}

	var plans []multiversePlan
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
		plans = append(plans, multiversePlan{name: w.Name, svc: svc, items: items})
	}
	return r.systemPrune(ctx, plans, yes, dry)
}

func (r runner) systemPrune(ctx context.Context, plans []multiversePlan, yes, dry bool) error {
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
	if dry {
		out.Plain("")
		out.Info("dry run: %s would be removed and %d kept; nothing was changed", plural(remove, "universe"), keep)
		return nil
	}
	if !yes {
		out.Plain("")
		out.Plain("%s", out.Red(out.Bold(fmt.Sprintf("!! THIS DESTROYS %s: THEIR CONTAINERS, VOLUMES AND WORKTREES, AND CANNOT BE UNDONE !!", strings.ToUpper(plural(remove, "universe"))))))
		out.Plain("  a branch holding commits that are on no remote is kept; the proxy and the multiverses are not touched")
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
