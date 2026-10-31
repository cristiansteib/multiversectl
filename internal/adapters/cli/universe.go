package cli

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cristiansteib/multiversectl/internal/app"
)

// parseBranches reads repeated --branch flags. A bare name is the branch for
// every repository, which is the common case — one feature spans them all —
// and repo=branch overrides a single one.
func parseBranches(values []string) (all string, byRepo map[string]string, err error) {
	byRepo = map[string]string{}
	for _, value := range values {
		repo, branch, ok := strings.Cut(value, "=")
		switch {
		case !ok && repo != "":
			all = repo
		case repo == "" || branch == "":
			return "", nil, fmt.Errorf("--branch %q: want a branch name, or repo=branch to override one repository", value)
		default:
			byRepo[repo] = branch
		}
	}
	return all, byRepo, nil
}

func (r runner) newCmd() *cobra.Command {
	var branches, bases []string
	var note string
	cmd := &cobra.Command{
		Use: "new [number|name]",
		// Old names point at their new one in the error; they do not run.
		SuggestFor: []string{"create", "checkout", "co"},
		Short:      "Create a universe on a branch: one worktree per repo, secrets and state",
		Long: "A universe is a worktree of every repository on one branch, named after the\n" +
			"universe (or --branch). Where the branch already exists, in the clone or on origin,\n" +
			"it is checked out, so a branch to review or to carry on is `universe new <branch>`;\n" +
			"where it does not, it is started from the base. What each repository got is said.\n" +
			"Git allows a branch in one worktree only, so one already checked out in another\n" +
			"universe, or in the shared clone, is refused and its place is shown. Leave the name\n" +
			"out and the next free number is used, stepping over a number whose branch a removed\n" +
			"universe left behind. Removing a universe deletes only the branches it started.",
		Example: "  multiversectl universe new\n" +
			"  multiversectl universe new 3\n" +
			"  multiversectl universe new fix/login                   # that branch: checked out where it exists, started where not\n" +
			"  multiversectl universe new 7 --branch fix/login        # universe 7, on fix/login\n" +
			"  multiversectl universe new 3 --base release/1.2        # start from another branch\n" +
			"  multiversectl universe new 3 --branch feat/invoicing --base api=release/1.2",
		Args: cobra.MaximumNArgs(1),
	}
	cmd.Flags().StringArrayVar(&branches, "branch", nil,
		"the branch in every repo, or repo=branch for one of them (repeatable; default: the universe's name)")
	cmd.Flags().StringArrayVar(&bases, "base", nil,
		"where a branch that does not exist yet starts, or repo=branch for one of them (default: each repo's default branch)")
	cmd.Flags().StringVar(&note, "about", "", "what this universe is for, kept with it and shown by ls")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		all, byRepo, err := parseBranches(branches)
		if err != nil {
			return err
		}
		baseAll, baseBy, err := parseBranches(bases)
		if err != nil {
			return err
		}
		return r.withService(func(ctx context.Context, s *app.Service) error {
			args, err := r.newName(cmd, args, s)
			if err != nil {
				return err
			}
			st, err := s.Create(ctx, app.CreateInput{
				Name: args[0], Branch: all, Branches: byRepo, Base: baseAll, Bases: baseBy, Note: note,
			})
			if err != nil {
				return err
			}
			s.Out.OK("universe `%s` ready (%s)", st.Name, st.Project)
			return nil
		})(cmd, args)
	}
	return cmd
}

func (r runner) upCmd() *cobra.Command {
	var with []string
	var failFast, rebind bool
	cmd := &cobra.Command{
		Use:        "up <universe> [app ...]",
		SuggestFor: []string{"start"},
		Short:      "Start a universe (all apps, or only those named)",
		Example:    "  multiversectl universe up 3\n  multiversectl universe up fix/login api --with workers",
		Args:       cobra.ArbitraryArgs,
	}
	cmd.Flags().StringSliceVar(&with, "with", nil, "optional service groups to include, comma separated")
	wait := waitFlags(cmd)
	cmd.Flags().BoolVar(&rebind, "rebind-proxy", false, "reconfigure the shared proxy even though universes of other multiverses are served through it")
	cmd.Flags().BoolVar(&failFast, "fail-fast", false, "stop at the first app that does not start (default: start the others, report the failures at the end, exit non-zero)")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return r.withService(func(ctx context.Context, s *app.Service) error {
			args, err := r.universeArgs(cmd, args, s)
			if err != nil {
				return err
			}
			return s.Start(ctx, app.StartInput{Universe: args[0], Apps: args[1:], With: with, FailFast: failFast, RebindProxy: rebind, Wait: wait()})
		})(cmd, args)
	}
	return cmd
}

