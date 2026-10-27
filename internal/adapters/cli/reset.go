package cli

import (
	"github.com/spf13/cobra"

	"github.com/cristiansteib/multiversectl/internal/app"
)

func (r runner) resetCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Forget this machine's root catalog and installed multiverses, to start again",
		Long: "Clears what multiversectl remembers: the root catalog and every installed\n" +
			"multiverse. It deletes no file, no checkout and no universe, so what is on disk\n" +
			"stays, unmanaged, and can be installed again (install reuses checkouts with\n" +
			"--root). Use it to switch to another root: init refuses to while multiverses\n" +
			"installed from the old one exist. Remove universes first with\n" +
			"`multiversectl system prune` if you want them gone. To see everything this tool\n" +
			"has put on the machine, and what to delete for a clean sweep, run\n" +
			"`multiversectl system footprint` first: once the multiverses are forgotten, it cannot find them.",
		Example: "  multiversectl system reset\n  multiversectl system reset --yes",
		Args:    cobra.NoArgs,
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "do not ask")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		c, err := r.catalogs(yes)
		if err != nil {
			return err
		}
		r.warnUniverses(cmd)
		_, err = c.Reset()
		return r.declined(err)
	}
	return cmd
}

// warnUniverses says which installed multiverses still have universes, since
// resetting leaves them on disk with nothing managing them.
func (r runner) warnUniverses(cmd *cobra.Command) {
	bare, err := r.f.Bare(*r.g, r.out)
	if err != nil {
		return
	}
	list, err := bare.Multiverses()
	if err != nil {
		return
	}
	for _, w := range list {
		svc, err := r.f.At(*r.g, r.out, w.Root, w.Manifest)
		if err != nil {
			continue
		}
		items, err := svc.PrunePlan(cmd.Context(), app.PruneOptions{})
		if err != nil || len(items) == 0 {
			continue
		}
		r.out.Warn("`%s` has %d universes in `%s`; they stay on disk and nothing will manage them. `multiversectl system prune` removes them first", w.Name, len(items), w.Root)
	}
}
