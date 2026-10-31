package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cristiansteib/multiversectl/internal/app"
)

// footprintCmd answers "what would I delete to get rid of all of this?". It only
// lists: removing the clones of your repositories is the one thing here that can
// lose work, so each is shown with what it holds before anyone decides.
//
// It is called footprint, and was called cleanup, because it removes nothing: a
// command named for an action that sits beside `prune`, which does delete, is a
// mistake waiting to be typed.
func (r runner) footprintCmd() *cobra.Command {
	var script bool
	var format string
	cmd := &cobra.Command{
		Use:   "footprint",
		Short: "List everything multiversectl put on this machine, and how to remove it (removes nothing)",
		Long: "Lists what this tool has created: the web server and proxy that may be running,\n" +
			"every universe with its containers, volumes and worktrees, the clones of your\n" +
			"repositories, branches left by universes that are gone, the configuration and\n" +
			"caches, shell completions, and the programs themselves, each with its size and\n" +
			"the command that removes it. Whatever would lose work that exists nowhere else\n" +
			"(uncommitted changes, commits on no remote, a volume's data) is marked, and the\n" +
			"commands come last, in the order that is safe to run them. It removes nothing.\n\n" +
			"A multiverse's folder may hold files this tool did not create; those are not\n" +
			"listed. The root catalog's repository, on GitHub or GitLab, is not touched.",
		Example: "  multiversectl system footprint\n  multiversectl system footprint --script > clean.sh\n  multiversectl system footprint -o json",
		Args:    cobra.NoArgs,
	}
	cmd.Flags().BoolVar(&script, "script", false, "print only the commands, one per line, to read and run")
	cmd.Flags().StringVarP(&format, "output", "o", "table", "output: table or json")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if format != "table" && format != "json" {
			return fmt.Errorf("--output must be table or json, not %q", format)
		}
		var items []app.Item
		err := r.spin(true, "Looking at what is on this machine", func() (err error) { items, err = r.collectFootprint(cmd.Context()); return err })
		if err != nil {
			return err
		}
		switch {
		case format == "json":
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(items)
		case script:
			for _, line := range removalPlan(items) {
				fmt.Fprintln(os.Stdout, line.text)
			}
		default:
			r.printFootprint(items)
		}
		return nil
	}
	return cmd
}

func (r runner) collectFootprint(ctx context.Context) ([]app.Item, error) {
	c, err := r.catalogs(false)
	if err != nil {
		return nil, err
	}
	bare, err := r.f.Bare(*r.g, quietOut{})
	if err != nil {
		return nil, err
	}
	var items []app.Item

	if p, err := r.f.Server(); err == nil {
		if info, err := p.Status(ctx); err == nil {
			// The link carries a token that is a password; the address is enough here.
			addr, _, _ := strings.Cut(info.URL, "?")
			items = append(items, app.Item{Kind: "server", What: "the web server", Path: addr, Size: -1,
				Note: fmt.Sprintf("running, pid %d", info.PID), Remove: "multiversectl server down"})
		}
	}

	list, err := bare.Multiverses()
	if err != nil {
		return nil, err
	}
	proxySeen := false
	for _, w := range list {
		svc, err := r.f.At(*r.g, quietOut{}, w.Root, w.Manifest)
		if err != nil {
			items = append(items, app.Item{Kind: "clone", What: "multiverse " + w.Name + ": its manifest cannot be read, so its folder was not looked into", Path: w.Root, Size: -1})
			continue
		}
		if !proxySeen {
			proxySeen = true
			if ps, err := svc.Proxy.Status(ctx); err == nil && ps.Running {
				names := make([]string, 0, len(ps.Entrypoints))
				for n, p := range ps.Entrypoints {
					names = append(names, fmt.Sprintf("%s:%d", n, p))
				}
				sort.Strings(names)
				items = append(items, app.Item{Kind: "proxy", What: "the shared proxy", Path: "multiverse-proxy", Size: -1,
					Note: "running, " + strings.Join(names, ", "), Remove: "multiversectl system proxy down && docker network rm " + firstNonEmpty(svc.Manifest.Proxy.Network, "multiversenet")})
			}
		}
		pi, err := svc.ProjectFootprint(ctx, w.Name)
		if err != nil {
			return nil, err
		}
		items = append(items, pi...)
	}

	if !proxySeen {
		// With no multiverse installed the proxy may still be up from one that was.
		if ps, err := bare.Proxy.Status(ctx); err == nil && ps.Running {
			names := make([]string, 0, len(ps.Entrypoints))
			for n, p := range ps.Entrypoints {
				names = append(names, fmt.Sprintf("%s:%d", n, p))
			}
			sort.Strings(names)
			items = append(items, app.Item{Kind: "proxy", What: "the shared proxy", Path: "multiverse-proxy", Size: -1,
				Note: "running, " + strings.Join(names, ", "), Remove: "docker compose --project-name multiverse-proxy down && docker network rm multiversenet"})
		}
	}
	items = append(items, c.MachineFootprint()...)
	items = append(items, r.completions().Footprint()...)
	items = append(items, r.programItems(bare)...)
	return items, nil
}

