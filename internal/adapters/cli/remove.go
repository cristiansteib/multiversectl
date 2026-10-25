package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cristiansteib/multiversectl/internal/app"
)

func (r runner) rmCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:     "rm <universe>",
		Aliases: []string{"remove", "destroy"},
		Short:   "Destroy a universe: worktrees, containers, volumes and state (asks first)",
		Example: "  multiversectl universe rm 3\n  multiversectl universe rm fix/login --yes",
		Args:    cobra.MaximumNArgs(1),
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "skip the confirmation prompt")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return r.withService(func(ctx context.Context, s *app.Service) error {
			args, err := r.universeArgs(cmd, args, s)
			if err != nil {
				return err
			}
			if !yes {
				if err := r.confirm(ctx, s, args[0]); err != nil {
					return err
				}
			}
			return s.Remove(ctx, app.RemoveInput{Universe: args[0]})
		})(cmd, args)
	}
	return cmd
}

// confirm prints everything rm will destroy and requires the universe's name
// typed back.
func (r runner) confirm(ctx context.Context, s *app.Service, arg string) error {
	st, err := s.Resolve(arg)
	if err != nil {
		return err
	}
	u := s.Universe(st.Name)
	out := r.out

	out.Plain("")
	out.Plain("%s", out.Red(out.Bold(fmt.Sprintf("!! THIS DESTROYS UNIVERSE %s AND CANNOT BE UNDONE !!", st.Name))))
	out.Plain("")
	out.Plain("  worktrees (uncommitted work in them is lost):")
	for _, repoKey := range sortedKeys(st.Branches) {
		out.Plain("    %s  branch %s", u.RepoDir(repoKey), st.Branches[repoKey])
	}
	out.Plain("  containers of project %s:", st.Project)
	containers, err := s.Runtime.Containers(ctx, st.Project)
	if err != nil {
		out.Plain("    (could not list: %v)", err)
	}
	for _, c := range containers {
		out.Plain("    %s  %s", c.Name, c.Status)
	}
	out.Plain("  volumes of project %s (all data in them)", st.Project)
	out.Plain("  state file for universe %s", st.Name)
	out.Plain("")

	fmt.Fprintf(os.Stdout, "Type %s to confirm, anything else aborts: ", out.Bold(st.Name))
	answer, err := readLine(ctx)
	if ctx.Err() != nil {
		fmt.Fprintln(os.Stdout)
		return ctx.Err()
	}
	if err != nil || strings.TrimSpace(answer) != st.Name {
		fmt.Fprintln(os.Stdout)
		return fmt.Errorf("aborted: nothing was removed")
	}
	return nil
}