func (r runner) downCmd() *cobra.Command {
	return &cobra.Command{
		Use:        "down <universe> [app ...]",
		SuggestFor: []string{"stop"},
		Short:      "Stop a universe (all apps, or only those named); keeps data and checkouts",
		Example:    "  multiversectl universe down 3\n  multiversectl universe down fix/login",
		Args:       cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return r.withService(func(ctx context.Context, s *app.Service) error {
				args, err := r.universeArgs(cmd, args, s)
				if err != nil {
					return err
				}
				return s.Stop(ctx, app.StopInput{Universe: args[0], Apps: args[1:]})
			})(cmd, args)
		},
	}
}

// lsFormat is what -o asked for. The shapes and the detail are separate
// choices, so they combine: "table,wide" or "list,wide" or both shapes at
// once.
type lsFormat struct {
	table bool
	list  bool
	wide  bool
	json  bool
	less  bool
}

// parseLsFormat reads a comma-separated -o value. "wide" on its own means the
// default shape with more detail, which is what a user who types it expects.
func parseLsFormat(spec string) (lsFormat, error) {
	var f lsFormat
	for _, token := range strings.Split(spec, ",") {
		switch strings.TrimSpace(token) {
		case "", "table":
			f.table = true
		case "list", "compact":
			f.list = true
		case "wide":
			f.wide = true
		case "json":
			f.json = true
		case "less":
			f.less = true
		default:
			return f, fmt.Errorf("unknown output %q: want table, list, wide, json or less, combined with commas like table,wide", token)
		}
	}
	if !f.table && !f.list && !f.json {
		f.table = true
	}
	return f, nil
}

func (r runner) lsCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:        "ls [universe]",
		SuggestFor: []string{"list", "status"},
		Short:      "List universes, or show one",
		Example: "  multiversectl universe ls\n" +
			"  multiversectl universe ls -o wide        # every service, its state and its volumes\n" +
			"  multiversectl universe ls -o list        # one line per universe: running or not, services up, since when\n" +
			"  multiversectl universe ls -o list,wide\n" +
			"  multiversectl universe ls -o json        # compact, for an agent: multiverse paths, and per universe its state, folders, addresses by app/host, and the running container of each service\n" +
			"  multiversectl universe ls -o json,wide   # everything: services, volumes, compose names, times\n" +
			"  multiversectl universe ls -o json,less   # the least: name, state, folder and addresses by app/host\n" +
			"  multiversectl universe ls 3 -o json      # only universe 3",
		Args: cobra.MaximumNArgs(1),
	}
	cmd.Flags().StringVarP(&format, "output", "o", "table",
		"output: table, list, wide, json, or a combination such as table,wide")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		f, err := parseLsFormat(format)
		if err != nil {
			return err
		}
		return r.withService(func(ctx context.Context, s *app.Service) error {
			// The one line per universe says how many services are up and whether its
			// code is behind, so it needs the services and the freshness as well.
			detail := f.wide || f.json || f.list
			list, err := s.List(ctx, app.ListOptions{Services: detail, Freshness: detail})
			if err != nil {
				return err
			}
			if len(args) == 1 {
				kept := list[:0]
				for _, u := range list {
					if u.Name == args[0] || u.Slug == args[0] {
						kept = append(kept, u)
					}
				}
				if len(kept) == 0 {
					return fmt.Errorf("no universe %q: `multiversectl universe ls` lists them", args[0])
				}
				list = kept
			}
			if f.json {
				if f.wide {
					return writeJSON(r.out.w, listView(s.Context(), list))
				}
				if f.less {
					return writeCompactJSON(r.out.w, lessView(s.Context(), list))
				}
				return writeCompactJSON(r.out.w, briefView(s.Context(), list))
			}
			// The header is for a person reading the table; -o list is for piping.
			if f.table {
				projectHeader(r.out, s.Context(), f.wide)
			}
			if len(list) == 0 {
				s.Out.Info("no universes yet — create one with: `multiversectl universe new 1`")
				return nil
			}
			if f.table {
				lsTable(r.out, list, f.wide)
				if f.wide { // the services are only known when they were asked for
					for _, note := range partialNotes(list) {
						r.out.Warn("%s", note)
					}
				}
			}
			if f.list {
				if f.table {
					r.out.Plain("")
				}
				lsList(r.out, list, f.wide)
				if !f.table || !f.wide { // the wide table has said them already
					for _, note := range partialNotes(list) {
						r.out.Warn("%s", note)
					}
				}
				for _, u := range list {
					if behind := behindOrigin(u); behind != "" {
						r.out.Warn("universe %s runs older code than origin has (%s commits behind): `multiversectl universe sync %s`", u.Name, behind, u.Name)
					}
				}
			}
			return nil
		})(cmd, args)
	}
	return cmd
}

