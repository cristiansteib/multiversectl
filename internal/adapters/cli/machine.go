package cli

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
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

// quietOut drops what a use case would print: status asks many services about
// many things, and what it wants from each is the answer, not their warnings.
type quietOut struct{}

func (quietOut) Info(string, ...any)        {}
func (quietOut) OK(string, ...any)          {}
func (quietOut) Warn(string, ...any)        {}
func (quietOut) Plain(string, ...any)       {}
func (quietOut) Table([]string, [][]string) {}

// machineStatus is everything `status` says, so the same answer can be printed
// or handed to a script.
type machineStatus struct {
	Version     string            `json:"version"`
	Catalog     app.CatalogStatus `json:"catalog"`
	Multiverses []multiverseLine  `json:"multiverses"`
	Proxy       proxyLine         `json:"proxy"`
	Server      serverLine        `json:"server"`
	Latest      string            `json:"latest,omitempty"`
	// Checks are what was looked at on the machine itself; each multiverse
	// has its own, and the universe asked about its own too.
	Checks   []checkLine    `json:"checks"`
	Universe *universeCheck `json:"universe,omitempty"`
	// Attention is every check that is not fine, as one line each, for a
	// script that only wants to know whether anything is to be done.
	Attention []string `json:"attention"`
}

type multiverseLine struct {
	Name      string      `json:"name"`
	From      string      `json:"from,omitempty"`
	Root      string      `json:"root"`
	Notes     []string    `json:"notes,omitempty"`
	Universes int         `json:"universes"`
	Running   int         `json:"running"`
	Error     string      `json:"error,omitempty"`
	Checks    []checkLine `json:"checks"`
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

// checkLine is one thing status looked at. Idle is not a finding: a proxy or a
// web view that is not running because nothing needs it.
type checkLine struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
	Fix    string `json:"fix,omitempty"`
}

const statusIdle = "idle"

func fromCheck(c app.Check) checkLine {
	line := checkLine{Name: c.Name, Status: string(c.Status), Detail: c.Detail, Fix: c.Fix}
	// An app that is not running is fine, and is not something that was
	// checked either.
	if c.Status == app.StatusOK && strings.HasPrefix(c.Name, "app ") && c.Detail == "not running" {
		line.Status = statusIdle
	}
	return line
}

func (r runner) machineStatusCmd() *cobra.Command {
	var fetch, preflight bool
	var format string
	cmd := &cobra.Command{
		Use:   "status [universe]",
		Short: "How this machine stands: what is installed and running, every check, and what to run to fix each",
		Long: "One answer to \"is this machine in order?\": the machine itself (the root catalog,\n" +
			"the container runtime, git, the shared proxy, the web view), then each installed\n" +
			"multiverse (whether its manifest is behind upstream, its shared clones, its domain,\n" +
			"its git host) with how many universes it has and how many run. Anything that is not\n" +
			"fine says what to run. Name a universe and it is checked too: its secrets, its apps\n" +
			"and whether every address answers. It exits 1 when a check fails.\n\n" +
			"It reads what was last fetched, and is quick; --fetch compares with the remotes and\n" +
			"looks for a newer release. --preflight asks instead what a first start needs from this\n" +
			"machine: docker reachable, compose and buildx present, free disk space, and, when a\n" +
			"multiverse is installed, that each of its repositories can be read and its secret\n" +
			"files are there.",
		Example: "  multiversectl status\n  multiversectl status 3                # and universe 3: its secrets, apps and addresses\n" +
			"  multiversectl status --fetch\n  multiversectl status --preflight\n  multiversectl status -o json",
		Args: cobra.MaximumNArgs(1),
	}
	cmd.Flags().BoolVar(&fetch, "fetch", false, "compare with the remotes and look for a newer release")
	cmd.Flags().BoolVar(&preflight, "preflight", false, "check what a first start needs from this machine")
	cmd.Flags().StringVarP(&format, "output", "o", "table", "output: table or json")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if format != "table" && format != "json" {
			return fmt.Errorf("--output must be table or json, not %q", format)
		}
		if preflight {
			if len(args) > 0 {
				return errors.New("--preflight looks at the machine, not at a universe: leave the universe out")
			}
			return r.preflight(cmd)
		}
		universe := ""
		if len(args) == 1 {
			universe = args[0]
		}
		var ms machineStatus
		err := r.spin(true, "Looking at this machine", func() (err error) { ms, err = r.collectStatus(cmd.Context(), fetch, universe); return err })
		if err != nil {
			return err
		}
		if format == "json" {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(ms); err != nil {
				return err
			}
		} else {
			r.printMachine(ms)
		}
		if ms.failed() {
			return errChecksFailed
		}
		return nil
	}
	return cmd
}

