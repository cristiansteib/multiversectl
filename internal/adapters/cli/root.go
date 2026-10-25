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
	// Bare needs no manifest: install, status, update and the workspace list.
	Bare func(g Globals, out app.Output) (*app.Service, error)
	// At builds a service for an explicit root and manifest file.
	At func(g Globals, out app.Output, root, manifestPath string) (*app.Service, error)
	// Catalogs builds the catalog service, which never needs a workspace.
	Catalogs func(g Globals, out app.Output) (*app.Catalogs, error)
	// Prompter asks questions on the terminal.
	Prompter func() app.Prompter
	// Progress shows a spinner for a slow step; nil means none.
	Progress func() app.Progress
	// Server builds the port that starts and stops the web view process.
	Server func() (app.ServerProcess, error)
	// Completions builds the use case that installs and refreshes completions.
	Completions func(out app.Output) *app.Completions
}

// New returns the root command and the globals its flags fill in.
func New(f Factory) (*cobra.Command, *Globals) {
	g := &Globals{}
	out := &Output{}
	// The pre-run hook needs the command it hangs on and the runner built after it.
	var run runner
	var root *cobra.Command
	root = &cobra.Command{
		Use:           "multiversectl",
		Short:         "Run numbered, isolated copies of a multi-repo stack side by side",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			*out = *NewOutput(os.Stdout, ColorEnabled(os.Stdout, g.NoColor))
			run.refreshCompletions(root, cmd)
		},
	}
	flags := root.PersistentFlags()
	flags.StringVarP(&g.Manifest, "manifest", "f", "", "path to the workspace manifest")
	flags.StringVarP(&g.Workspace, "workspace", "w", "", "workspace to act on (default: the one chosen with the use command)")
	flags.StringVar(&g.Root, "root", "", "workspace root (default $MULTIVERSECTL_ROOT, the manifest's spec.root, config, or the manifest's directory)")
	flags.BoolVar(&g.NoColor, "no-color", false, "disable colour output")
	flags.BoolVarP(&g.Verbose, "verbose", "v", false, "show the commands being run")

	run = runner{g: g, out: out, f: f}
	root.CompletionOptions.DisableDefaultCmd = true
	root.AddCommand(
		run.universeCmd(), run.machineStatusCmd(), run.serverCmd(), run.catalogCmd(), run.projectCmd(), run.initCmd(), run.resetCmd(), run.completionCmd(root),
		run.selfUpdateCmd(), run.versionCmd(),
	)
	_ = root.RegisterFlagCompletionFunc("workspace", run.completeWorkspaces)
	root.Args = cobra.NoArgs
	root.RunE = run.welcome
	explainArgs(root)
	root.SetFlagErrorFunc(flagError)
	groupCommands(root, g)
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

// welcome is what bare `multiversectl` does: the help, and, on a machine with
// nothing installed, the two ways to begin.
func (r runner) welcome(cmd *cobra.Command, _ []string) error {
	if err := cmd.Help(); err != nil {
		return err
	}
	bare, err := r.f.Bare(*r.g, r.out)
	if err != nil {
		return nil
	}
	if list, err := bare.Workspaces(); err == nil && len(list) == 0 {
		r.out.Plain("")
		r.out.Plain("%s", r.out.Bold("Get started:"))
		r.out.Plain("  `multiversectl catalog install <owner/repo>`   one project, from a repository or a path")
		r.out.Plain("  `multiversectl init`                   your own index of projects")
	}
	return nil
}