// lsList is one line per universe, for a quick look at what exists or for
// piping into something else.
func lsList(o *Output, list []app.Summary, wide bool) {
	if !wide {
		rows := make([][]Cell, 0, len(list))
		for _, u := range list {
			state, style, up := standing(u)
			started := "never"
			if !u.LastStarted.IsZero() {
				started = age(u.LastStarted)
			}
			rows = append(rows, []Cell{{Text: u.Name}, {Text: state, Style: style}, {Text: up}, {Text: started},
				{Text: oneBranch(u.Branches)}, {Text: strings.TrimSpace(u.PR + " " + u.Note), Style: dim}})
		}
		o.Cells([]string{"UNIVERSE", "STATE", "UP", "LAST STARTED", "BRANCH", "ABOUT"}, rows)
		return
	}
	headers := []string{"UNIVERSE", "COMPOSE", "DIR", "CREATED", "RUNNING", "SERVICES", "VOLUMES", "HOSTS", "BRANCHES"}
	rows := make([][]string, 0, len(list))
	for _, u := range list {
		up, expected, volumes := totals(u)
		rows = append(rows, []string{u.Name, u.Project, shortPath(u.Dir), age(u.Created), fmt.Sprint(u.Running),
			fmt.Sprintf("%d/%d", up, expected), fmt.Sprint(volumes), strings.Join(sortedValues(u.Hosts), ","), joinPairs(u.Branches)})
	}
	o.Table(headers, rows)
}

// behindOrigin names the checkouts of a universe that origin has moved past, as
// "api 2, web 1", or is empty when none has.
func behindOrigin(u app.Summary) string {
	var behind []string
	for _, c := range u.Checkouts {
		if c.Freshness != nil && c.Freshness.Behind > 0 {
			behind = append(behind, fmt.Sprintf("%s %d", c.Repo, c.Freshness.Behind))
		}
	}
	return strings.Join(behind, ", ")
}

// totals add up a universe's services and volumes across its apps.
func totals(u app.Summary) (up, expected, volumes int) {
	for _, a := range u.Apps {
		for _, svc := range a.Services {
			if svc.Up() {
				up++
			}
		}
		expected += a.Expected
		volumes += len(a.Volumes)
	}
	return up, expected, volumes
}

// lsTable is one block per universe. The default answers what somebody who
// typed `ls` is nearly always after — which app, on which branch, at which
// address — and leaves the rest to `-o wide`: the service behind each address,
// what is up and what is not, what publishes a port, and every worktree,
// compose project and volume on disk.
//
// The forward column appears only for a universe that has one, since most do
// not and an empty column is a column to read past.
func lsTable(o *Output, list []app.Summary, wide bool) {
	for i, u := range list {
		if i > 0 {
			o.Plain("")
		}
		if wide {
			o.Block(universeTitle(o, u),
				[]string{"APP", "BRANCH", "SERVICE", "STATE", "PUBLISHED", "HOST", "URL"}, universeWideRows(u))
			o.Plain("%s", o.Dim(metaLine(u)))
			o.Plain("")
			o.Block(o.Dim("on disk"),
				[]string{"REPO", "WORKTREE", "CODE", "COMPOSE", "VOLUMES"}, diskRows(u))
			continue
		}
		headers := []string{"APP", "BRANCH", "URL"}
		if anyForward(u) {
			headers = append(headers, "FORWARDED")
		}
		o.Block(universeTitle(o, u), headers, universeRows(u))
	}
}

// forwardSuffix marks the second address of a host: the one on a host port, for
// something that cannot use a hostname. It is a sibling of the address it
// belongs to rather than a row of its own, so the table pairs them again.
const forwardSuffix = ":forward"

func anyForward(u app.Summary) bool {
	for _, a := range u.Apps {
		for _, h := range a.URLs {
			if strings.HasSuffix(h.Host, forwardSuffix) {
				return true
			}
		}
	}
	return false
}

