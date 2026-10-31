package cli

import (
	"testing"
)

func TestRepositoriesAreShownByPlatform(t *testing.T) {
	t.Setenv("HOME", "/home/me")
	for in, want := range map[string]string{
		"https://github.com/acme/api.git":  "github:acme/api",
		"git@github.com:acme/api.git":      "github:acme/api",
		"https://gitlab.com/acme/team/api": "gitlab:acme/team/api",
		"file:///home/me/code/proj":        "file:~/code/proj",
		"ssh://git@git.example.test/a/b":   "git:git.example.test/a/b",
		"":                                 "",
	} {
		if got := shortRepoName(in); got != want {
			t.Errorf("shortRepoName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStatusAndCatalogAreCommandsWithTheirGroupsAndAliases(t *testing.T) {
	root, _ := New(Factory{})
	for _, path := range [][]string{{"status"}, {"catalog", "list"}, {"catalog", "new"}, {"catalog", "add"}, {"catalog", "ls"}, {"u", "checkout"}, {"u", "co"}} {
		c, _, err := root.Find(path)
		if err != nil || c == root {
			t.Errorf("%v: not found (%v)", path, err)
		}
	}
}

func TestTheServerCanBeRestarted(t *testing.T) {
	root, _ := New(Factory{})
	for _, name := range []string{"up", "down", "restart", "status"} {
		if c, _, err := root.Find([]string{"server", name}); err != nil || c.Name() != name {
			t.Errorf("server %s: %v", name, err)
		}
	}
}
