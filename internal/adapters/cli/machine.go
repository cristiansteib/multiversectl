package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

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
	Attention   []string          `json:"attention"`
}

type multiverseLine struct {
	Name      string   `json:"name"`
	Current   bool     `json:"current,omitempty"`
	From      string   `json:"from,omitempty"`
	Root      string   `json:"root"`
	Notes     []string `json:"notes,omitempty"`
	Universes int      `json:"universes"`
	Running   int      `json:"running"`
	Error     string   `json:"error,omitempty"`
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

func (r runner) machineStatusCmd() *cobra.Command {
	var fetch bool
	var format string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "How this machine stands: the root, what is installed and drifted, what is running, and what needs attention",
		Long: "One answer to \"is this machine in order?\": the root catalog, each installed\n" +
			"multiverse with how many universes it has and how many run, the shared proxy and\n" +
			"the web server, and, first, what needs attention: a root that cannot be read or\n" +
			"is not marked, a multiverse behind its remote, universes running without the\n" +
			"proxy, and anything `doctor` warns about. It reads what was last fetched, and is\n" +
			"quick; --fetch compares with the remotes and looks for a newer release.\n" +
			"`status` and `universe ls -o list` have the detail of each part.",
		Example: "  multiversectl status\n  multiversectl status --fetch\n  multiversectl status -o json",
		Args:    cobra.NoArgs,
	}
	cmd.Flags().BoolVar(&fetch, "fetch", false, "compare with the remotes and look for a newer release")
	cmd.Flags().StringVarP(&format, "output", "o", "table", "output: table or json")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if format != "table" && format != "json" {
			return fmt.Errorf("--output must be table or json, not %q", format)
		}
		var ms machineStatus
		err := r.spin(true, "Looking at this machine", func() (err error) { ms, err = r.collectStatus(cmd.Context(), fetch); return err })
		if err != nil {
			return err
		}
		if format == "json" {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(ms)
		}
		r.printMachine(ms)
		return nil
	}
	return cmd
}

