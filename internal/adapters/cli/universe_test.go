package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/app"
)

// noTerminal answers every question as a script would: there is nobody to ask.
type noTerminal struct{}

func (noTerminal) Select(string, []app.Option, int) (int, error)   { return 0, app.ErrNotInteractive }
func (noTerminal) MultiSelect(string, []app.Option) ([]int, error) { return nil, app.ErrNotInteractive }
func (noTerminal) Confirm(string, bool) (bool, error)              { return false, app.ErrNotInteractive }
func (noTerminal) Input(string, string) (string, error)            { return "", app.ErrNotInteractive }

func TestUniverseCommandsLiveUnderUniverse(t *testing.T) {
	root, _ := New(Factory{})
	for _, name := range []string{"new", "checkout", "up", "down", "rm", "ls", "status", "urls", "env", "run", "exec", "doctor", "proxy", "prune"} {
		c, _, err := root.Find([]string{"u", name})
		if err != nil || c.Name() != name {
			t.Errorf("u %s: not found under universe (%v)", name, err)
		}
		// status is also a top-level command, but a different one: the machine's
		// overview, not a universe listing.
		if top, _, _ := root.Find([]string{name}); name != "status" && top != root {
			t.Errorf("%s is still a top-level command", name)
		}
	}
	if c, _, _ := root.Find([]string{"universe"}); c == root || c.Name() != "universe" {
		t.Error("universe command missing")
	}
}

func TestEachCommandLivesWhereItsSubjectDoes(t *testing.T) {
	root, _ := New(Factory{})
	// The tree of catalogs is edited under catalog; what is installed here, and
	// how it is kept, is a project's.
	for parent, names := range map[string][]string{
		"catalog": {"add", "create", "rm"},
		"project": {"list", "init", "add", "install", "uninstall", "use", "status", "update"},
	} {
		for _, name := range names {
			c, _, err := root.Find([]string{parent, name})
			if err != nil || c.Name() != name {
				t.Errorf("%s %s: not found (%v)", parent, name, err)
			}
			// status and init are also top-level commands, but different ones: the
			// machine's overview and its root, not a project's drift or start.
			if top, _, _ := root.Find([]string{name}); name != "status" && name != "init" && top != root {
				t.Errorf("%s is still a top-level command", name)
			}
		}
	}
	for _, gone := range []string{"install", "use", "update"} {
		if c, _, _ := root.Find([]string{"catalog", gone}); c.Name() == gone {
			t.Errorf("catalog %s should be project %s", gone, gone)
		}
	}
}

// The list of commands is where someone looks for a shorter name.
func TestHelpListsAliases(t *testing.T) {
	root, _ := New(Factory{})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "universe, u ") {
		t.Errorf("help does not show the alias of universe:\n%s", out.String())
	}
}

// Cobra's own wording ("accepts 1 arg(s), received 0") says neither what is
// missing nor what to type.
func TestArgumentErrorsSayWhatToDo(t *testing.T) {
	cases := []struct {
		args []string
		want []string
	}{
		{[]string{"catalog", "create"}, []string{"needs <name>", "usage:", "example:"}},
		{[]string{"universe", "ls", "extra"}, []string{"takes no arguments", "extra"}},
		{[]string{"u", "nwe"}, []string{"has no command nwe", "did you mean new", "commands:"}},
	}
	for _, c := range cases {
		root, _ := New(Factory{Prompter: func() app.Prompter { return noTerminal{} }})
		root.SetOut(&bytes.Buffer{})
		root.SetErr(&bytes.Buffer{})
		root.SetArgs(c.args)
		err := root.Execute()
		if err == nil {
			t.Errorf("%v: no error", c.args)
			continue
		}
		shown := Markup(err.Error(), false) // what a person reads without colour
		for _, w := range c.want {
			if !strings.Contains(shown, w) {
				t.Errorf("%v: %q lacks %q", c.args, shown, w)
			}
		}
	}
}

// -w/--workspace is what the flag was called before; scripts that use it keep
// working, and it says what to use instead.
func TestTheOldWorkspaceFlagStillSelectsAProject(t *testing.T) {
	for _, args := range [][]string{{"-p", "acme"}, {"--project", "acme"}, {"-w", "acme"}, {"--workspace", "acme"}} {
		root, g := New(Factory{})
		if err := root.PersistentFlags().Parse(args); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if g.Workspace != "acme" {
			t.Errorf("%v selected %q", args, g.Workspace)
		}
	}
	root, _ := New(Factory{})
	if f := root.PersistentFlags().Lookup("workspace"); f == nil || !f.Hidden || f.Deprecated == "" {
		t.Errorf("the old flag should be hidden and say what replaced it: %+v", f)
	}
}
