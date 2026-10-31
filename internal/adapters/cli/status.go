package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cristiansteib/multiversectl/internal/app"
	"github.com/cristiansteib/multiversectl/internal/domain"
)

// statusReport is what `status` says: what is on this machine and what runs,
// and no judgement of it, which is `doctor`'s.
type statusReport struct {
	Version     string             `json:"version"`
	Catalog     app.CatalogStatus  `json:"catalog"`
	Proxy       proxyLine          `json:"proxy"`
	Server      serverLine         `json:"server"`
	Multiverses []multiverseStatus `json:"multiverses"`
}

type multiverseStatus struct {
	Name string `json:"name"`
	From string `json:"from,omitempty"`
	Root string `json:"root"`
	// Notes say how its manifest differs from where it came from; none is up to date.
	Notes     []string         `json:"notes,omitempty"`
	Error     string           `json:"error,omitempty"`
	Universes []universeStatus `json:"universes"`
}

type universeStatus struct {
	Name        string `json:"name"`
	State       string `json:"state"`
	Up          string `json:"up"`
	LastStarted string `json:"lastStarted,omitempty"`
	Branch      string `json:"branch"`
	About       string `json:"about,omitempty"`
	PR          string `json:"pr,omitempty"`

	style   string
	started time.Time
}

func (r runner) statusCmd() *cobra.Command {
	var fetch bool
	var format string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "What is on this machine and what runs: the catalog, the proxy, the web view, every multiverse and its universes",
		Long: "The root catalog, the shared proxy and the web view, and then each installed\n" +
			"multiverse — where it came from and whether its manifest is behind that — with\n" +
			"one line per universe: running, partial, stopped or only created, how many of its\n" +
			"services are up, when it was last started, its branch and what it is for. It says\n" +
			"how things are; `multiversectl doctor` checks the machine and `universe doctor` a\n" +
			"multiverse and its universes, and they say what to fix. It reads what was last\n" +
			"fetched; --fetch compares with the remotes first.",
		Example: "  multiversectl status\n  multiversectl status --fetch\n  multiversectl status -o json",
		Args:    cobra.NoArgs,
	}
	cmd.Flags().BoolVar(&fetch, "fetch", false, "compare each manifest with its remote first")
	cmd.Flags().StringVarP(&format, "output", "o", "table", "output: table or json")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if format != "table" && format != "json" {
			return fmt.Errorf("--output must be table or json, not %q", format)
		}
		var st statusReport
		err := r.spin(true, "Looking at this machine", func() (err error) { st, err = r.collectStatus(cmd.Context(), fetch); return err })
		if err != nil {
			return err
		}
		if format == "json" {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(st)
		}
		r.printStatus(st)
		return nil
	}
	return cmd
}

func (r runner) collectStatus(ctx context.Context, fetch bool) (statusReport, error) {
	st := statusReport{Version: r.f.Version, Multiverses: []multiverseStatus{}}
	c, err := r.catalogs(false)
	if err != nil {
		return st, err
	}
	rep, err := c.Status(ctx, fetch)
	if err != nil {
		return st, err
	}
	st.Catalog = rep.Catalog
	bare, err := r.f.Bare(*r.g, quietOut{})
	if err != nil {
		return st, err
	}
	if ps, err := bare.Proxy.Status(ctx); err == nil {
		st.Proxy = proxyLine{Running: ps.Running, Entrypoints: ps.Entrypoints}
	}
	if p, err := r.f.Server(); err == nil {
		if info, err := p.Status(ctx); err == nil {
			st.Server = serverLine{Running: true, URL: info.URL, Outdated: info.Outdated}
		}
	}
	for _, w := range rep.Multiverses {
		line := multiverseStatus{Name: w.Name, From: firstNonEmpty(w.From, w.Source), Root: w.Root, Notes: w.Notes, Universes: []universeStatus{}}
		info, err := bare.MultiverseNamed(w.Name)
		if err != nil {
			line.Error = err.Error()
			st.Multiverses = append(st.Multiverses, line)
			continue
		}
		svc, err := r.f.At(*r.g, quietOut{}, info.Root, info.Manifest)
		if err != nil {
			line.Error = "its manifest cannot be read: " + err.Error()
			st.Multiverses = append(st.Multiverses, line)
			continue
		}
		list, err := svc.List(ctx, app.ListOptions{Services: true})
		if err != nil {
			line.Error = err.Error()
		}
		for _, u := range list {
			state, style, up := standing(u)
			us := universeStatus{Name: u.Name, State: state, Up: up, Branch: oneBranch(u.Branches), About: u.Note, PR: u.PR, style: style, started: u.LastStarted}
			if !u.LastStarted.IsZero() {
				us.LastStarted = u.LastStarted.UTC().Format("2006-01-02T15:04:05Z")
			}
			line.Universes = append(line.Universes, us)
		}
		st.Multiverses = append(st.Multiverses, line)
	}
	return st, nil
}

