package cli

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cristiansteib/multiversectl/internal/app"
)

// completionCmd replaces cobra's, which can only print a script and leave the
// installing to whoever reads the docs. With no argument it installs; with a
// shell's name it prints, as before.
func (r runner) completionCmd(root *cobra.Command) *cobra.Command {
	var shell string
	var yes, remove bool
	cmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Install shell completions, or print the script of a shell",
		Long: "With no argument, installs completions for your shell (from $SHELL, or --shell)\n" +
			"under your home directory, after asking. The file records the version that\n" +
			"wrote it, and any later run of multiversectl rewrites it when the version has\n" +
			"changed, so an upgrade does not leave stale completions behind. zsh also gets\n" +
			"a marked block in its startup file; --uninstall removes everything.\n\n" +
			"With a shell's name, prints the script to stdout instead.",
		Example: "  multiversectl completion\n  multiversectl completion --yes\n  multiversectl completion --uninstall\n  multiversectl completion zsh > _multiversectl",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			comps := r.completions()
			s, err := comps.DetectShell()
			if shell != "" {
				s, err = app.Shell(shell), nil
			}
			if err != nil {
				return err
			}
			if remove {
				return comps.Uninstall(s)
			}
			return comps.Install(s, scripts(root), yes)
		},
	}
	cmd.Flags().StringVar(&shell, "shell", "", "bash, zsh or fish (default: from $SHELL)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "install without asking")
	cmd.Flags().BoolVar(&remove, "uninstall", false, "remove what was installed")
	_ = cmd.RegisterFlagCompletionFunc("shell", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return []string{"bash", "zsh", "fish"}, cobra.ShellCompDirectiveNoFileComp
	})
	for _, name := range []string{"bash", "zsh", "fish", "powershell"} {
		name := name
		cmd.AddCommand(&cobra.Command{
			Use:                   name,
			Short:                 "Print the " + name + " completion script",
			Args:                  cobra.NoArgs,
			DisableFlagsInUseLine: true,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return printScript(root, name, cmd.OutOrStdout())
			},
		})
	}
	return cmd
}

func (r runner) completions() *app.Completions {
	return r.f.Completions(r.out)
}

func printScript(root *cobra.Command, shell string, w io.Writer) error {
	switch shell {
	case "bash":
		return root.GenBashCompletionV2(w, true)
	case "zsh":
		return root.GenZshCompletion(w)
	case "fish":
		return root.GenFishCompletion(w, true)
	case "powershell":
		return root.GenPowerShellCompletionWithDesc(w)
	}
	return fmt.Errorf("no completion script for %q", shell)
}

func scripts(root *cobra.Command) app.ScriptFunc {
	return func(s app.Shell) ([]byte, error) {
		var b bytes.Buffer
		err := printScript(root, string(s), &b)
		return b.Bytes(), err
	}
}

// refreshCompletions keeps an installed script in step with this binary. It
// skips the commands that are themselves about completion, where a rewrite
// would race with what the shell is asking for.
func (r runner) refreshCompletions(root, cmd *cobra.Command) {
	if r.f.Completions == nil {
		return
	}
	switch name := cmd.Name(); {
	case name == "completion", strings.HasPrefix(name, cobra.ShellCompRequestCmd),
		cmd.Parent() != nil && cmd.Parent().Name() == "completion":
		return
	}
	r.completions().Refresh(scripts(root))
}
