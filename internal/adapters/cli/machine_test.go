package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/app"
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

// doctor lists the machine's checks, with one line per command that fixes
// something however many checks it fixes, and fails when a check does.
func TestDoctorSaysEachFixOnce(t *testing.T) {
	var buf bytes.Buffer
	run := runner{g: &Globals{}, out: NewOutput(&buf, false)}
	ms := doctorReport{
		Version: "1.2.3",
		Checks: []checkLine{
			{Name: "docker", Status: "ok", Detail: "reachable; compose 5.1.3"},
			{Name: "glab", Status: "idle", Detail: "not installed; only needed for merge requests on GitLab"},
			{Name: "gh", Status: "warn", Detail: "2.6, not signed in", Fix: "gh auth login"},
			{Name: "port 80", Status: "fail", Detail: "taken by another program", Fix: "sudo lsof -i :80"},
			{Name: "port 443", Status: "fail", Detail: "taken by another program", Fix: "sudo lsof -i :80"},
		},
	}
	run.printChecks(ms)
	out := buf.String()
	for _, want := range []string{"This machine", "✓ docker", "○ glab", "! gh", "✗ port 80", "→ gh auth login", "2 failures, 1 warning"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if n := strings.Count(out, "→ sudo lsof -i :80"); n != 1 {
		t.Errorf("a fix for two checks is said %d times, want once:\n%s", n, out)
	}
	if !ms.failed() {
		t.Error("a failed check fails doctor")
	}
	ms.Checks = ms.Checks[:3]
	if ms.failed() {
		t.Error("warnings alone do not fail doctor")
	}
}

// universe doctor says the multiverse's checks under its name, and the
// universe's under that, and none of the machine's.
func TestUniverseDoctorKeepsTheMachineOut(t *testing.T) {
	report := app.Report{Checks: []app.Check{
		{Name: "runtime", Status: app.StatusOK, Scope: app.ScopeMachine},
		{Name: "clone api", Status: app.StatusWarn, Scope: app.ScopeMultiverse, Fix: "multiversectl catalog sync"},
		{Name: "app api", Status: app.StatusIdle, Detail: "not running", Scope: app.ScopeUniverse},
	}}
	run := runner{g: &Globals{}, out: NewOutput(&bytes.Buffer{}, false)}
	ms := run.multiverseChecks(&app.Service{Multiverse: "/r/acme"}, report, "3")
	if len(ms.Checks) != 0 || len(ms.Multiverses) != 1 || len(ms.Multiverses[0].Checks) != 1 || ms.Universe == nil || len(ms.Universe.Checks) != 1 {
		t.Errorf("report = %+v", ms)
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
		"1          running  4/4", "fix/login  created", "the redirect", "no universes yet", "universe doctor"} {
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