// diskRows is what a universe owns on disk: one row per repository, with the
// worktree its code is in and, for the ones that run something, the runtime
// multiverse and the volumes under it. Every name carrying the universe's own
// prefix is the evidence that this universe shares nothing with another.
func diskRows(u app.Summary) [][]Cell {
	byRepo := map[string]app.AppSummary{}
	for _, a := range u.Apps {
		byRepo[a.Repo] = a
	}
	rows := make([][]Cell, 0, len(u.Checkouts))
	for _, c := range u.Checkouts {
		row := []Cell{
			{Text: c.Repo, Style: bold},
			{Text: shortPath(c.Path), Style: dim},
			codeCell(c.Freshness),
		}
		a, runs := byRepo[c.Repo]
		switch {
		case !runs:
			row = append(row, Cell{Text: "-", Style: dim}, Cell{Text: "-", Style: dim})
		case len(a.Volumes) == 0:
			row = append(row, Cell{Text: a.Project}, Cell{Text: "none", Style: dim})
		default:
			row = append(row, Cell{Text: a.Project}, Cell{Text: strings.Join(a.Volumes, ", ")})
		}
		rows = append(rows, row)
	}
	return rows
}

// codeCell says whether a checkout has what the branch it follows has.
func codeCell(f *app.Freshness) Cell {
	switch {
	case f == nil || f.Follows == "":
		return Cell{Text: "-", Style: dim}
	case f.Behind > 0 && f.Dirty:
		return Cell{Text: fmt.Sprintf("%d behind origin/%s, edited", f.Behind, f.Follows), Style: yellow}
	case f.Behind > 0:
		return Cell{Text: fmt.Sprintf("%d behind origin/%s", f.Behind, f.Follows), Style: yellow}
	case f.Dirty:
		return Cell{Text: "edited", Style: dim}
	}
	return Cell{Text: "current", Style: dim}
}

// universeTitle heads a universe's block: which universe, why it exists if
// anyone said, its compose project, whether anything is up, and the domain its
// hostnames sit under.
func universeTitle(o *Output, u app.Summary) string {
	state := o.Green(fmt.Sprintf("%d running", u.Running))
	if u.Running == 0 {
		state = o.Yellow("stopped")
	}
	parts := []string{o.paint(bold+cyan, "universe "+u.Name)}
	if u.PR != "" {
		parts = append(parts, o.Accent(u.PR))
	}
	if u.Note != "" {
		parts = append(parts, o.Yellow(truncate(u.Note, 48)))
	}
	parts = append(parts, o.Dim(u.Project), state, o.Dim(u.Domain))
	return strings.Join(parts, o.Dim("  ·  "))
}

// metaLine is what nothing else in the table says: how old the universe is,
// when it last ran, and what made it. Only the wide shape has room for it.
func metaLine(u app.Summary) string {
	parts := []string{"created " + age(u.Created)}
	if u.LastStarted.IsZero() {
		parts = append(parts, "never started")
	} else {
		parts = append(parts, "last started "+age(u.LastStarted))
	}
	return strings.Join(parts, "  ·  ")
}

