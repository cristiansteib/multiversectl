package app

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type machineSystem struct {
	portSystem
	have map[string]bool
}

func (m machineSystem) Which(name string) (string, bool) { return "/usr/bin/" + name, m.have[name] }

// machineCommands answers --version for every tool, and auth status as signedIn says.
type machineCommands struct{ signedIn map[string]bool }

func (c machineCommands) RunHost(_ context.Context, h HostCommand) (string, error) {
	switch {
	case len(h.Args) > 1 && h.Args[1] == "auth":
		if !c.signedIn[h.Args[0]] {
			return "", errors.New("not logged in")
		}
		return "", nil
	case h.Args[0] == "git":
		return "git version 2.47.0\n", nil
	case h.Args[0] == "gh":
		return "gh version 2.60.1 (2024-10-01)\n", nil
	}
	return "github.com/docker/buildx v0.17.1 abc\n", nil
}

// The machine's checks: git is needed, gh and glab only for pull and merge
// requests, and with the proxy down every port an installed multiverse needs
// it to take is asked for, naming who needs it.
func TestTheMachineSaysWhatIsMissingAndWhichPortIsTaken(t *testing.T) {
	s := &Service{
		Runtime:  preflightRuntime{},
		System:   machineSystem{portSystem: portSystem{taken: map[int]bool{8001: true}}, have: map[string]bool{"git": true, "gh": true}},
		Commands: machineCommands{signedIn: map[string]bool{}},
		Proxy:    &portProxy{},
		Out:      quietOut{},
	}
	checks := checksByName(s.MachineChecks(context.Background(), MachineInput{
		Ports: map[int][]string{80: {"web of acme"}, 8001: {"devapi of acme"}}, Hosts: map[string]bool{"github": true},
	}))
	for name, want := range map[string]Status{
		"git": StatusOK, "gh": StatusWarn, "glab": StatusIdle, "proxy": StatusIdle, "port 80": StatusOK, "port 8001": StatusFail,
	} {
		if got := checks[name].Status; got != want {
			t.Errorf("%s: %s (%s), want %s", name, got, checks[name].Detail, want)
		}
	}
	if checks["git"].Detail != "2.47.0" || !strings.HasPrefix(checks["gh"].Detail, "2.60.1") || checks["gh"].Fix != "gh auth login" {
		t.Errorf("git = %+v, gh = %+v", checks["git"], checks["gh"])
	}
	if !strings.Contains(checks["port 8001"].Detail, "devapi of acme") {
		t.Errorf("a taken port should say who needs it: %+v", checks["port 8001"])
	}
	for _, c := range checks {
		if c.Scope != ScopeMachine {
			t.Errorf("%s is about the machine, not %q", c.Name, c.Scope)
		}
	}

	// A git that is missing fails: nothing works without it.
	s.System = machineSystem{portSystem: portSystem{}, have: map[string]bool{}}
	if got := checksByName(s.MachineChecks(context.Background(), MachineInput{}))["git"].Status; got != StatusFail {
		t.Errorf("a missing git is %s, want fail", got)
	}
}
