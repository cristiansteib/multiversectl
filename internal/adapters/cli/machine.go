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
	Version    string          `json:"version"`
	Root       app.RootStatus  `json:"root"`
	Workspaces []workspaceLine `json:"projects"`
	Proxy      proxyLine       `json:"proxy"`
	Server     serverLine      `json:"server"`
	Latest     string          `json:"latest,omitempty"`
	Attention  []string        `json:"attention"`
}

type workspaceLine struct {
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
}

func (r runner) machineStatusCmd() *cobra.Command {
	var fetch bool
	var format string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "How this machine stands: the root, what is installed and drifted, what is running, and what needs attention",
		Long: "One answer to \"is this machine in order?\": the root catalog, each installed\n" +
			"project with how many universes it has and how many run, the shared proxy and\n" +
			"the web server, and, first, what needs attention: a root that cannot be read or\n" +
			"is not marked, a project behind its remote, universes running without the\n" +
			"proxy, and anything `doctor` warns about. It reads what was last fetched, and is\n" +
			"quick; --fetch compares with the remotes and looks for a newer release.\n" +
			"`catalog status` and `universe status` have the detail of each part.",
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
	ms.Root = rep.Root
	bare, err := r.f.Bare(*r.g, quietOut{})
	if err != nil {
		return ms, err
	}

	var current *app.Service
	var universesRunning int
	for _, w := range rep.Workspaces {
		line := workspaceLine{Name: w.Name, Current: w.Current, From: firstNonEmpty(w.From, w.Source), Root: w.Root, Notes: w.Notes}
		if info, err := bare.WorkspaceNamed(w.Name); err == nil {
			svc, err := r.f.At(*r.g, quietOut{}, info.Root, info.Manifest)
			if err != nil {
				line.Error = err.Error()
				ms.Attention = append(ms.Attention, fmt.Sprintf("`%s`: its manifest cannot be read: %v", w.Name, err))
			} else {
				if w.Current || current == nil {
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
		if w.Stale {
			ms.Attention = append(ms.Attention, fmt.Sprintf("`%s` is behind its remote (%s): `multiversectl project update %s`", w.Name, strings.Join(w.Notes, " · "), w.Name))
		}
		ms.Workspaces = append(ms.Workspaces, line)
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
			ms.Server = serverLine{Running: true, URL: info.URL}
		}
	}

	switch {
	case rep.Root.Unavailable != "":
		ms.Attention = append(ms.Attention, "the root catalog cannot be read: "+rep.Root.Unavailable)
	case rep.Root.Legacy:
		ms.Attention = append(ms.Attention, "the root is a plain Catalog, which a new machine cannot tell from a project's: `multiversectl init` marks it as your root")
	}
	if rep.Root.NoRemote {
		ms.Attention = append(ms.Attention, "the root catalog is only on this machine, so it is lost with it: `git -C "+filepath.Dir(rep.Root.Source)+" remote add origin <url>`")
	}
	if universesRunning > 0 && !ms.Proxy.Running {
		ms.Attention = append(ms.Attention, "universes are running but the shared proxy is not, so their addresses do not answer: `multiversectl universe proxy up`")
	}
	// What doctor warns about is attention too; its manifest warning is the same
	// thing as "nothing installed", which the header already says.
	if report, err := svc.Doctor(ctx, app.DoctorInput{}); err == nil {
		for _, ch := range report.Checks {
			if ch.Status == app.StatusOK || (ch.Name == "manifest" && len(rep.Workspaces) == 0) {
				continue
			}
			ms.Attention = append(ms.Attention, fmt.Sprintf("doctor: %s: %s", ch.Name, ch.Detail))
		}
	}
	if fetch {
		if rel, err := bare.Releases.Latest(ctx); err == nil {
			ms.Latest = rel.Version
			if looksReleased(r.f.Version) && app.NewerVersion(rel.Version, r.f.Version) {
				ms.Attention = append(ms.Attention, fmt.Sprintf("multiversectl %s is out (this is %s): `multiversectl self-update`", rel.Version, r.f.Version))
			}
		}
	}
	return ms, nil
}

// looksReleased is true for a version that came from a release, not a build
// from source, which is neither older nor newer than one.
func looksReleased(v string) bool {
	v = strings.TrimPrefix(v, "v")
	return v != "" && v[0] >= '0' && v[0] <= '9'
}

func (r runner) printMachine(ms machineStatus) {
	o := r.out
	o.Plain("%s %s", o.Bold("multiversectl"), o.Dim(ms.Version+" · this machine"))
	o.Plain("")

	root := "none"
	switch {
	case ms.Root.Source == "":
		root = o.Dim("none: ") + "`multiversectl init`"
	case ms.Root.Unavailable != "":
		root = o.Place(domain.Locate(ms.Root.Source, "")) + "  " + o.Red("✗ unreadable")
	case ms.Root.Legacy:
		root = o.Place(domain.Locate(ms.Root.Source, "")) + "  " + o.Yellow("plain Catalog")
	default:
		root = o.Place(domain.Locate(ms.Root.Source, "")) + "  " + o.Green("✓ CatalogRoot")
	}
	o.Plain("%s  %s", o.Bold("Root       "), root)

	if len(ms.Workspaces) == 0 {
		o.Plain("%s  %s", o.Bold("Projects   "), o.Dim("nothing installed: ")+"`multiversectl project install <owner/repo>`")
	} else {
		o.Plain("%s", o.Bold("Projects"))
		cells := make([][]Cell, len(ms.Workspaces))
		for i, w := range ms.Workspaces {
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
		o.Plain("%s  %s", o.Bold("Server     "), o.Dim("not running: ")+"`multiversectl server start`")
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
	o.Info("detail: `multiversectl project status`  ·  `multiversectl universe status`  ·  `multiversectl universe doctor`")
}

func pluralize(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