// oneBranch is a universe's branch, said once when every repository is on the
// same one, which is the usual case, and repo=branch for each otherwise.
func oneBranch(branches map[string]string) string {
	seen := map[string]bool{}
	for _, b := range branches {
		seen[b] = true
	}
	if len(seen) == 1 {
		for b := range seen {
			return b
		}
	}
	return joinPairs(branches)
}

func (r runner) printStatus(st statusReport) {
	o := r.out
	label := func(s string) string { return o.Bold(fmt.Sprintf("%-9s", s)) }
	o.Plain("%s %s", o.Bold("multiversectl"), o.Dim(st.Version))
	o.Plain("")

	switch {
	case st.Catalog.Source == "":
		o.Plain("%s  %s", label("Catalog"), o.Dim("none · ")+Markup("`multiversectl init`", o.color))
	case st.Catalog.Unavailable != "":
		o.Plain("%s  %s", label("Catalog"), o.Place(domain.Locate(st.Catalog.Source, ""))+o.Red(" · cannot be read"))
	default:
		o.Plain("%s  %s", label("Catalog"), o.Place(domain.Locate(st.Catalog.Source, "")))
	}
	if st.Proxy.Running {
		names := make([]string, 0, len(st.Proxy.Entrypoints))
		for n, p := range st.Proxy.Entrypoints {
			names = append(names, fmt.Sprintf("%s:%d", n, p))
		}
		sort.Strings(names)
		o.Plain("%s  %s", label("Proxy"), o.Green("running")+o.Dim(" · "+strings.Join(names, ", ")))
	} else {
		o.Plain("%s  %s", label("Proxy"), o.Dim("not running"))
	}
	switch {
	case st.Server.Running && st.Server.Outdated:
		o.Plain("%s  %s", label("Web view"), o.Yellow("running an older build")+o.Dim(" · ")+Markup("`"+st.Server.URL+"`", o.color))
	case st.Server.Running:
		o.Plain("%s  %s", label("Web view"), o.Green("running")+o.Dim(" · ")+Markup("`"+st.Server.URL+"`", o.color))
	default:
		o.Plain("%s  %s", label("Web view"), o.Dim("not running"))
	}

	if len(st.Multiverses) == 0 {
		o.Plain("")
		o.Plain("%s  %s", o.Bold("No multiverse installed"), Markup("`multiversectl start <owner/repo>`", o.color))
	}
	for _, w := range st.Multiverses {
		o.Plain("")
		// The notes name what they are about: "manifest behind 2".
		manifest := o.Green("manifest up to date")
		switch {
		case w.Error != "":
			manifest = o.Red(w.Error)
		case len(w.Notes) > 0:
			manifest = o.Yellow(strings.Join(w.Notes, " · "))
		}
		o.Plain("%s  %s", o.Bold(w.Name), o.Dim(firstNonEmpty(w.From, w.Root)+" · ")+manifest)
		if w.Error != "" {
			continue
		}
		if len(w.Universes) == 0 {
			o.Plain("  %s", o.Dim("no universes yet · ")+Markup("`multiversectl universe new`", o.color))
			continue
		}
		rows := make([][]Cell, len(w.Universes))
		for i, u := range w.Universes {
			started := "never"
			if !u.started.IsZero() {
				started = age(u.started)
			}
			rows[i] = []Cell{{Text: "  " + u.Name}, {Text: u.State, Style: u.style}, {Text: u.Up}, {Text: started}, {Text: u.Branch}, {Text: strings.TrimSpace(u.PR + " " + u.About), Style: dim}}
		}
		o.Cells([]string{"  UNIVERSE", "STATE", "UP", "LAST STARTED", "BRANCH", "ABOUT"}, rows)
	}
	o.Plain("")
	o.Info("checks, and what to fix: `multiversectl doctor` for the machine, `multiversectl universe doctor` for a multiverse and its universes")
}