// programItems are the binaries: this one, the server beside it, and the short
// name a package installs.
func (r runner) programItems(bare *app.Service) []app.Item {
	var paths []string
	seen := map[string]bool{}
	add := func(p string) {
		if p == "" || seen[p] {
			return
		}
		if _, err := os.Lstat(p); err == nil {
			seen[p] = true
			paths = append(paths, p)
		}
	}
	if exe, err := os.Executable(); err == nil {
		add(exe)
		add(filepath.Join(filepath.Dir(exe), "multiversectl-server"))
	}
	for _, name := range []string{"multiversectl", "multiversectl-server", "mvctl"} {
		if p, err := exec.LookPath(name); err == nil {
			add(p)
		}
	}
	items := make([]app.Item, 0, len(paths))
	for _, p := range paths {
		// Quoted, because --script writes these out to be run: a binary under a
		// path with a space in it would otherwise become two arguments to rm.
		it := app.Item{Kind: "program", What: filepath.Base(p), Path: p, Size: -1, Remove: "rm " + shellQuote(p)}
		if n, err := bare.Files.Size(p); err == nil {
			it.Size = n
		}
		items = append(items, it)
	}
	return items
}

type section struct{ kind, title, hint string }

var sections = []section{
	{"server", "Running now", "stop these first"},
	{"proxy", "", ""},
	{"universe", "Universes", "removing one deletes its worktrees, its containers and its volumes, which hold its data"},
	{"branch", "Branches left behind", "started by universes that are gone"},
	{"clone", "Clones of your repositories", "yours: check nothing is unpushed before deleting one"},
	{"copy", "Copies of manifests", "kept inside each multiverse's folder"},
	{"state", "Configuration, caches and state", "all of it is in one folder"},
	{"completion", "Shell completions", ""},
	{"program", "The programs", "last, so everything above can still be undone with them"},
}

func (r runner) printFootprint(items []app.Item) {
	o := r.out
	var total int64
	risky := 0
	for _, it := range items {
		if it.Size > 0 {
			total += it.Size
		}
		if it.Risk {
			risky++
		}
	}
	o.Plain("%s  %s", o.Bold("multiversectl system footprint"), o.Dim("(this lists; it removes nothing)"))

	printed := map[string]bool{}
	for _, s := range sections {
		var group []app.Item
		for _, it := range items {
			if it.Kind == s.kind {
				group = append(group, it)
			}
		}
		if len(group) == 0 {
			continue
		}
		title := s.title
		if title == "" { // proxy joins the server's section
			title = "Running now"
		}
		if !printed[title] {
			printed[title] = true
			o.Plain("")
			head := o.Bold(title)
			if s.hint != "" {
				head += "  " + o.Dim(s.hint)
			}
			o.Plain("%s", head)
		}
		for _, it := range group {
			mark := "  "
			if it.Risk {
				mark = o.Yellow("⚠ ")
			}
			size := ""
			if it.Size >= 0 {
				size = "  " + o.Dim(humanBytes(it.Size))
			}
			o.Plain("%s%s%s", mark, it.What, size)
			if it.Path != "" {
				o.Plain("    `%s`", it.Path)
			}
			if it.Note != "" {
				note := it.Note
				if it.Risk {
					note = o.Yellow(note)
				}
				o.Plain("    %s", note)
			}
		}
	}
	if len(items) == 0 {
		o.Plain("")
		o.OK("nothing of multiversectl's is on this machine")
		return
	}

	o.Plain("")
	o.Plain("%s", o.Bold("To remove everything, in this order"))
	n := 0
	for _, step := range removalPlan(items) {
		if step.comment {
			o.Plain("  %s", o.Dim(step.text))
			continue
		}
		n++
		o.Plain("  %2d. `%s`", n, step.text)
	}
	o.Plain("")
	o.Info("this lists what is still installed: the folder of a multiverse you already forgot with `system reset` or `system forget` is not known any more, so look in the folder you installed it into (`~/multiverse` by default)")
	line := fmt.Sprintf("about %s in all", humanBytes(total))
	if risky > 0 {
		line += fmt.Sprintf("; %s marked ⚠ would lose work that is nowhere else", plural(risky, "thing"))
	}
	o.Info("%s. Run `multiversectl system footprint --script` for just the commands.", line)
}

type step struct {
	text    string
	comment bool
}

// removalPlan orders the commands so each is safe to run: stop what is running,
// remove the universes (which asks first), the branches, the completions, then
// the clones and the configuration, and the programs last.
func removalPlan(items []app.Item) []step {
	order := []string{"server", "proxy", "universe", "branch", "completion", "clone", "copy", "state", "program"}
	var plan []step
	seen := map[string]bool{}
	for _, kind := range order {
		if kind == "state" {
			// The machine's own files all live in one folder, which is one command.
			for _, it := range items {
				if it.Kind != "state" {
					continue
				}
				cmd := "rm -rf " + shellQuote(filepath.Dir(it.Path))
				if !seen[cmd] {
					seen[cmd] = true
					plan = append(plan, step{text: "# the configuration and every cache and state file in it", comment: true}, step{text: cmd})
				}
			}
			continue
		}
		for _, it := range items {
			if it.Kind != kind || it.Remove == "" || seen[it.Remove] {
				continue
			}
			seen[it.Remove] = true
			if it.Risk {
				plan = append(plan, step{text: "# loses work: " + it.Note, comment: true})
			}
			plan = append(plan, step{text: it.Remove})
		}
	}
	return plan
}

func shellQuote(p string) string { return "'" + strings.ReplaceAll(p, "'", `'\''`) + "'" }

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