// age reads as a glance rather than a timestamp: what matters about a universe
// is whether it is from this morning or from last month.
func age(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// universeRows is one row per address, each column in its own colour so a row
// can be read across at a glance.
//
// The app's name is repeated on every row of its group rather than left
// blank: a blank cell says which app a row belongs to by absence, which is
// the weakest signal there is, and it drops out of `grep`. Bold on the row
// the group starts at and dim after it marks the seam between groups without
// spending a colour.
func universeRows(u app.Summary) [][]Cell {
	forwards := anyForward(u)
	rows := make([][]Cell, 0, len(u.Apps)+1)
	for _, a := range u.Apps {
		head := []Cell{
			{Text: a.Key, Style: bold},
			{Text: a.Branch, Style: yellow},
		}
		// Keyed by the address they belong to, so each lands beside it.
		forward := map[string]string{}
		for _, h := range a.URLs {
			if name, ok := strings.CutSuffix(h.Host, forwardSuffix); ok {
				forward[name] = h.URL
			}
		}
		shown := 0
		for _, h := range a.URLs {
			if strings.HasSuffix(h.Host, forwardSuffix) {
				continue
			}
			// The URL is what gets copied, so it stays in the terminal's own
			// foreground rather than a colour that may be dim.
			row := append(head, Cell{Text: h.URL})
			if forwards {
				row = append(row, Cell{Text: forward[h.Host], Style: cyan})
			}
			rows = append(rows, row)
			head, shown = continuation(a.Key, 1), shown+1
		}
		if shown == 0 {
			row := append(head, Cell{Text: "no address", Style: dim})
			if forwards {
				row = append(row, Cell{Text: "", Style: dim})
			}
			rows = append(rows, row)
		}
	}
	return append(rows, checkoutRows(u, false, forwards)...)
}

// continuation is the head of a row that belongs to the group above it: the
// app named again, quietly, and the columns describing its checkout left
// blank, since they were stated once and have not changed.
func continuation(appKey string, blanks int) []Cell {
	head := append([]Cell{{Text: appKey, Style: dim}}, make([]Cell, blanks)...)
	return head
}

// universeWideRows is one row per service of every app, so the reader can see
// what is on, what is off and what nothing has ever started — and that no
// service publishes a host port it was not allowed to.
func universeWideRows(u app.Summary) [][]Cell {
	rows := make([][]Cell, 0, len(u.Apps)*4)
	for _, a := range u.Apps {
		byService := map[string][]app.HostURL{}
		for _, h := range a.URLs {
			byService[h.Service] = append(byService[h.Service], h)
		}
		if len(rows) > 0 {
			// Groups here run to a dozen services, so they are set apart by a
			// blank line as well as by weight.
			rows = append(rows, nil)
		}
		head := []Cell{
			{Text: a.Key, Style: bold},
			{Text: a.Branch, Style: yellow},
		}
		if len(a.Services) == 0 {
			rows = append(rows, append(head,
				Cell{Text: "-", Style: dim},
				Cell{Text: "no services", Style: dim},
				Cell{Text: "-", Style: dim},
				Cell{Text: "-", Style: dim},
				Cell{Text: "-", Style: dim},
			))
			continue
		}
		for _, svc := range a.Services {
			row := []Cell{head[0], head[1],
				{Text: svc.Name},
				{Text: stateText(svc), Style: stateStyle(svc.State)},
				{Text: publishedText(svc), Style: publishedStyle(svc)},
			}
			addrs := byService[svc.Name]
			if len(addrs) == 0 {
				rows = append(rows, append(row, Cell{Text: "-", Style: dim}, Cell{Text: "-", Style: dim}))
			}
			for i, h := range addrs {
				if i > 0 {
					row = append(continuation(a.Key, 1), make([]Cell, 3)...)
				}
				rows = append(rows, append(row, Cell{Text: h.Host, Style: cyan}, Cell{Text: h.URL}))
			}
			head = continuation(a.Key, 1)
		}
	}
	if len(rows) > 0 && len(u.Checkouts) > len(u.Apps) {
		rows = append(rows, nil)
	}
	return append(rows, checkoutRows(u, true, false)...)
}

// checkoutRows give every repository without an app a row of its own. A
// repository with no services is still checked out for editing, and a table
// that silently drops it reads as if the universe did not have it.
func checkoutRows(u app.Summary, wide, forwards bool) [][]Cell {
	var rows [][]Cell
	for _, c := range u.Checkouts {
		if c.App != "" {
			continue
		}
		row := []Cell{
			{Text: c.Repo, Style: dim},
			{Text: c.Branch, Style: yellow},
		}
		// Both shapes end with the reason there is nothing to show, in the
		// column that would hold the URL; the columns before it are dashed.
		if wide {
			row = append(row, dashes(4)...)
		}
		row = append(row, Cell{Text: "checkout only, no services", Style: dim})
		if !wide && forwards {
			row = append(row, Cell{})
		}
		rows = append(rows, row)
	}
	return rows
}

// shortPath writes a path under the home directory with a tilde, which is
// both shorter in the table and what the reader would type.
func shortPath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || path == "" {
		return path
	}
	if rest, ok := strings.CutPrefix(path, home+string(os.PathSeparator)); ok {
		return "~" + string(os.PathSeparator) + rest
	}
	return path
}

func dashes(n int) []Cell {
	row := make([]Cell, n)
	for i := range row {
		row[i] = Cell{Text: "-", Style: dim}
	}
	return row
}

// stateText keeps the state to one scannable word, except where the detail is
// the actionable part: the flag that would start an optional service.
func stateText(svc app.ServiceStatus) string {
	if svc.State == app.StateOptional {
		return "off  " + svc.Detail
	}
	if svc.State == app.StateOff && svc.Detail == "on demand" {
		return "off  on demand"
	}
	return string(svc.State)
}

func stateStyle(state app.ServiceState) string {
	switch state {
	case app.StateRunning:
		return green
	case app.StateStopped:
		return red
	case app.StateNotUp:
		return yellow
	default:
		return dim
	}
}

