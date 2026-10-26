package cli

import (
	"context"
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
		Use:     "new [number|name]",
		Aliases: []string{"create"},
		Short:   "Create a universe on branches of its own: one worktree per repo, secrets and state",
		Long: "A new universe starts a branch of its own in every repository, from the base\n" +
			"branch, so it can never land on anyone's work: if the branch already exists, in\n" +
			"the clone or on origin, nothing is created and every clash is listed. Leave the\n" +
			"name out and the next free number is used, stepping over a number whose branch a\n" +
			"removed universe left behind. To work on a branch that already exists, use\n" +
			"`multiversectl universe checkout`.",
		Example: "  multiversectl universe new\n" +
			"  multiversectl universe new 3\n" +
			"  multiversectl universe new fix/login                   # a new branch of that name\n" +
			"  multiversectl universe new 3 --base release/1.2        # start from another branch\n" +
			"  multiversectl universe new 3 --branch feat/invoicing --base api=release/1.2",
		Args: cobra.MaximumNArgs(1),
	}
	cmd.Flags().StringArrayVar(&branches, "branch", nil,
		"name of the new branch in every repo, or repo=branch for one of them (repeatable; it must not exist yet)")
	cmd.Flags().StringArrayVar(&bases, "base", nil,
		"branch the new ones start from, or repo=branch for one of them (default: each repo's default branch)")
	cmd.Flags().StringVar(&note, "note", "", "why this universe exists, kept with it and shown by ls")
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

func (r runner) checkoutCmd() *cobra.Command {
	var name, note string
	var perRepo []string
	var onlyExisting bool
	cmd := &cobra.Command{
		Use:     "checkout [branch]",
		Aliases: []string{"co"},
		Short:   "Create a universe on a branch that already exists, to work on or review it",
		Long: "Checks the branch out in every repository that has it, locally or on origin, and\n" +
			"names the universe after it (--name for another). A repository that lacks it gets\n" +
			"a new branch of that name from its base, or with --only-existing is left on its\n" +
			"base, detached. Git allows a branch in one worktree only, so one that is already\n" +
			"checked out in another universe, or in the shared clone, is refused and its place\n" +
			"is shown. A branch that does not exist anywhere is refused too, so a typo does not\n" +
			"become one; `multiversectl universe new` starts a new branch. Removing a checkout\n" +
			"universe never deletes the branch it checked out.",
		Example: "  multiversectl universe checkout fix/login\n" +
			"  multiversectl universe checkout fix/login --name 7\n" +
			"  multiversectl universe checkout fix/login --repo web=feat/ui --only-existing\n" +
			"  multiversectl universe checkout                         # pick from the branches there are",
		Args: cobra.MaximumNArgs(1),
	}
	cmd.Flags().StringVar(&name, "name", "", "name the universe (default: the branch)")
	cmd.Flags().StringArrayVar(&perRepo, "repo", nil, "a different branch for one repository, as repo=branch (repeatable)")
	cmd.Flags().BoolVar(&onlyExisting, "only-existing", false, "leave repositories without the branch on their base branch, detached, instead of starting one")
	cmd.Flags().StringVar(&note, "note", "", "why this universe exists, kept with it and shown by ls")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		for _, p := range perRepo {
			if !strings.Contains(p, "=") {
				return fmt.Errorf("--repo %q: want repo=branch", p)
			}
		}
		_, byRepo, err := parseBranches(perRepo)
		if err != nil {
			return err
		}
		return r.withService(func(ctx context.Context, s *app.Service) error {
			branch := ""
			if len(args) > 0 || len(byRepo) == 0 {
				if branch, err = r.branchArg(cmd, args, s); err != nil {
					return err
				}
			}
			st, err := s.Create(ctx, app.CreateInput{
				Name: name, Checkout: true, Branch: branch, Branches: byRepo, Note: note, OnlyExisting: onlyExisting,
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
	cmd := &cobra.Command{
		Use:     "up <universe> [app ...]",
		Aliases: []string{"start"},
		Short:   "Start a universe (all apps, or only those named)",
		Example: "  multiversectl universe up 3\n  multiversectl universe up fix/login api --with workers",
		Args:    cobra.ArbitraryArgs,
	}
	cmd.Flags().StringSliceVar(&with, "with", nil, "optional service groups to include, comma separated")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return r.withService(func(ctx context.Context, s *app.Service) error {
			args, err := r.universeArgs(cmd, args, s)
			if err != nil {
				return err
			}
			return s.Start(ctx, app.StartInput{Universe: args[0], Apps: args[1:], With: with})
		})(cmd, args)
	}
	return cmd
}

func (r runner) downCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "down <universe> [app ...]",
		Aliases: []string{"stop"},
		Short:   "Stop a universe (all apps, or only those named); keeps data and checkouts",
		Example: "  multiversectl universe down 3\n  multiversectl universe down fix/login",
		Args:    cobra.ArbitraryArgs,
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
		Use:     "ls [universe]",
		Aliases: []string{"list"},
		Short:   "List universes, or show one",
		Example: "  multiversectl universe ls\n" +
			"  multiversectl universe ls -o wide        # every service, its state and its volumes\n" +
			"  multiversectl universe ls -o list        # one line per universe\n" +
			"  multiversectl universe ls -o list,wide\n" +
			"  multiversectl universe ls -o json        # compact, for an agent: project paths, and per universe its state, folders and URLs\n" +
			"  multiversectl universe ls -o json,wide   # everything: services, volumes, compose names, times\n" +
			"  multiversectl universe ls -o json,less   # the least: name, state, folder and URLs\n" +
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
			list, err := s.List(ctx, app.ListOptions{Services: f.wide || f.json, Freshness: f.wide || f.json})
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
			}
			if f.list {
				if f.table {
					r.out.Plain("")
				}
				lsList(r.out, list, f.wide)
			}
			return nil
		})(cmd, args)
	}
	return cmd
}

