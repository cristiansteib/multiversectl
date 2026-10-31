package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/cristiansteib/multiversectl/internal/app"
	"github.com/cristiansteib/multiversectl/internal/domain"
)

// quietOut drops what a use case would print: status and doctor ask many
// services about many things, and want the answers, not their warnings.
type quietOut struct{}

func (quietOut) Info(string, ...any)        {}
func (quietOut) OK(string, ...any)          {}
func (quietOut) Warn(string, ...any)        {}
func (quietOut) Plain(string, ...any)       {}
func (quietOut) Table([]string, [][]string) {}

// doctorReport is everything a doctor found, so the same answer can be printed
// or handed to a script: the machine's checks for `doctor`, a multiverse's and
// a universe's for `universe doctor`.
type doctorReport struct {
	Version     string           `json:"version"`
	Checks      []checkLine      `json:"checks"`
	Multiverses []multiverseLine `json:"multiverses,omitempty"`
	Universe    *universeCheck   `json:"universe,omitempty"`
}

type multiverseLine struct {
	Name   string      `json:"name"`
	Root   string      `json:"root"`
	Checks []checkLine `json:"checks"`
}

type universeCheck struct {
	Name       string      `json:"name"`
	Multiverse string      `json:"multiverse"`
	Checks     []checkLine `json:"checks"`
}

type proxyLine struct {
	Running     bool           `json:"running"`
	Entrypoints map[string]int `json:"entrypoints,omitempty"`
}

type serverLine struct {
	Running bool   `json:"running"`
	URL     string `json:"url,omitempty"`
	// Outdated is a server that started before the installed program was replaced.
	Outdated bool `json:"outdated,omitempty"`
}

// checkLine is one thing a doctor looked at.
type checkLine struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
	Fix    string `json:"fix,omitempty"`
}

func fromCheck(c app.Check) checkLine {
	return checkLine{Name: c.Name, Status: string(c.Status), Detail: c.Detail, Fix: c.Fix}
}

// doctorCmd checks the machine: what every multiverse needs from it, and
// nothing about any one of them, which is `universe doctor`.
func (r runner) doctorCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check what this machine needs to run any multiverse: docker, git, the git host tools, disk, the proxy's ports",
		Long: "Checks what every multiverse needs from this machine: docker reachable with compose\n" +
			"and buildx, room in Docker's address pools for more networks, git, the GitHub and\n" +
			"GitLab command lines (optional; signed in when there), free disk space, and the\n" +
			"shared proxy — where it listens when it runs, and when it does not, that every port\n" +
			"an installed multiverse needs it to take is free. Anything that is not fine says what\n" +
			"to run. It exits 1 when a check fails. A multiverse and its universes are checked by\n" +
			"`multiversectl universe doctor`.",
		Example: "  multiversectl doctor\n  multiversectl doctor -o json",
		Args:    cobra.NoArgs,
	}
	cmd.Flags().StringVarP(&format, "output", "o", "table", "output: table or json")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if format != "table" && format != "json" {
			return fmt.Errorf("--output must be table or json, not %q", format)
		}
		var ms doctorReport
		err := r.spin(true, "Checking this machine", func() (err error) { ms, err = r.machineChecks(cmd.Context()); return err })
		if err != nil {
			return err
		}
		return r.reportDoctor(ms, format)
	}
	return cmd
}

// universeDoctorCmd checks one multiverse and, when one is named, a universe of
// it: what the machine-wide `doctor` leaves out.
func (r runner) universeDoctorCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "doctor [universe]",
		Short: "Check the multiverse and optionally one universe: its clones, domain, git host, secrets, apps and addresses",
		Long: "Checks the multiverse: that its shared clones are up to date with their remotes,\n" +
			"the proxy it asks for, a proxy it would clash with, that its domain resolves to this\n" +
			"machine and that its git host is signed in. Name a universe and it is checked too:\n" +
			"its secrets, each app, and every address of a running app, asked once. Anything that\n" +
			"is not fine says what to run. It exits 1 when a check fails. What the machine needs\n" +
			"for any multiverse is `multiversectl doctor`.",
		Example: "  multiversectl universe doctor\n  multiversectl universe doctor 3\n  multiversectl universe doctor fix/login -o json",
		Args:    cobra.MaximumNArgs(1),
	}
	cmd.Flags().StringVarP(&format, "output", "o", "table", "output: table or json")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if format != "table" && format != "json" {
			return fmt.Errorf("--output must be table or json, not %q", format)
		}
		return r.withService(func(ctx context.Context, s *app.Service) error {
			universe := ""
			if len(args) == 1 {
				universe = args[0]
			}
			var ms doctorReport
			err := r.spin(true, "Checking", func() error {
				report, err := s.Doctor(ctx, app.DoctorInput{Universe: universe})
				if err != nil {
					return err
				}
				ms = r.multiverseChecks(s, report, universe)
				return nil
			})
			if err != nil {
				return err
			}
			return r.reportDoctor(ms, format)
		})(cmd, args)
	}
	return cmd
}

// reportDoctor prints a report, or writes it for a script, and fails when a
// check did.
func (r runner) reportDoctor(ms doctorReport, format string) error {
	if format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(ms); err != nil {
			return err
		}
	} else {
		r.printChecks(ms)
	}
	if ms.failed() {
		return errChecksFailed
	}
	return nil
}

