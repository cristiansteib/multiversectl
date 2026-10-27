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
	Manifest   string
	Root       string
	Multiverse string
	NoColor    bool
	Verbose    bool
	JSON       bool
}

// Factory builds services; main owns it so the CLI never names an adapter.
type Factory struct {
	Version, Commit, Date string

	// Multiverse resolves root and manifest from flags, env, config and cwd.
	Multiverse func(g Globals, out app.Output) (*app.Service, error)
	// Bare needs no manifest: install, status, update and the multiverse list.
	Bare func(g Globals, out app.Output) (*app.Service, error)
	// At builds a service for an explicit root and manifest file.
	At func(g Globals, out app.Output, root, manifestPath string) (*app.Service, error)
	// Catalogs builds the catalog service, which never needs a multiverse.
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
	// Detach runs this program again with args, on its own and unwaited for,
	// so that it outlives the command that started it.
	Detach func(args ...string)
}

// New returns the root command and the globals its flags fill in.
func New(f Factory) (*cobra.Command, *Globals) {
	g := &Globals{}
	out := &Output{}
	// The pre-run hook needs the command it hangs on and the runner built after it.
	var run runner
	var root *cobra.Command
	// What the pre-run hook started and the post-run hook finishes: the look for
	// a newer release runs beside the command.
	var endUpdateCheck = func() {}
	root = &cobra.Command{
		Use:           "multiversectl",
		Short:         "Run numbered, isolated copies of a multi-repo stack side by side",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			*out = *NewOutput(os.Stdout, ColorEnabled(os.Stdout, g.NoColor))
			run.refreshCompletions(root, cmd)
			run.refreshSkills(cmd)
			endUpdateCheck = run.startUpdateCheck(cmd)
			return applyJSON(cmd, out)
		},
		// Only a command that worked: after a failure the error is the news.
		PersistentPostRun: func(*cobra.Command, []string) { endUpdateCheck() },
	}
	flags := root.PersistentFlags()
	flags.StringVarP(&g.Manifest, "manifest", "f", "", "path to the multiverse manifest")
	flags.StringVarP(&g.Multiverse, "multiverse", "m", "", "multiverse to act on (default: the one whose folder you are in, else the only one installed)")
	flags.StringVar(&g.Root, "root", "", "multiverse root (default $MULTIVERSECTL_ROOT, the manifest's spec.root, config, or the manifest's directory)")
	flags.BoolVar(&g.NoColor, "no-color", false, "disable colour output")
	flags.BoolVarP(&g.Verbose, "verbose", "v", false, "show the commands being run")
	flags.BoolVar(&g.JSON, jsonFlag, false, "machine-readable output: -o json on a listing, and --help as a document (for agents)")

	run = runner{g: g, out: out, f: f}
	root.CompletionOptions.DisableDefaultCmd = true
	root.AddCommand(
		run.startCmd(), run.helpTreeCmd(root), run.universeCmd(), run.machineStatusCmd(), run.serverCmd(), run.catalogCmd(), run.initCmd(), run.systemCmd(), run.completionCmd(root), run.agentsCmd(),
		run.selfUpdateCmd(), run.versionCmd(), run.updateLookCmd(),
	)
	_ = root.RegisterFlagCompletionFunc("multiverse", run.completeMultiverses)
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

// withService runs fn against the resolved multiverse.
func (r runner) withService(fn func(ctx context.Context, s *app.Service) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		s, err := r.f.Multiverse(*r.g, r.out)
		if err != nil {
			return err
		}
		// News about the multiverse's manifest, on the way in: the commands that
		// act on a multiverse are where someone would otherwise never look.
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
	if list, err := bare.Multiverses(); err == nil && len(list) == 0 {
		r.out.Plain("")
		r.out.Plain("%s", r.out.Bold("Nothing is installed yet:"))
		r.out.Plain("  `multiversectl start <owner/repo>`   install it, make a universe, run it")
		r.out.Plain("  `multiversectl init`                 your own index of multiverses")
	}
	return nil
}
