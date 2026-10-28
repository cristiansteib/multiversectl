package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/cristiansteib/multiversectl/internal/app"
	"github.com/cristiansteib/multiversectl/internal/domain"
)

// catalogManageCmd is the catalog on one screen, for a person at a terminal: the
// tree to move through with the arrow keys, and install, uninstall, leave out and
// bring back as keys on it. What is changed is staged and reviewed before it is
// made, and each change is shown as the command that does the same.
func (r runner) catalogManageCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "manage",
		Short: "Manage the catalog on one screen: install, uninstall, leave out or bring back, with the keyboard",
		Long: "The catalog as a tree you move through with the arrow keys:\n\n" +
			"  space      install what is not installed, or uninstall what is (on a catalog,\n" +
			"             everything under it)\n" +
			"  e          leave a node out on this machine (it is not read or offered), or\n" +
			"             bring it back\n" +
			"  r          remove the entry from the catalog (a shared repository)\n" +
			"  a, u       add an entry, or update what is installed (guided)\n" +
			"  /          filter       ←  →  fold        ?  every key        q  quit\n\n" +
			"Installing, uninstalling and leaving out are staged: you toggle as many as you\n" +
			"like and nothing is done until you press enter and review the list, which shows\n" +
			"the command for each (`start <path> --install-only`, `system forget <name>`,\n" +
			"`catalog ignore <path>`), so the same can be scripted. It needs a terminal.",
		Example: "  multiversectl catalog manage",
		Args:    cobra.NoArgs,
	}
	cmd.RunE = func(cmd *cobra.Command, _ []string) error { return r.manage(cmd) }
	return cmd
}

func (r runner) manage(cmd *cobra.Command) error {
	if !term.IsTerminal(os.Stdin.Fd()) || !term.IsTerminal(os.Stdout.Fd()) {
		return errors.New("catalog manage is a screen and needs a terminal; the same things one at a time: `catalog list`, `start <path> --install-only`, `system forget <name>`, `catalog ignore <path>`, `catalog add`, `catalog rm <path>`, `catalog update`")
	}
	c, err := r.catalogs(false)
	if err != nil {
		return err
	}
	env := manageRunner{r: r, cmd: cmd, c: c}
	p := tea.NewProgram(newManageModel(env), tea.WithAltScreen(), tea.WithContext(cmd.Context()))
	if _, err := p.Run(); err != nil && !errors.Is(err, tea.ErrProgramKilled) && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

// manageRunner is the screen's hands: it reads the catalog and makes the changes
// with the same use cases the commands do.
type manageRunner struct {
	r   runner
	cmd *cobra.Command
	c   *app.Catalogs
}

var _ manageEnv = manageRunner{}

func (e manageRunner) Load() (domain.Node, map[string]string, error) {
	tree, err := e.c.TreeCached(e.cmd.Context())
	if err != nil {
		return domain.Node{}, nil, err
	}
	installed, err := e.c.InstalledIn(tree)
	return tree, installed, err
}

func (e manageRunner) Apply(plan managePlan) tea.ExecCommand {
	return &execFunc{run: func() error {
		ctx := e.cmd.Context()
		var failures []string
		fail := func(what string, err error) {
			failures = append(failures, what)
			e.r.out.Warn("%s: %v", what, err)
		}
		paths := func(items []planItem) []string {
			out := make([]string, len(items))
			for i, it := range items {
				out[i] = it.Path
			}
			return out
		}
		step := func(command string) { e.r.out.Info("$ multiversectl %s", command) }

		if len(plan.Enable) > 0 {
			step("catalog unignore " + strings.Join(paths(plan.Enable), " "))
			if err := e.c.Ignore(ctx, paths(plan.Enable), false); err != nil {
				fail("bring back", err)
			}
		}
		base := firstNonEmpty(os.Getenv("MULTIVERSECTL_ROOT"), "~/multiverse")
		for _, it := range plan.Install {
			step("start " + it.Path + " --install-only")
			if _, err := e.r.install(e.cmd, []string{it.Path}, app.InstallOptions{Base: base, Root: e.r.g.Root}, false, false); err != nil {
				fail("install "+it.Name, err)
			}
		}
		if len(plan.Uninstall) > 0 {
			bare, err := e.r.f.Bare(*e.r.g, e.r.out)
			if err != nil {
				fail("uninstall", err)
			} else {
				for _, it := range plan.Uninstall {
					step("system forget " + it.Name)
					if err := bare.Forget(it.Name); err != nil {
						fail("uninstall "+it.Name, err)
					}
				}
			}
		}
		if len(plan.Ignore) > 0 {
			step("catalog ignore " + strings.Join(paths(plan.Ignore), " "))
			if err := e.c.Ignore(ctx, paths(plan.Ignore), true); err != nil {
				fail("leave out", err)
			}
		}
		if len(failures) > 0 {
			return fmt.Errorf("%d of %d changes failed: %s", len(failures), plan.count(), strings.Join(failures, "; "))
		}
		e.r.out.OK("%d change(s) made", plan.count())
		return nil
	}}
}

func (e manageRunner) RemoveEntry(path string) tea.ExecCommand {
	return &execFunc{run: func() error {
		e.r.out.Info("$ multiversectl catalog rm %s", path)
		return e.c.Remove(e.cmd.Context(), path)
	}}
}

func (e manageRunner) Interactive(which string) tea.ExecCommand {
	return &execFunc{run: func() error {
		switch which {
		case "add":
			return e.r.delegate(e.cmd, e.r.catalogAddCmd())
		case "update":
			return e.r.delegate(e.cmd, e.r.updateCmd())
		}
		return fmt.Errorf("unknown action %q", which)
	}}
}

// delegate runs a command of the catalog as it would be run with no arguments:
// asking for what it needs, as it does by itself.
func (r runner) delegate(cmd *cobra.Command, sub *cobra.Command) error {
	sub.SetContext(cmd.Context())
	return sub.RunE(sub, nil)
}

// execFunc is a command the screen hands the terminal to: it runs as it would
// from the shell, prints as it would, and waits to be told to go back, so what it
// said can be read.
type execFunc struct {
	run func() error
	in  io.Reader
	out io.Writer
}

func (x *execFunc) SetStdin(r io.Reader)  { x.in = r }
func (x *execFunc) SetStdout(w io.Writer) { x.out = w }
func (x *execFunc) SetStderr(io.Writer)   {}

func (x *execFunc) Run() error {
	err := x.run()
	if x.out != nil && x.in != nil {
		if err != nil {
			fmt.Fprintf(x.out, "\n✗ %v\n", err)
		}
		fmt.Fprint(x.out, "\nPress enter to go back to the catalog. ")
		_, _ = bufio.NewReader(x.in).ReadString('\n')
	}
	return err
}
