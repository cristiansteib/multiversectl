package cli

import (
	"context"
	"os"

	"github.com/spf13/cobra"

	"github.com/cristiansteib/multiversectl/internal/app"
)

// Globals are the flags every command accepts.
type Globals struct {
	Manifest  string
	Root      string
	Workspace string
	NoColor   bool
	Verbose   bool
}

// Factory builds services; main owns it so the CLI never names an adapter.
type Factory struct {
	Version, Commit, Date string

	// Workspace resolves root and manifest from flags, env, config and cwd.
	Workspace func(g Globals, out app.Output) (*app.Service, error)
	// Bare needs no manifest: bootstrap and update.
	Bare func(g Globals, out app.Output) (*app.Service, error)
	// At builds a service for an explicit root and manifest file.
	At func(g Globals, out app.Output, root, manifestPath string) (*app.Service, error)
}

// New returns the root command and the globals its flags fill in.
func New(f Factory) (*cobra.Command, *Globals) {
	g := &Globals{}
	out := &Output{}
	root := &cobra.Command{
		Use:           "multiversectl",
		Short:         "Run numbered, isolated copies of a multi-repo stack side by side",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRun: func(*cobra.Command, []string) {
			*out = *NewOutput(os.Stdout, ColorEnabled(os.Stdout, g.NoColor))
		},
	}
	root.CompletionOptions.HiddenDefaultCmd = true
	flags := root.PersistentFlags()
	flags.StringVarP(&g.Manifest, "manifest", "f", "", "path to the workspace manifest")
	flags.StringVarP(&g.Workspace, "workspace", "w", "", "workspace to act on (default: the one set with `use`)")
	flags.StringVar(&g.Root, "root", "", "workspace root (default $MULTIVERSECTL_ROOT, the manifest's spec.root, config, or the manifest's directory)")
	flags.BoolVar(&g.NoColor, "no-color", false, "disable colour output")
	flags.BoolVarP(&g.Verbose, "verbose", "v", false, "show the commands being run")

	run := runner{g: g, out: out, f: f}
	root.AddCommand(
		run.newCmd(), run.upCmd(), run.downCmd(), run.rmCmd(), run.lsCmd(), run.urlsCmd(),
		run.doctorCmd(), run.envCmd(), run.runCmd(), run.execCmd(), run.bootstrapCmd(), run.workspacesCmd(), run.useCmd(), run.proxyCmd(),
		run.updateCmd(), run.versionCmd(),
	)
	return root, g
}

type runner struct {
	g   *Globals
	out *Output
	f   Factory
}

// withService runs fn against the resolved workspace.
func (r runner) withService(fn func(ctx context.Context, s *app.Service) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		s, err := r.f.Workspace(*r.g, r.out)
		if err != nil {
			return err
		}
		return fn(cmd.Context(), s)
	}
}
