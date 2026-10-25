package cli

import "testing"

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