// lsList is one line per universe, for a quick look at what exists or for
// piping into something else.
func lsList(o *Output, list []app.Summary, wide bool) {
	headers := []string{"UNIVERSE", "COMPOSE", "RUNNING", "HOSTS", "BRANCHES"}
	if wide {
		headers = []string{"UNIVERSE", "COMPOSE", "DIR", "CREATED", "RUNNING", "SERVICES", "VOLUMES", "HOSTS", "BRANCHES"}
	}
	rows := make([][]string, 0, len(list))
	for _, u := range list {
		row := []string{u.Name, u.Project}
		if wide {
			row = append(row, shortPath(u.Dir), age(u.Created))
		}
		row = append(row, fmt.Sprint(u.Running))
		if wide {
			up, expected, volumes := totals(u)
			row = append(row, fmt.Sprintf("%d/%d", up, expected), fmt.Sprint(volumes))
		}
		row = append(row, strings.Join(sortedValues(u.Hosts), ","), joinPairs(u.Branches))
		rows = append(rows, row)
	}
	o.Table(headers, rows)
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

// lsTable is the default: one block per universe, with a row per address so
// each app's branch, service and URL are readable rather than crammed into a
// single cell. Wide trades the addresses for every service of every app, and
// names the volumes underneath.
func lsTable(o *Output, list []app.Summary, wide bool) {
	headers := []string{"APP", "BRANCH", "RUNNING", "SERVICE", "HOST", "URL", "PATH"}
	rows := universeRows
	if wide {
		headers = []string{"APP", "BRANCH", "SERVICE", "STATE", "PUBLISHED", "HOST", "URL"}
		rows = universeWideRows
	}
	for i, u := range list {
		if i > 0 {
			o.Plain("")
		}
		o.Block(universeTitle(o, u), headers, rows(u))
		if wide {
			o.Plain("%s", o.Dim(metaLine(u)))
			o.Plain("")
			o.Block(o.Dim("on disk"),
				[]string{"REPO", "WORKTREE", "CODE", "COMPOSE", "VOLUMES"}, diskRows(u))
		}
	}
}

// diskRows is what a universe owns on disk: one row per repository, with the
// worktree its code is in and, for the ones that run something, the runtime
// project and the volumes under it. Every name carrying the universe's own
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
	rows := make([][]Cell, 0, len(u.Apps)+1)
	for _, a := range u.Apps {
		head := []Cell{
			{Text: a.Key, Style: bold},
			{Text: a.Branch, Style: yellow},
			{Text: fmt.Sprint(a.Running), Style: runningStyle(a.Running)},
		}
		path := Cell{Text: shortPath(a.Path), Style: dim}
		if len(a.URLs) == 0 {
			rows = append(rows, append(head, dashes(3)[0], dashes(3)[1], dashes(3)[2], path))
			continue
		}
		for _, h := range a.URLs {
			rows = append(rows, append(head,
				Cell{Text: h.Service, Style: dim},
				Cell{Text: h.Host, Style: cyan},
				// The URL is what gets copied, so it stays in the terminal's
				// own foreground rather than a colour that may be dim.
				Cell{Text: h.URL},
				path,
			))
			head, path = continuation(a.Key, 2), Cell{}
		}
	}
	return append(rows, checkoutRows(u, false)...)
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
	return append(rows, checkoutRows(u, true)...)
}

