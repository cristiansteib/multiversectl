package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

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
	JSON      bool
}

// Factory builds services; main owns it so the CLI never names an adapter.
type Factory struct {
	Version, Commit, Date string

	// Project resolves root and manifest from flags, env, config and cwd.
	Workspace func(g Globals, out app.Output) (*app.Service, error)
	// Bare needs no manifest: install, status, update and the project list.
	Bare func(g Globals, out app.Output) (*app.Service, error)
	// At builds a service for an explicit root and manifest file.
	At func(g Globals, out app.Output, root, manifestPath string) (*app.Service, error)
	// Catalogs builds the catalog service, which never needs a project.
	Catalogs func(g Globals, out app.Output) (*app.Catalogs, error)
	// Prompter asks questions on the terminal.
	Prompter func() app.Prompter
	// Progress shows a spinner for a slow step; nil means none.
	Progress func() app.Progress
	// Server builds the port that starts and stops the web view process.
	Server func() (app.ServerProcess, error)
	// Completions builds the use case that installs and refreshes completions.
	Completions func(out app.Output) *app.Completions
	// Skills builds the use case that installs the agent skill.
	Skills func(out app.Output) *app.Skills
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
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			*out = *NewOutput(os.Stdout, ColorEnabled(os.Stdout, g.NoColor))
			run.refreshCompletions(root, cmd)
			run.refreshSkills(cmd)
			return applyJSON(cmd, out)
		},
	}
	flags := root.PersistentFlags()
	flags.StringVarP(&g.Manifest, "manifest", "f", "", "path to the project manifest")
	flags.StringVarP(&g.Workspace, "project", "p", "", "project to act on (default: the one chosen with `project use`)")
	// The name this flag had before a project was called a project.
	flags.StringVarP(&g.Workspace, "workspace", "w", "", "")
	_ = flags.MarkHidden("workspace")
	_ = flags.MarkDeprecated("workspace", "use --project (-p)")
	flags.StringVar(&g.Root, "root", "", "project root (default $MULTIVERSECTL_ROOT, the manifest's spec.root, config, or the manifest's directory)")
	flags.BoolVar(&g.NoColor, "no-color", false, "disable colour output")
	flags.BoolVarP(&g.Verbose, "verbose", "v", false, "show the commands being run")
	flags.BoolVar(&g.JSON, jsonFlag, false, "machine-readable output: -o json on a listing, and --help as a document (for agents)")

	run = runner{g: g, out: out, f: f}
	root.CompletionOptions.DisableDefaultCmd = true
	root.AddCommand(
		run.startCmd(), run.allTreeCmd(root), run.universeCmd(), run.machineStatusCmd(), run.serverCmd(), run.catalogCmd(), run.projectCmd(), run.initCmd(), run.resetCmd(), run.cleanupCmd(), run.completionCmd(root), run.agentsCmd(),
		run.selfUpdateCmd(), run.versionCmd(),
	)
	_ = root.RegisterFlagCompletionFunc("project", run.completeWorkspaces)
	root.Args = cobra.NoArgs
	root.RunE = run.welcome
	explainArgs(root)
	root.SetFlagErrorFunc(run.flagError)
	groupCommands(root, g)
	jsonHelp(root, nil)
	return root, g
}

type runner struct {
	g   *Globals
	out *Output
	f   Factory
	// quietHints leaves out the "next: …" lines a command prints to say what
	// to do after it. `start` does those things itself, in the same breath.
	quietHints bool
}

// withService runs fn against the resolved project.
func (r runner) withService(fn func(ctx context.Context, s *app.Service) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		s, err := r.f.Workspace(*r.g, r.out)
		if err != nil {
			return err
		}
		// News about the project's manifest, on the way in: the commands that
		// act on a project are where someone would otherwise never look.
		if msg := s.ManifestNotice(cmd.Context()); msg != "" {
			if o := cmd.Flags().Lookup("output"); o != nil && strings.Contains(o.Value.String(), "json") {
				// stdout is a document: the notice must not break it.
				fmt.Fprintln(os.Stderr, "! "+Markup(msg, false))
			} else {
				r.out.Warn("%s", msg)
			}
		}
		return fn(cmd.Context(), s)
	}
}

// welcome is what bare `multiversectl` does: the help, and, on a machine with
// nothing installed, the two ways to begin.
func (r runner) welcome(cmd *cobra.Command, _ []string) error {
	if wantsJSON(cmd) { // an agent running the bare command wants the index
		return cmd.Help()
	}
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
		r.out.Plain("  `multiversectl start <owner/repo>`   install it, make a universe, run it")
		r.out.Plain("  `multiversectl init`                 your own index of projects")
	}
	return nil
}
