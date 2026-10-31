package cli

import (
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/app"
)

func TestTheRemovalCommandsComeInASafeOrder(t *testing.T) {
	items := []app.Item{
		{Kind: "program", Path: "/bin/multiversectl", Remove: "rm /bin/multiversectl"},
		{Kind: "state", Path: "/home/me/.config/multiversectl/config.yaml", Remove: "rm -rf '/home/me/.config/multiversectl/config.yaml'"},
		{Kind: "state", Path: "/home/me/.config/multiversectl/catalogs", Remove: "rm -rf '/home/me/.config/multiversectl/catalogs'"},
		{Kind: "clone", Path: "/w/api", Remove: "rm -rf '/w/api'", Risk: true, Note: "2 commits on no remote"},
		{Kind: "universe", Path: "/w/universes/3", Remove: "multiversectl -p acme universe rm 3"},
		{Kind: "server", Remove: "multiversectl server down"},
		{Kind: "branch", Remove: "git -C '/w/api' branch -D u3"},
	}
	var cmds []string
	for _, s := range removalPlan(items) {
		if !s.comment {
			cmds = append(cmds, s.text)
		}
	}
	want := []string{
		"multiversectl server down",
		"multiversectl -p acme universe rm 3",
		"git -C '/w/api' branch -D u3",
		"rm -rf '/w/api'",
		"rm -rf '/home/me/.config/multiversectl'", // every file of the configuration is one command
		"rm /bin/multiversectl",
	}
	if strings.Join(cmds, "\n") != strings.Join(want, "\n") {
		t.Errorf("order:\n%s\nwant:\n%s", strings.Join(cmds, "\n"), strings.Join(want, "\n"))
	}
	var warned bool
	for _, s := range removalPlan(items) {
		warned = warned || (s.comment && strings.Contains(s.text, "loses work: 2 commits on no remote"))
	}
	if !warned {
		t.Error("a command that loses work says so just before it")
	}
}

func TestSizesAreReadable(t *testing.T) {
	for n, want := range map[int64]string{0: "0 B", 1023: "1023 B", 1024: "1.0 KiB", 3 << 30: "3.0 GiB", 15<<30 + 512<<20: "15.5 GiB"} {
		if got := humanBytes(n); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", n, got, want)
		}
	}
}
