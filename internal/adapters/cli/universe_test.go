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
	for _, name := range []string{"new", "up", "down", "rm", "ls", "urls", "env", "run", "exec"} {
		c, _, err := root.Find([]string{"u", name})
		if err != nil || c.Name() != name {
			t.Errorf("u %s: not found under universe (%v)", name, err)
		}
		if top, _, _ := root.Find([]string{name}); top != root {
			t.Errorf("%s is still a top-level command", name)
		}
	}
	if c, _, _ := root.Find([]string{"universe"}); c == root || c.Name() != "universe" {
		t.Error("universe command missing")
	}
}

func TestCatalogCommandsLiveUnderCatalog(t *testing.T) {
	root, _ := New(Factory{})
	for _, name := range []string{"add", "create", "rm", "install", "uninstall", "use", "status", "update"} {
		c, _, err := root.Find([]string{"catalog", name})
		if err != nil || c.Name() != name {
			t.Errorf("catalog %s: not found (%v)", name, err)
		}
		if top, _, _ := root.Find([]string{name}); top != root {
			t.Errorf("%s is still a top-level command", name)
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
		{[]string{"u", "nwe"}, []string{`no command "nwe"`, "did you mean new", "commands:"}},
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
		for _, w := range c.want {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("%v: %q lacks %q", c.args, err, w)
			}
		}
	}
}
