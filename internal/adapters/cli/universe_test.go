package cli

import (
	"bytes"
	"strings"
	"testing"
)

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
