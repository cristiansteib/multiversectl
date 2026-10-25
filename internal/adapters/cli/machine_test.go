package cli

import (
	"testing"

	"github.com/cristiansteib/multiversectl/internal/app"
)

func TestRepositoriesAreShownAsOwnerSlashName(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/acme/api.git":   "github.com/acme/api",
		"git@github.com:acme/api.git":       "github.com/acme/api",
		"file:///tmp/uidemo/proj":           "/tmp/uidemo/proj",
		"ssh://git@gitlab.example.test/a/b": "gitlab.example.test/a/b",
		"":                                  "",
	} {
		if got := shortRepoName(in); got != want {
			t.Errorf("shortRepoName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOnlyAReleaseIsToldToUpdate(t *testing.T) {
	for v, want := range map[string]bool{"v1.2.3": true, "1.2.3": true, "dev": false, "": false} {
		if got := looksReleased(v); got != want {
			t.Errorf("looksReleased(%q) = %v", v, got)
		}
	}
}

func TestAProjectsStateIsItsDriftOrUpToDate(t *testing.T) {
	if got := stateOf(app.WorkspaceStatus{}); got != "up to date" {
		t.Errorf("got %q", got)
	}
	if got := stateOf(app.WorkspaceStatus{Notes: []string{"manifest behind 1", "api not cloned"}}); got != "manifest behind 1 · api not cloned" {
		t.Errorf("got %q", got)
	}
}

func TestStatusAndProjectAreCommandsWithTheirGroupsAndAliases(t *testing.T) {
	root, _ := New(Factory{})
	for _, path := range [][]string{{"status"}, {"p", "list"}, {"p", "init"}, {"p", "add"}, {"project", "ls"}, {"u", "checkout"}, {"u", "co"}} {
		c, _, err := root.Find(path)
		if err != nil || c == root {
			t.Errorf("%v: not found (%v)", path, err)
		}
	}
	if c, _, _ := root.Find([]string{"p"}); c.Name() != "project" {
		t.Errorf("p is %q", c.Name())
	}
}