// preflight is what a first start would need from this machine, with no
// universe and nothing cloned.
func (r runner) preflight(cmd *cobra.Command) error {
	// The multiverse when there is one, so its repositories are checked too;
	// the machine alone when there is none.
	s, err := r.f.Multiverse(*r.g, r.out)
	full := err == nil
	if !full {
		if s, err = r.f.Bare(*r.g, r.out); err != nil {
			return err
		}
	}
	return r.reportPreflight(s.Preflight(cmd.Context(), app.PreflightInput{
		Path: r.installPath(s, ""), Machine: true, Repos: full, Secrets: full,
	}), true)
}

func (r runner) collectStatus(ctx context.Context, fetch bool, universe string) (machineStatus, error) {
	ms := machineStatus{Version: r.f.Version, Checks: []checkLine{}, Attention: []string{}}
	c, err := r.catalogs(false)
	if err != nil {
		return ms, err
	}
	rep, err := c.Status(ctx, fetch)
	if err != nil {
		return ms, err
	}
	ms.Catalog = rep.Catalog
	bare, err := r.f.Bare(*r.g, quietOut{})
	if err != nil {
		return ms, err
	}

	switch {
	case rep.Catalog.Source == "":
		ms.Checks = append(ms.Checks, checkLine{Name: "catalog", Status: statusIdle, Detail: "none", Fix: "multiversectl init"})
	case rep.Catalog.Unavailable != "":
		ms.Checks = append(ms.Checks, checkLine{Name: "catalog", Status: string(app.StatusFail), Detail: rep.Catalog.Source + " cannot be read: " + rep.Catalog.Unavailable})
	case rep.Catalog.NoRemote:
		ms.Checks = append(ms.Checks, checkLine{Name: "catalog", Status: string(app.StatusWarn), Detail: rep.Catalog.Source + " is only on this machine, so it is lost with it",
			Fix: "git -C " + filepath.Dir(rep.Catalog.Source) + " remote add origin <url>"})
	default:
		ms.Checks = append(ms.Checks, checkLine{Name: "catalog", Status: string(app.StatusOK), Detail: cmp.Or(domain.Locate(rep.Catalog.Source, "").Short(""), rep.Catalog.Source)})
	}

	// The universe asked about belongs to the multiverse the other universe
	// commands would pick: -m, the folder this is run in, or the only one.
	var asked *app.Service
	if universe != "" {
		if asked, err = r.f.Multiverse(*r.g, quietOut{}); err != nil {
			return ms, err
		}
	}

	var first *app.Service
	machine := map[string]bool{} // the machine's checks, by what they say
	universesRunning := 0
	for _, w := range rep.Multiverses {
		line := multiverseLine{Name: w.Name, From: firstNonEmpty(w.From, w.Source), Root: w.Root, Notes: w.Notes, Checks: []checkLine{}}
		manifest := checkLine{Name: "manifest", Status: string(app.StatusOK), Detail: "up to date with its source"}
		if !fetch {
			manifest.Detail += ", as of the last fetch"
		}
		switch {
		case w.Stale:
			manifest = checkLine{Name: "manifest", Status: string(app.StatusWarn), Detail: strings.Join(w.Notes, " · "), Fix: "multiversectl catalog sync " + w.Name}
		case w.Advice != "":
			manifest = checkLine{Name: "manifest", Status: string(app.StatusWarn), Detail: firstNonEmpty(strings.Join(w.Notes, " · "), "changed"), Fix: w.Advice}
		}
		var svc *app.Service
		if info, err := bare.MultiverseNamed(w.Name); err == nil {
			if svc, err = r.f.At(*r.g, quietOut{}, info.Root, info.Manifest); err != nil {
				line.Error = err.Error()
				manifest = checkLine{Name: "manifest", Status: string(app.StatusFail), Detail: "cannot be read: " + err.Error()}
			}
		}
		line.Checks = append(line.Checks, manifest)
		if svc != nil {
			if first == nil {
				first = svc
			}
			if list, err := svc.List(ctx, app.ListOptions{}); err == nil {
				line.Universes = len(list)
				for _, u := range list {
					if u.Running > 0 {
						line.Running++
					}
				}
				universesRunning += line.Running
			}
			in := app.DoctorInput{}
			mine := asked != nil && filepath.Clean(asked.Multiverse) == filepath.Clean(svc.Multiverse)
			if mine {
				in.Universe = universe
			}
			if report, err := svc.Doctor(ctx, in); err == nil {
				uc := universeCheck{Name: universe, Multiverse: w.Name, Checks: []checkLine{}}
				for _, ch := range report.Checks {
					switch {
					case ch.Scope == app.ScopeUniverse:
						uc.Checks = append(uc.Checks, fromCheck(ch))
					case ch.Scope == app.ScopeMachine:
						// Said once for the machine; a multiverse that sees it
						// differently (it wants the proxy on another address)
						// says so under its own name.
						key := ch.Name + "\x00" + ch.Detail
						switch {
						case len(machine) == 0 || ms.firstMachine(ch.Name) == nil:
							machine[key] = true
							ms.Checks = append(ms.Checks, fromCheck(ch))
						case !machine[key]:
							line.Checks = append(line.Checks, fromCheck(ch))
						}
					default:
						line.Checks = append(line.Checks, fromCheck(ch))
					}
				}
				if mine {
					ms.Universe = &uc
				}
			}
		}
		ms.Multiverses = append(ms.Multiverses, line)
	}
	if universe != "" && ms.Universe == nil {
		return ms, fmt.Errorf("universe %q: its multiverse is not installed on this machine", universe)
	}
	if first == nil {
		// Nothing installed: the machine is still worth looking at.
		if report, err := bare.Doctor(ctx, app.DoctorInput{}); err == nil {
			for _, ch := range report.Checks {
				if ch.Scope == app.ScopeMachine {
					ms.Checks = append(ms.Checks, fromCheck(ch))
				}
			}
		}
	}

	svc := cmp.Or(first, bare)
	if ps, err := svc.Proxy.Status(ctx); err == nil {
		ms.Proxy = proxyLine{Running: ps.Running, Entrypoints: ps.Entrypoints}
	}
	proxy := ms.firstMachine("proxy")
	switch {
	case proxy == nil:
	case !ms.Proxy.Running && universesRunning > 0:
		*proxy = checkLine{Name: "proxy", Status: string(app.StatusFail), Detail: "not running, so the universes that are do not answer", Fix: "multiversectl system proxy up"}
	case !ms.Proxy.Running:
		// Nothing needs it yet: the first start brings it up.
		*proxy = checkLine{Name: "proxy", Status: statusIdle, Detail: "not running; the first start brings it up"}
	default:
		names := make([]string, 0, len(ms.Proxy.Entrypoints))
		for n, p := range ms.Proxy.Entrypoints {
			names = append(names, fmt.Sprintf("%s:%d", n, p))
		}
		sort.Strings(names)
		if len(names) > 0 {
			proxy.Detail += " · " + strings.Join(names, ", ")
		}
	}

	view := checkLine{Name: "web view", Status: statusIdle, Detail: "not running", Fix: "multiversectl server up"}
	if p, err := r.f.Server(); err == nil {
		if info, err := p.Status(ctx); err == nil {
			ms.Server = serverLine{Running: true, URL: info.URL, Outdated: info.Outdated}
			view = checkLine{Name: "web view", Status: string(app.StatusOK), Detail: info.URL}
			if info.Outdated {
				view = checkLine{Name: "web view", Status: string(app.StatusWarn), Detail: "runs an older build than the one installed", Fix: "multiversectl server restart"}
			}
		}
	}
	ms.Checks = append(ms.Checks, view)

	if fetch {
		if rel, err := bare.Releases.Latest(ctx); err == nil {
			ms.Latest = rel.Version
			if app.Released(r.f.Version) && app.NewerVersion(rel.Version, r.f.Version) {
				ms.Checks = append(ms.Checks, checkLine{Name: "release", Status: string(app.StatusWarn),
					Detail: fmt.Sprintf("multiversectl %s is out (this is %s)", rel.Version, r.f.Version), Fix: "multiversectl self-update"})
			}
		}
	}

	ms.each(func(where string, c checkLine) {
		if c.Status == string(app.StatusWarn) || c.Status == string(app.StatusFail) {
			a := where + c.Name + ": " + c.Detail
			if c.Fix != "" {
				a += ": `" + c.Fix + "`"
			}
			ms.Attention = append(ms.Attention, a)
		}
	})
	return ms, nil
}