// checkoutRows give every repository without an app a row of its own. A
// repository with no services is still checked out for editing, and a table
// that silently drops it reads as if the universe did not have it.
func checkoutRows(u app.Summary, wide bool) [][]Cell {
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
		} else {
			row = append(row, dashes(3)...)
		}
		row = append(row, Cell{Text: "checkout only, no services", Style: dim})
		if !wide {
			// Wide names every worktree in its own block instead.
			row = append(row, Cell{Text: shortPath(c.Path), Style: dim})
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
						url := "http://" + route.Hostname
						if port := s.Manifest.Proxy.Entrypoints[route.Entrypoint]; port != 0 && port != 80 {
							url += fmt.Sprintf(":%d", port)
						}
						s.Out.Plain("%s", url)
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
	cmd.AddCommand(r.newCmd(), r.checkoutCmd(), r.upCmd(), r.doctorCmd(), r.proxyCmd(), r.pruneCmd(), r.downCmd(), r.syncCmd(), r.noteCmd(), r.rmCmd(), r.lsCmd(), r.universeStatusCmd(), r.urlsCmd(),
		r.envCmd(), r.runCmd(), r.execCmd())
	return cmd
}

// universeStatusCmd is the glance: one line per universe saying whether it is
// running, where ls says what each app is doing.
func (r runner) universeStatusCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:     "status",
		Short:   "One line per universe: whether it is running, how many services are up, and since when",
		Example: "  multiversectl universe status\n  multiversectl universe status -o json   # compact, for an agent",
		Args:    cobra.NoArgs,
	}
	cmd.Flags().StringVarP(&format, "output", "o", "table", "output: table, json or json,less")
	cmd.RunE = r.withService(func(ctx context.Context, s *app.Service) error {
		f, err := parseLsFormat(format)
		if err != nil || f.wide || f.list {
			return fmt.Errorf("unknown output %q: want table, json or json,less", format)
		}
		list, err := s.List(ctx, app.ListOptions{Services: true, Freshness: true})
		if err != nil {
			return err
		}
		if f.json {
			if f.less {
				return writeCompactJSON(r.out.w, lessView(s.Context(), list))
			}
			return writeCompactJSON(r.out.w, briefView(s.Context(), list))
		}
		projectHeader(r.out, s.Context(), false)
		if len(list) == 0 {
			s.Out.Info("no universes yet — create one with: `multiversectl universe new 1`")
			return nil
		}
		rows := make([][]Cell, 0, len(list))
		for _, u := range list {
			state, style, up := standing(u)
			started := "never"
			if !u.LastStarted.IsZero() {
				started = age(u.LastStarted)
			}
			rows = append(rows, []Cell{{Text: u.Name}, {Text: state, Style: style}, {Text: up}, {Text: started},
				{Text: joinPairs(u.Branches)}, {Text: strings.TrimSpace(u.PR + " " + u.Note), Style: dim}})
		}
		r.out.Cells([]string{"UNIVERSE", "STATE", "UP", "LAST STARTED", "BRANCHES", "NOTE"}, rows)
		for _, u := range list {
			var behind []string
			for _, c := range u.Checkouts {
				if c.Freshness != nil && c.Freshness.Behind > 0 {
					behind = append(behind, fmt.Sprintf("%s %d", c.Repo, c.Freshness.Behind))
				}
			}
			if len(behind) > 0 {
				r.out.Warn("universe %s runs older code than origin has (%s commits behind): `multiversectl universe sync %s`", u.Name, strings.Join(behind, ", "), u.Name)
			}
		}
		return nil
	})
	return cmd
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

func (r runner) noteCmd() *cobra.Command {
	var pr string
	var clear bool
	cmd := &cobra.Command{
		Use:   "note <universe> [text]",
		Short: "Say what a universe is for, and which pull request its work is going to",
		Long: "The note and the PR are kept with the universe and shown by ls, status, -o json and\n" +
			"the web page, so a person or another agent finding it later knows what it is doing\n" +
			"without asking. With neither text nor --pr it prints what is there; --clear empties both.",
		Example: "  multiversectl universe note 3 \"fixing the login redirect\"\n" +
			"  multiversectl universe note 3 --pr https://github.com/acme/api/pull/42\n" +
			"  multiversectl universe note 3",
		Args: cobra.MinimumNArgs(1),
	}
	cmd.Flags().StringVar(&pr, "pr", "", "the pull or merge request the work is going to (a URL)")
	cmd.Flags().BoolVar(&clear, "clear", false, "remove the note and the PR")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return r.withService(func(ctx context.Context, s *app.Service) error {
			name, text := args[0], strings.Join(args[1:], " ")
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
				t, p, err := s.Note(name)
				if err != nil {
					return err
				}
				if t == "" && p == "" {
					r.out.Info("no note on %s: `multiversectl universe note %s \"what it is for\" --pr <url>`", name, name)
					return nil
				}
				if p != "" {
					r.out.Plain("pr   %s", p)
				}
				if t != "" {
					r.out.Plain("note %s", t)
				}
				return nil
			}
			st, err := s.SetNote(name, in)
			if err != nil {
				return err
			}
			if st.PR == "" && st.Note == "" {
				r.out.OK("universe %s: note cleared", st.Name)
				return nil
			}
			r.out.OK("universe %s: %s", st.Name, strings.TrimSpace(st.PR+" "+st.Note))
			return nil
		})(cmd, args)
	}
	return cmd
}