// publishedText shows a service's host ports. Empty is the healthy case — a
// universe is reached through the proxy — so it is spelled out rather than
// left blank.
func publishedText(svc app.ServiceStatus) string {
	if svc.State != app.StateRunning {
		return "-"
	}
	if svc.Published == "" {
		return "none"
	}
	return svc.Published
}

// publishedStyle marks a published port: it is the one thing here that stops
// a second universe from starting.
func publishedStyle(svc app.ServiceStatus) string {
	if svc.State == app.StateRunning && svc.Published != "" {
		return yellow
	}
	return dim
}

// runningStyle dims an app with nothing up, so the eye lands on the ones that
// are serving.
func runningStyle(n int) string {
	if n == 0 {
		return dim
	}
	return green
}

func (r runner) urlsCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "urls <universe>",
		Short:   "Print a universe's URLs, one per line",
		Example: "  multiversectl universe urls 3\n  multiversectl universe urls fix/login",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return r.withService(func(ctx context.Context, s *app.Service) error {
				args, err := r.universeArgs(cmd, args, s)
				if err != nil {
					return err
				}
				st, err := s.Resolve(args[0])
				if err != nil {
					return err
				}
				for _, appKey := range sortedKeys(s.Manifest.Apps) {
					for _, route := range s.Routes(st, appKey) {
						if route.Hostname == "" {
							continue
						}
						s.Out.Plain("%s", s.Manifest.Origin(route.Entrypoint, route.Hostname))
					}
				}
				return nil
			})(cmd, args)
		},
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedValues(m map[string]string) []string {
	vals := make([]string, 0, len(m))
	for _, k := range sortedKeys(m) {
		vals = append(vals, m[k])
	}
	return vals
}

func joinPairs(m map[string]string) string {
	pairs := make([]string, 0, len(m))
	for _, k := range sortedKeys(m) {
		pairs = append(pairs, k+"="+m[k])
	}
	return strings.Join(pairs, " ")
}

// universeCmd holds what a person does with universes, so the top level stays
// for the tool itself and for the catalog.
func (r runner) universeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "universe",
		Aliases: []string{"u"},
		Short:   "Create, start, inspect and remove universes",
		Example: "  multiversectl universe new 3\n  multiversectl u up 3\n  multiversectl u ls",
		Args:    cobra.NoArgs,
		RunE:    func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(r.newCmd(), r.upCmd(), r.universeDoctorCmd(), r.downCmd(), r.syncCmd(), r.aboutCmd(), r.notesCmd(), r.rmCmd(), r.lsCmd(), r.urlsCmd(),
		r.envCmd(), r.runCmd(), r.execCmd())
	return cmd
}

// partialNotes says, for each universe that is partly up, which services are
// not running and why, so "partial" is never left to be worked out from the
// containers. What is off on purpose is not among them; it is named after,
// so it is clear it was not counted.
func partialNotes(list []app.Summary) []string {
	var notes []string
	for _, u := range list {
		if state, _, _ := standing(u); state != "partial" {
			continue
		}
		var down []string
		for _, sf := range u.Shortfalls() {
			item := sf.App + "/" + sf.Service + " " + sf.Why
			if sf.Detail != "" && sf.Why != "not started" {
				item += " (" + sf.Detail + ")"
			}
			down = append(down, item)
		}
		note := fmt.Sprintf("universe %s is partial: %s", u.Name, strings.Join(down, ", "))
		if skipped := u.Skipped(); len(skipped) > 0 {
			var parts []string
			for _, key := range sortedKeys(skipped) {
				parts = append(parts, key+" "+skipped[key])
			}
			note += "; not counted: " + strings.Join(parts, ", ")
		}
		notes = append(notes, note)
	}
	return notes
}

// standing reduces a universe to the one word a glance wants. Optional services
// that are off on purpose are not counted against it.
func standing(u app.Summary) (state, style, up string) {
	n, expected, _ := totals(u)
	up = fmt.Sprint(u.Running)
	if expected > 0 {
		up = fmt.Sprintf("%d/%d", n, expected)
	}
	switch {
	case n > 0 && n >= expected:
		return "running", green, up
	case n > 0:
		return "partial", yellow, up
	case u.LastStarted.IsZero() && u.Running == 0:
		return "created", dim, up
	}
	return "stopped", dim, up
}

