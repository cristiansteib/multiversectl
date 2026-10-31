package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// MachineInput is what the machine's checks need to know of what is installed.
type MachineInput struct {
	// Ports are the host ports the shared proxy listens on, each with the
	// entrypoints that ask for it ("web of acme"): every installed
	// multiverse's, since any of them may be the one that starts it.
	Ports map[int][]string
	// Path is where multiverses are written, for the disk check.
	Path string
	// Hosts are the git hosts the installed multiverses' repositories are on,
	// by platform ("github", "gitlab"): a command line that is signed out is
	// only a warning for a host something is on.
	Hosts map[string]bool
}

// MachineChecks is what this machine needs for any multiverse to run, apart
// from any one of them: docker with compose and buildx and room for networks,
// git, the GitHub and GitLab command lines, disk space, and the ports the shared
// proxy listens on.
func (s *Service) MachineChecks(ctx context.Context, in MachineInput) []Check {
	var out []Check
	addFix := func(name string, status Status, fix, format string, a ...any) {
		out = append(out, Check{Name: name, Status: status, Detail: fmt.Sprintf(format, a...), Scope: ScopeMachine, Fix: fix})
	}
	add := func(name string, status Status, format string, a ...any) { addFix(name, status, "", format, a...) }

	s.preflightDocker(ctx, add)
	s.checkTool(ctx, addFix, "git", "worktrees and clones", true, true, "", []string{"git", "--version"}, 2)
	s.checkTool(ctx, addFix, "gh", "pull requests on GitHub", false, in.Hosts["github"], "gh auth login", []string{"gh", "--version"}, 2)
	s.checkTool(ctx, addFix, "glab", "merge requests on GitLab", false, in.Hosts["gitlab"], "glab auth login", []string{"glab", "--version"}, 1)
	s.preflightDisk(ctx, add, in.Path)
	s.checkProxyFree(ctx, add, in.Ports)
	return out
}

// checkTool says whether a program is there and, for a git host's command line,
// signed in. A required one that is missing fails; an optional one only means
// what it is for is not shown, so it is said and not counted.
func (s *Service) checkTool(ctx context.Context, addFix fixCheck, name, forWhat string, required, used bool, signIn string, version []string, word int) {
	if _, found := s.System.Which(name); !found {
		if required {
			addFix(name, StatusFail, "", "not installed, and it is needed for %s", forWhat)
			return
		}
		addFix(name, StatusIdle, "", "not installed; only needed for %s", forWhat)
		return
	}
	v := "installed"
	if s.Commands != nil {
		if o, err := s.Commands.RunHost(ctx, HostCommand{Args: version}); err == nil {
			if fields := strings.Fields(firstLine(o)); len(fields) > word {
				v = fields[word]
			}
		}
	}
	if signIn == "" || s.Commands == nil {
		addFix(name, StatusOK, "", "%s", v)
		return
	}
	if _, err := s.Commands.RunHost(ctx, HostCommand{Args: []string{name, "auth", "status"}}); err != nil {
		if !used {
			addFix(name, StatusIdle, signIn, "%s, not signed in; no installed multiverse needs it", v)
			return
		}
		addFix(name, StatusWarn, signIn, "%s, not signed in, so %s cannot be shown", v, forWhat)
		return
	}
	addFix(name, StatusOK, "", "%s, signed in", v)
}

// checkProxyFree says where the shared proxy listens when it runs, and when it
// does not, whether each port it would take is free: a port another program
// holds stops the first start of every multiverse.
func (s *Service) checkProxyFree(ctx context.Context, add addCheck, ports map[int][]string) {
	if status, err := s.Proxy.Status(ctx); err != nil {
		add("proxy", StatusFail, "%v", err)
		return
	} else if status.Running {
		where := status.Bind
		if where == "" {
			where = "0.0.0.0"
		}
		reach := "reachable from this network"
		if where == "127.0.0.1" {
			reach = "this machine only"
		}
		names := make([]string, 0, len(status.Entrypoints))
		for n, p := range status.Entrypoints {
			names = append(names, fmt.Sprintf("%s:%d", n, p))
		}
		sort.Strings(names)
		add("proxy", StatusOK, "running on %s, %s · %s", where, reach, strings.Join(names, ", "))
		return
	}
	add("proxy", StatusIdle, "not running; the first start brings it up")
	numbers := make([]int, 0, len(ports))
	for p := range ports {
		numbers = append(numbers, p)
	}
	sort.Ints(numbers)
	for _, port := range numbers {
		label := fmt.Sprintf("port %d", port)
		who := strings.Join(ports[port], ", ")
		free, err := s.System.PortFree(ctx, port)
		switch {
		case err != nil:
			add(label, StatusWarn, "cannot tell if it is free (%s needs it): %v", who, err)
		case !free:
			add(label, StatusFail, "taken by another program, and the proxy needs it for %s", who)
		default:
			add(label, StatusOK, "free, for %s", who)
		}
	}
}