// machineChecks asks the machine what every installed multiverse needs of it,
// the proxy's ports most of all, which each multiverse's manifest names.
func (r runner) machineChecks(ctx context.Context) (doctorReport, error) {
	ms := doctorReport{Version: r.f.Version, Checks: []checkLine{}}
	bare, err := r.f.Bare(*r.g, quietOut{})
	if err != nil {
		return ms, err
	}
	ports := map[int][]string{}
	hosts := map[string]bool{}
	installed, err := bare.Multiverses()
	if err != nil {
		return ms, err
	}
	for _, info := range installed {
		svc, err := r.f.At(*r.g, quietOut{}, info.Root, info.Manifest)
		if err != nil || svc.Manifest == nil {
			continue
		}
		for entrypoint, port := range svc.Manifest.Proxy.Entrypoints {
			ports[port] = append(ports[port], entrypoint+" of "+info.Name)
		}
		for _, repo := range svc.Manifest.Repos {
			hosts[string(domain.Locate(repo.URL, "").Platform)] = true
		}
	}
	if len(installed) == 0 {
		// Nothing installed: the first start takes the default.
		ports[80] = []string{"web, the default"}
	}
	for _, who := range ports {
		sort.Strings(who)
	}
	for _, c := range bare.MachineChecks(ctx, app.MachineInput{Ports: ports, Path: r.installPath(bare, ""), Hosts: hosts}) {
		ms.Checks = append(ms.Checks, fromCheck(c))
	}
	return ms, nil
}

// multiverseChecks keeps what Doctor found about the multiverse and the
// universe, and leaves the machine's to `doctor`.
func (r runner) multiverseChecks(s *app.Service, report app.Report, universe string) doctorReport {
	ms := doctorReport{Version: r.f.Version, Checks: []checkLine{}}
	name := s.Multiverse
	if s.Manifest != nil {
		name = s.Manifest.Name
	}
	line := multiverseLine{Name: name, Root: s.Multiverse, Checks: []checkLine{}}
	uc := universeCheck{Name: universe, Multiverse: name, Checks: []checkLine{}}
	for _, c := range report.Checks {
		switch c.Scope {
		case app.ScopeMultiverse:
			line.Checks = append(line.Checks, fromCheck(c))
		case app.ScopeUniverse:
			uc.Checks = append(uc.Checks, fromCheck(c))
		}
	}
	ms.Multiverses = []multiverseLine{line}
	if universe != "" {
		ms.Universe = &uc
	}
	return ms
}

// each walks every check, with where it was made as a prefix: "" for the
// machine, "acme: " for a multiverse, "acme/3: " for the universe.
func (ms doctorReport) each(f func(where string, c checkLine)) {
	for _, c := range ms.Checks {
		f("", c)
	}
	for _, w := range ms.Multiverses {
		for _, c := range w.Checks {
			f(w.Name+": ", c)
		}
	}
	if u := ms.Universe; u != nil {
		for _, c := range u.Checks {
			f(u.Multiverse+"/"+u.Name+": ", c)
		}
	}
}

func (ms doctorReport) failed() bool {
	failed := false
	ms.each(func(_ string, c checkLine) { failed = failed || c.Status == string(app.StatusFail) })
	return failed
}

func (r runner) printChecks(ms doctorReport) {
	o := r.out
	home, _ := os.UserHomeDir()
	width := 0
	ms.each(func(_ string, c checkLine) { width = max(width, utf8.RuneCountInString(c.Name)) })
	block := func(lines []checkLine) {
		var fixes []string
		seen := map[string]bool{}
		for _, c := range lines {
			glyph := o.Green("✓")
			detail := c.Detail
			if home != "" {
				detail = strings.ReplaceAll(detail, home+string(filepath.Separator), "~"+string(filepath.Separator))
			}
			switch c.Status {
			case string(app.StatusWarn):
				glyph = o.Yellow("!")
			case string(app.StatusFail):
				glyph = o.Red("✗")
			case string(app.StatusIdle):
				// Not a problem, so its command is a hint beside it, not a fix.
				glyph, detail = o.Dim("○"), o.Dim(detail)
				if c.Fix != "" {
					detail += o.Dim(" · ") + Markup("`"+c.Fix+"`", o.color)
				}
			}
			o.Plain("  %s %s  %s", glyph, c.Name+strings.Repeat(" ", width-utf8.RuneCountInString(c.Name)), detail)
			// One line per fix, under the block, however many checks it fixes.
			if c.Fix != "" && (c.Status == string(app.StatusWarn) || c.Status == string(app.StatusFail)) && !seen[c.Fix] {
				seen[c.Fix] = true
				fixes = append(fixes, c.Fix)
			}
		}
		for _, f := range fixes {
			o.Plain("  %s %s", o.Accent("→"), Markup("`"+f+"`", o.color))
		}
	}

	if len(ms.Checks) > 0 {
		o.Plain("%s", o.Bold("This machine"))
		block(ms.Checks)
	}
	for i, w := range ms.Multiverses {
		if i > 0 || len(ms.Checks) > 0 {
			o.Plain("")
		}
		o.Plain("%s  %s", o.Bold(w.Name), o.Dim(w.Root))
		block(w.Checks)
		if u := ms.Universe; u != nil && u.Multiverse == w.Name {
			o.Plain("")
			o.Plain("%s  %s", o.Bold("universe "+u.Name), o.Dim("of "+w.Name))
			block(u.Checks)
		}
	}

	warns, fails := 0, 0
	ms.each(func(_ string, c checkLine) {
		switch c.Status {
		case string(app.StatusWarn):
			warns++
		case string(app.StatusFail):
			fails++
		}
	})
	o.Plain("")
	switch {
	case fails > 0 && warns > 0:
		o.Plain("%s", o.Red(pluralize(fails, "failure")+", ")+o.Yellow(pluralize(warns, "warning")))
	case fails > 0:
		o.Plain("%s", o.Red(pluralize(fails, "failure")))
	case warns > 0:
		o.Plain("%s", o.Yellow(pluralize(warns, "warning")))
	default:
		o.Plain("%s", o.Green("all good"))
	}
}

func pluralize(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