func (r runner) syncCmd() *cobra.Command {
	var all, merge bool
	cmd := &cobra.Command{
		Use:   "sync [universe]",
		Short: "Bring a universe's code up to date with the branches it follows",
		Long: "A universe's checkouts keep the commit they were made from, so a fix that reached\n" +
			"main since is not in them. sync fetches and moves each checkout forward when that\n" +
			"loses nothing: a checkout with uncommitted changes is never touched, and one with\n" +
			"commits of its own is left alone unless --merge brings the branch in with a merge.\n" +
			"Restart the universe afterwards to run the new code.",
		Example: "  multiversectl universe sync 1\n  multiversectl universe sync --all\n  multiversectl universe sync fix/login --merge",
		Args:    cobra.MaximumNArgs(1),
	}
	cmd.Flags().BoolVar(&all, "all", false, "every universe")
	cmd.Flags().BoolVar(&merge, "merge", false, "merge the branch into a checkout that has commits of its own")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return r.withService(func(ctx context.Context, s *app.Service) error {
			var names []string
			if all {
				states, err := s.Store.List()
				if err != nil {
					return err
				}
				for _, st := range states {
					names = append(names, st.Name)
				}
			} else {
				args, err := r.universeArgs(cmd, args, s)
				if err != nil {
					return err
				}
				names = args
			}
			for _, name := range names {
				res, err := s.Sync(ctx, name, app.SyncOptions{Merge: merge})
				if err != nil {
					return err
				}
				for _, x := range res {
					switch {
					case x.Moved > 0:
						r.out.OK("%s/%s: %s (%d commits)", name, x.Repo, x.Detail, x.Moved)
					case strings.HasPrefix(x.Detail, "up to date"):
						r.out.Plain("%s", r.out.Dim(name+"/"+x.Repo+": "+x.Detail))
					default:
						r.out.Warn("%s/%s: %s", name, x.Repo, x.Detail)
					}
				}
			}
			return nil
		})(cmd, args)
	}
	return cmd
}

// aboutCmd is everything about one universe: what it is for and its PR, which
// it also sets, the manifest it runs on and the hooks that manifest ties to it.
// What it says is all read from disk, so it is quick to ask.
func (r runner) aboutCmd() *cobra.Command {
	var pr string
	var clear, diff, yaml, files bool
	cmd := &cobra.Command{
		Use:        "about <universe> [text]",
		SuggestFor: []string{"note", "config", "render", "hooks"},
		Short:      "What a universe is for, its PR, the manifest it runs on and its hooks; and set what it is for",
		Long: "With only the universe it says what it is for and its pull request, which manifest it\n" +
			"uses (whether that loads, and whether it was edited since the universe started) and\n" +
			"the hooks the manifest ties to its life. With text, or --pr, it sets what the universe\n" +
			"is for, kept with it and shown by ls, -o json and the web page, so a person or another\n" +
			"agent finding it later knows what it is doing; --clear empties both. `universe notes`\n" +
			"is for the messages left in it as the work goes on.\n\n" +
			"--diff shows how the manifest changed since the universe started, --yaml prints it\n" +
			"as it is now, and --files the files its `render` entries make, as `universe up` would\n" +
			"write them, which is how a template is tried.",
		Example: "  multiversectl universe about 3\n" +
			"  multiversectl universe about 3 \"fixing the login redirect\"\n" +
			"  multiversectl universe about 3 --pr https://github.com/acme/api/pull/42\n" +
			"  multiversectl universe about 3 --diff\n" +
			"  multiversectl universe about 3 --files",
		Args: cobra.MinimumNArgs(1),
	}
	cmd.Flags().StringVar(&pr, "pr", "", "the pull or merge request the work is going to (a URL)")
	cmd.Flags().BoolVar(&clear, "clear", false, "remove what it is for and the PR")
	cmd.Flags().BoolVar(&diff, "diff", false, "show how the manifest changed since the universe started")
	cmd.Flags().BoolVar(&yaml, "yaml", false, "print the manifest as it is now")
	cmd.Flags().BoolVar(&files, "files", false, "print the files the manifest renders for it, without writing them")
	cmd.MarkFlagsMutuallyExclusive("diff", "yaml", "files", "clear", "pr")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return r.withService(func(ctx context.Context, s *app.Service) error {
			name, text := args[0], strings.Join(args[1:], " ")
			switch {
			case files:
				return s.Render(ctx, app.RenderInput{Universe: name, Print: true})
			case diff, yaml:
				rep, err := s.UniverseConfig(name)
				if err != nil {
					return err
				}
				if yaml {
					r.out.Plain("%s", strings.TrimRight(string(rep.Current), "\n"))
					return nil
				}
				return r.manifestDiff(rep)
			}
			var in app.NoteInput
			switch {
			case clear:
				empty := ""
				in = app.NoteInput{Text: &empty, PR: &empty}
			default:
				if text != "" {
					in.Text = &text
				}
				if cmd.Flags().Changed("pr") {
					in.PR = &pr
				}
			}
			if in.Text == nil && in.PR == nil {
				return r.about(s, name)
			}
			st, err := s.SetNote(name, in)
			if err != nil {
				return err
			}
			if st.PR == "" && st.Note == "" {
				r.out.OK("universe %s: what it is for, and its PR, cleared", st.Name)
				return nil
			}
			r.out.OK("universe %s: %s", st.Name, strings.TrimSpace(st.PR+" "+st.Note))
			return nil
		})(cmd, args)
	}
	return cmd
}

