package cli

import (
	"bytes"
	"strings"
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
	for _, path := range [][]string{{"status"}, {"catalog", "new"}, {"catalog", "add"}, {"catalog", "ls"}, {"u", "new"}, {"u", "about"}} {
		c, _, err := root.Find(path)
		if err != nil || c == root || c.Name() != path[len(path)-1] {
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

// doctor says the machine's checks once, then each multiverse's under its
// name, with one line per command that fixes something however many checks
// it fixes, and fails when a check does.
func TestDoctorGroupsTheChecksAndSaysEachFixOnce(t *testing.T) {
	var buf bytes.Buffer
	run := runner{g: &Globals{}, out: NewOutput(&buf, false)}
	ms := doctorReport{
		Version: "1.2.3",
		Checks: []checkLine{
			{Name: "runtime", Status: "ok", Detail: "5.1.3"},
			{Name: "web view", Status: statusIdle, Detail: "not running", Fix: "multiversectl server up"},
		},
		Multiverses: []multiverseLine{{Name: "acme", From: "github:acme/multiverse", Universes: 2, Running: 1, Checks: []checkLine{
			{Name: "manifest", Status: "ok", Detail: "up to date"},
			{Name: "clone api", Status: "warn", Detail: "2 commits behind", Fix: "multiversectl catalog sync"},
			{Name: "clone web", Status: "warn", Detail: "1 commits behind", Fix: "multiversectl catalog sync"},
		}}},
	}
	run.printChecks(ms)
	out := buf.String()
	for _, want := range []string{"This machine", "✓ runtime", "○ web view", "acme  github:acme/multiverse · 2 universes, 1 running", "! clone api", "2 warnings"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if n := strings.Count(out, "→ multiversectl catalog sync"); n != 1 {
		t.Errorf("the fix for both clones is said %d times, want once:\n%s", n, out)
	}
	if strings.Contains(out, "→ multiversectl server up") {
		t.Errorf("an idle web view is not something to fix:\n%s", out)
	}
	if ms.failed() {
		t.Error("warnings alone do not fail doctor")
	}
	ms.Multiverses[0].Checks[1].Status = "fail"
	if !ms.failed() {
		t.Error("a failed check fails doctor")
	}
}

// status says how things are, one line per universe under its multiverse, and
// leaves judging them to doctor.
func TestStatusSaysWhatRunsAndJudgesNothing(t *testing.T) {
	var buf bytes.Buffer
	run := runner{g: &Globals{}, out: NewOutput(&buf, false)}
	run.printStatus(statusReport{
		Version: "1.2.3",
		Proxy:   proxyLine{Running: true, Entrypoints: map[string]int{"web": 80}},
		Multiverses: []multiverseStatus{
			{Name: "acme", From: "github:acme/multiverse", Notes: []string{"manifest behind 2"}, Universes: []universeStatus{
				{Name: "1", State: "running", Up: "4/4", Branch: "u1"},
				{Name: "fix/login", State: "created", Up: "0/4", Branch: "fix/login", About: "the redirect"},
			}},
			{Name: "empty", From: "github:acme/empty", Universes: []universeStatus{}},
		},
	})
	out := buf.String()
	for _, want := range []string{"Proxy      running · web:80", "Web view   not running", "acme  github:acme/multiverse · manifest behind 2",
		"1          running  4/4", "fix/login  created", "the redirect", "no universes yet", "multiversectl doctor"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	for _, judged := range []string{"✓", "!", "✗", "warning"} {
		if strings.Contains(out, judged) {
			t.Errorf("status judges nothing, but says %q:\n%s", judged, out)
		}
	}
	if got := oneBranch(map[string]string{"api": "u1", "web": "u1"}); got != "u1" {
		t.Errorf("oneBranch = %q, want u1", got)
	}
}