// firstMachine is the machine's check of that name, to say more in it.
func (ms *machineStatus) firstMachine(name string) *checkLine {
	for i := range ms.Checks {
		if ms.Checks[i].Name == name {
			return &ms.Checks[i]
		}
	}
	return nil
}

// each walks every check, with where it was made as a prefix: "" for the
// machine, "acme: " for a multiverse, "acme/3: " for the universe.
func (ms machineStatus) each(f func(where string, c checkLine)) {
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

func (ms machineStatus) failed() bool {
	failed := false
	ms.each(func(_ string, c checkLine) { failed = failed || c.Status == string(app.StatusFail) })
	return failed
}

func (r runner) printMachine(ms machineStatus) {
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
			case statusIdle:
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

	o.Plain("%s %s", o.Bold("multiversectl"), o.Dim(ms.Version))
	o.Plain("")
	o.Plain("%s", o.Bold("This machine"))
	block(ms.Checks)
	if len(ms.Multiverses) == 0 {
		o.Plain("")
		o.Plain("%s  %s", o.Bold("No multiverse installed"), Markup("`multiversectl start <owner/repo>`", o.color))
	}
	for _, w := range ms.Multiverses {
		o.Plain("")
		universes := pluralize(w.Universes, "universe") + ", none running"
		if w.Running > 0 {
			universes = pluralize(w.Universes, "universe") + ", " + o.Green(fmt.Sprintf("%d running", w.Running))
		}
		if w.Universes == 0 {
			universes = "no universes"
		}
		o.Plain("%s  %s", o.Bold(w.Name), o.Dim(firstNonEmpty(w.From, w.Root)+" · ")+universes)
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