// about prints what is known about a universe without changing it: what it is
// for, the manifest it runs on, and its hooks.
func (r runner) about(s *app.Service, name string) error {
	t, p, err := s.Note(name)
	if err != nil {
		return err
	}
	if t == "" && p == "" {
		r.out.Info("nothing said about %s yet: `multiversectl universe about %s \"what it is for\" --pr <url>`", name, name)
	}
	if t != "" {
		r.out.Plain("about      %s", t)
	}
	if p != "" {
		r.out.Plain("pr         %s", p)
	}
	rep, err := s.UniverseConfig(name)
	if err != nil {
		return err
	}
	r.out.Plain("manifest   %s", rep.Path)
	r.out.Plain("sha256     %s", rep.SHA256[:16])
	for _, f := range rep.RepoFiles {
		from := "this universe's checkout"
		if f.FromClone {
			from = "the shared clone"
		}
		r.out.Plain("repo file  %s: %s (%s)", f.Repo, f.Path, from)
	}
	if len(rep.Problems) == 0 {
		r.out.OK("it loads without complaint")
	} else {
		r.out.Warn("loading it would fail:")
		for _, p := range rep.Problems {
			r.out.Plain("  - %s", p)
		}
	}
	switch rec := rep.StartedWith; {
	case rec == nil:
		r.out.Info("no record of what it was started with (it was started before that was kept, or not at all)")
	case rep.Changed:
		r.out.Warn("started %s with %s (build %s); the manifest has been edited since: `--diff` shows what changed", rec.At.Local().Format("2006-01-02 15:04"), rec.SHA256[:16], firstNonEmpty(rec.Version, "unknown"))
		if len(rep.SnapshotProblems) > 0 {
			r.out.Info("what it was started with would not load either: %s", strings.Join(rep.SnapshotProblems, "; "))
		}
	default:
		r.out.OK("started %s with this manifest, unchanged since", rec.At.Local().Format("2006-01-02 15:04"))
	}

	hooks, err := s.Hooks(name)
	if err != nil {
		return err
	}
	r.out.Plain("")
	if len(hooks) == 0 {
		r.out.Info("the manifest ties no commands to this universe's life")
		return nil
	}
	rows := make([][]string, len(hooks))
	for i, h := range hooks {
		state := string(h.OnFailure)
		switch {
		case h.Done:
			state += ", done"
		case h.Once:
			state += ", once"
		}
		rows[i] = []string{string(h.Event), cmp.Or(h.App, "(multiverse)"), h.Where, cmp.Or(h.Name, h.Command), state}
	}
	r.out.Table([]string{"HOOK", "APP", "RUNS", "COMMAND", "ON FAILURE"}, rows)
	r.out.Info("run an event's hooks again with `multiversectl universe run %s --hook <event> [app...]`", name)
	return nil
}

// manifestDiff shows how the manifest changed since the universe started.
func (r runner) manifestDiff(rep app.ConfigReport) error {
	switch {
	case rep.StartedWith == nil:
		return errors.New("this universe has no record of the manifest it was started with (it was started before that was kept); start it again to record one")
	case rep.Snapshot == nil:
		return errors.New("the manifest it was started with was not kept, only its fingerprint")
	case !rep.Changed:
		r.out.OK("the manifest is the one it was started with")
		return nil
	}
	for _, l := range app.DiffLines(string(rep.Snapshot), string(rep.Current)) {
		line := string(l.Op) + " " + l.Text
		switch l.Op {
		case '+':
			line = r.out.Green(line)
		case '-':
			line = r.out.Red(line)
		default:
			line = r.out.Dim(line)
		}
		r.out.Plain("%s", line)
	}
	return nil
}