func (r runner) collectStatus(ctx context.Context, fetch bool) (machineStatus, error) {
	ms := machineStatus{Version: r.f.Version, Attention: []string{}}
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

	var current *app.Service
	var universesRunning int
	for _, w := range rep.Multiverses {
		line := multiverseLine{Name: w.Name, From: firstNonEmpty(w.From, w.Source), Root: w.Root, Notes: w.Notes}
		if info, err := bare.MultiverseNamed(w.Name); err == nil {
			svc, err := r.f.At(*r.g, quietOut{}, info.Root, info.Manifest)
			if err != nil {
				line.Error = err.Error()
				ms.Attention = append(ms.Attention, fmt.Sprintf("`%s`: its manifest cannot be read: %v", w.Name, err))
			} else {
				if current == nil {
					current = svc
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
			}
		}
		switch {
		case w.Stale:
			ms.Attention = append(ms.Attention, fmt.Sprintf("`%s` is behind its remote (%s): `multiversectl catalog sync %s`", w.Name, strings.Join(w.Notes, " · "), w.Name))
		case w.Advice != "":
			ms.Attention = append(ms.Attention, fmt.Sprintf("`%s`: %s", w.Name, w.Advice))
		}
		ms.Multiverses = append(ms.Multiverses, line)
	}

	svc := current
	if svc == nil {
		svc = bare
	}
	if ps, err := svc.Proxy.Status(ctx); err == nil {
		ms.Proxy = proxyLine{Running: ps.Running, Entrypoints: ps.Entrypoints}
	}
	if p, err := r.f.Server(); err == nil {
		if info, err := p.Status(ctx); err == nil {
			ms.Server = serverLine{Running: true, URL: info.URL, Outdated: info.Outdated}
			if info.Outdated {
				ms.Attention = append(ms.Attention, "the server is running an older build than the one installed: `multiversectl server restart`")
			}
		}
	}

	switch {
	case rep.Catalog.Unavailable != "":
		ms.Attention = append(ms.Attention, "the catalog cannot be read: "+rep.Catalog.Unavailable)
	}
	if rep.Catalog.NoRemote {
		ms.Attention = append(ms.Attention, "the catalog is only on this machine, so it is lost with it: `git -C "+filepath.Dir(rep.Catalog.Source)+" remote add origin <url>`")
	}
	if universesRunning > 0 && !ms.Proxy.Running {
		ms.Attention = append(ms.Attention, "universes are running but the shared proxy is not, so their addresses do not answer: `multiversectl system proxy up`")
	}
	// What doctor warns about is attention too; its manifest warning is the same
	// thing as "nothing installed", which the header already says.
	if report, err := svc.Doctor(ctx, app.DoctorInput{}); err == nil {
		for _, ch := range report.Checks {
			if ch.Status == app.StatusOK || (ch.Name == "manifest" && len(rep.Multiverses) == 0) {
				continue
			}
			ms.Attention = append(ms.Attention, fmt.Sprintf("doctor: %s: %s", ch.Name, ch.Detail))
		}
	}
	if fetch {
		if rel, err := bare.Releases.Latest(ctx); err == nil {
			ms.Latest = rel.Version
			if app.Released(r.f.Version) && app.NewerVersion(rel.Version, r.f.Version) {
				ms.Attention = append(ms.Attention, fmt.Sprintf("multiversectl %s is out (this is %s): `multiversectl self-update`", rel.Version, r.f.Version))
			}
		}
	}
	return ms, nil
}

func (r runner) printMachine(ms machineStatus) {
	o := r.out
	o.Plain("%s %s", o.Bold("multiversectl"), o.Dim(ms.Version+" · this machine"))
	o.Plain("")

	catalog := "none"
	switch {
	case ms.Catalog.Source == "":
		catalog = o.Dim("none: ") + "`multiversectl init`"
	case ms.Catalog.Unavailable != "":
		catalog = o.Place(domain.Locate(ms.Catalog.Source, "")) + "  " + o.Red("✗ unreadable")
	default:
		catalog = o.Place(domain.Locate(ms.Catalog.Source, "")) + "  " + o.Green("✓")
	}
	o.Plain("%s  %s", o.Bold("Catalog    "), catalog)

	if len(ms.Multiverses) == 0 {
		o.Plain("%s  %s", o.Bold("Multiverses   "), o.Dim("nothing installed: ")+"`multiversectl start <owner/repo>`")
	} else {
		o.Plain("%s", o.Bold("Multiverses"))
		cells := make([][]Cell, len(ms.Multiverses))
		for i, w := range ms.Multiverses {
			name := "  " + w.Name
			if w.Current {
				name = "★ " + w.Name
			}
			state := Cell{Text: "up to date", Style: green}
			switch {
			case w.Error != "":
				state = Cell{Text: "✗ unreadable", Style: red}
			case len(w.Notes) > 0:
				state = Cell{Text: strings.Join(w.Notes, " · "), Style: yellow}
			}
			uni := Cell{Text: pluralize(w.Universes, "universe"), Style: dim}
			if w.Running > 0 {
				uni = Cell{Text: pluralize(w.Universes, "universe") + ", " + fmt.Sprint(w.Running) + " running", Style: green}
			}
			cells[i] = []Cell{{Text: name}, {Text: firstNonEmpty(w.From, "—"), Style: dim}, state, uni}
		}
		o.Cells([]string{"  NAME", "FROM", "STATE", "UNIVERSES"}, cells)
	}

	proxy := o.Dim("not running")
	if ms.Proxy.Running {
		names := make([]string, 0, len(ms.Proxy.Entrypoints))
		for n, p := range ms.Proxy.Entrypoints {
			names = append(names, fmt.Sprintf("%s:%d", n, p))
		}
		sort.Strings(names)
		proxy = o.Green("running") + o.Dim("  "+strings.Join(names, ", "))
	}
	o.Plain("%s  %s", o.Bold("Proxy      "), proxy)
	if ms.Server.Running {
		o.Plain("%s  %s", o.Bold("Server     "), o.Green("running")+"  `"+ms.Server.URL+"`")
	} else {
		o.Plain("%s  %s", o.Bold("Server     "), o.Dim("not running: ")+"`multiversectl server up`")
	}

	o.Plain("")
	if len(ms.Attention) == 0 {
		o.OK("nothing needs attention")
	} else {
		verb := "need"
		if len(ms.Attention) == 1 {
			verb = "needs"
		}
		o.Plain("%s", o.Bold(o.Yellow(pluralize(len(ms.Attention), "thing")+" "+verb+" attention")))
		for _, a := range ms.Attention {
			o.Warn("%s", a)
		}
	}
	o.Plain("")
	o.Info("detail: `multiversectl status`  ·  `multiversectl universe ls -o list`  ·  `multiversectl universe doctor`")
}

func pluralize(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
