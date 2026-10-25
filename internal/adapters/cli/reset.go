package cli

import (
	"github.com/spf13/cobra"

	"github.com/cristiansteib/multiversectl/internal/app"
)

func (r runner) resetCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Forget this machine's root catalog and installed workspaces, to start again",
		Long: "Clears what multiversectl remembers: the root catalog and every installed\n" +
			"workspace. It deletes no file, no checkout and no universe, so what is on disk\n" +
			"stays, unmanaged, and can be installed again (install reuses checkouts with\n" +
			"--root). Use it to switch to another root: init refuses to while workspaces\n" +
			"installed from the old one exist. Remove universes first with\n" +
			"`multiversectl system --prune` if you want them gone.",
		Example: "  multiversectl reset\n  multiversectl reset --yes",
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

// warnUniverses says which installed workspaces still have universes, since
// resetting leaves them on disk with nothing managing them.
func (r runner) warnUniverses(cmd *cobra.Command) {
	bare, err := r.f.Bare(*r.g, r.out)
	if err != nil {
		return
	}
	list, err := bare.Workspaces()
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
		r.out.Warn("`%s` has %d universes in `%s`; they stay on disk and nothing will manage them. `multiversectl system --prune` removes them first", w.Name, len(items), w.Root)
	}
}
