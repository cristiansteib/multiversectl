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
	for _, name := range []string{"new", "checkout", "up", "down", "rm", "ls", "status", "urls", "env", "run", "exec", "doctor"} {
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
	// Everything about an entry of the catalog — looking at the tree, editing
	// it, and bringing it up to date — is the catalog's. There is no second
	// noun to look under, and starting one is `start`, not a catalog verb.
	for _, name := range []string{"list", "add", "rm", "new", "update"} {
		c, _, err := root.Find([]string{"catalog", name})
		if err != nil || c.Name() != name || c.Hidden {
			t.Errorf("catalog %s: not found as a visible command (%v)", name, err)
		}
		if top, _, _ := root.Find([]string{name}); top != root {
			t.Errorf("%s is a top-level command", name)
		}
	}
	// What acts on a multiverse as a whole is typed where the machine's
	// things are, and what was a catalog verb for it is gone.
	for _, gone := range []string{"create", "install", "uninstall", "use", "status"} {
		if c, _, _ := root.Find([]string{"catalog", gone}); c != nil && c.Name() == gone {
			t.Errorf("catalog %s should not exist", gone)
		}
	}
	for _, path := range [][]string{{"system", "forget"}, {"system", "reset"}, {"status"}, {"start"}} {
		if c, _, err := root.Find(path); err != nil || c.Name() != path[len(path)-1] || c.Hidden {
			t.Errorf("%v: not found as a visible command (%v)", path, err)
		}
	}
	for _, old := range []string{"project", "reset", "cleanup", "p"} {
		if c, _, _ := root.Find([]string{old}); c != nil && c != root {
			t.Errorf("%s is gone, but %q answers", old, c.Name())
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
		{[]string{"catalog", "create"}, []string{"has no command create", "commands:"}},
		{[]string{"universe", "status", "extra"}, []string{"takes no arguments", "extra"}},
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

// The flag is -m/--multiverse, and nothing else names it any more.
func TestTheMultiverseFlagSelectsOne(t *testing.T) {
	for _, args := range [][]string{{"-m", "acme"}, {"--multiverse", "acme"}} {
		root, g := New(Factory{})
		if err := root.PersistentFlags().Parse(args); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if g.Multiverse != "acme" {
			t.Errorf("%v selected %q", args, g.Multiverse)
		}
	}
	for _, args := range [][]string{{"-p", "acme"}, {"--project", "acme"}, {"-w", "acme"}} {
		root, _ := New(Factory{})
		if err := root.PersistentFlags().Parse(args); err == nil {
			t.Errorf("%v should not be a flag any more", args)
		}
	}
}

func TestLsAsksForJSONForScriptsAndAgents(t *testing.T) {
	f, err := parseLsFormat("json")
	if err != nil || !f.json || f.table || f.list {
		t.Errorf("json alone is json only: %+v %v", f, err)
	}
	f, err = parseLsFormat("table,wide")
	if err != nil || f.json || !f.table || !f.wide {
		t.Errorf("%+v %v", f, err)
	}
	if _, err := parseLsFormat("yaml"); err == nil || !strings.Contains(err.Error(), "json") {
		t.Errorf("the error should list json among the choices: %v", err)
	}
}
