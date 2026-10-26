package cli

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/cristiansteib/multiversectl/internal/app"
	"github.com/cristiansteib/multiversectl/internal/domain"
)

func TestParseBranches(t *testing.T) {
	all, byRepo, err := parseBranches([]string{"api=feature/login", "web=main"})
	if err != nil || all != "" || byRepo["api"] != "feature/login" || byRepo["web"] != "main" {
		t.Fatalf("got %q, %v, %v", all, byRepo, err)
	}
	// A bare name is every repository; a pair alongside it overrides one.
	all, byRepo, err = parseBranches([]string{"feat/invoicing", "web=main"})
	if err != nil || all != "feat/invoicing" || byRepo["web"] != "main" {
		t.Fatalf("got %q, %v, %v", all, byRepo, err)
	}
	for _, bad := range []string{"=x", "api="} {
		if _, _, err := parseBranches([]string{bad}); err == nil {
			t.Errorf("%q: want an error", bad)
		}
	}
}

func TestExportLineEscapesQuotes(t *testing.T) {
	if got, want := exportLine("A", "it's $HOME"), `export A='it'\''s $HOME'`; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func lsFixture() []app.Summary {
	return []app.Summary{
		{Name: "1", Slug: "u1", Project: "acme-u1", Domain: "example.test", Running: 2,
			Branches: map[string]string{"api": "u1", "docs": "u1"},
			Dir:      "/w/universes/1",
			Checkouts: []app.Checkout{
				{Repo: "api", Branch: "u1", Path: "/w/universes/1/api", App: "api"},
				{Repo: "docs", Branch: "u1", Path: "/w/universes/1/docs"},
			},
			Apps: []app.AppSummary{{Key: "api", Repo: "api", Branch: "u1", Running: 2,
				Path:     "/w/universes/1/api",
				Project:  "acme-u1-api",
				Expected: 3,
				Volumes:  []string{"acme-u1-api_postgres_data"},
				URLs:     []app.HostURL{{Host: "api", Service: "web", URL: "http://api-u1.example.test"}},
				Services: []app.ServiceStatus{
					{Name: "web", State: app.StateRunning, Detail: "Up 2 hours"},
					{Name: "db", State: app.StateRunning, Detail: "Up 2 hours", Published: "0.0.0.0:5432->5432/tcp"},
					{Name: "mailpit", State: app.StateNotUp},
					{Name: "celery-worker", State: app.StateOptional, Detail: "--with celery"},
				}}}},
		{Name: "fix/login", Slug: "fix-login", Project: "acme-fix-login", Domain: "example.test",
			Branches:  map[string]string{"api": "u2"},
			Dir:       "/w/universes/fix-login",
			Checkouts: []app.Checkout{{Repo: "api", Branch: "fix/login", Path: "/w/universes/fix-login/api", App: "api"}},
			Apps: []app.AppSummary{{Key: "api", Repo: "api", Branch: "fix/login",
				Path: "/w/universes/fix-login/api", Project: "acme-fix-login-api"}}},
	}
}

func TestLsTableSeparatesUniverses(t *testing.T) {
	var buf bytes.Buffer
	lsTable(NewOutput(&buf, false), lsFixture(), false)
	out := buf.String()

	if n := strings.Count(out, "─"); n == 0 {
		t.Error("want a rule under each universe's title")
	}
	if !strings.Contains(out, "\n\nuniverse fix/login") {
		t.Errorf("want a blank line between universes, and the second named as typed:\n%s", out)
	}
	if !strings.Contains(out, "stopped") {
		t.Error("want the second universe marked stopped")
	}
}

// A repository with no app is still checked out, so it must have a row of its
// own rather than being dropped from the table.
func TestLsTableRowForARepoWithoutAnApp(t *testing.T) {
	for _, wide := range []bool{false, true} {
		var buf bytes.Buffer
		lsTable(NewOutput(&buf, false), lsFixture(), wide)
		out := buf.String()
		var row string
		// Only the table itself: the wide shape names worktrees again below.
		body, _, _ := strings.Cut(out, "on disk")
		for _, line := range strings.Split(body, "\n") {
			if strings.HasPrefix(line, "docs") {
				row = line
			}
		}
		if row == "" {
			t.Fatalf("wide=%v: want a row for the docs checkout:\n%s", wide, out)
		}
		for _, want := range []string{"u1", "checkout only"} {
			if !strings.Contains(row, want) {
				t.Errorf("wide=%v: want %q in the checkout row, got %q", wide, want, row)
			}
		}
		// Where the code is belongs to the wide shape, in its own block. The
		// plain one answers which app, which branch and which address, and a
		// column of paths is what made it too wide to read.
		if got := strings.Contains(out, "/w/universes/1/docs"); got != wide {
			t.Errorf("wide=%v: the worktree is named: %v\n%s", wide, got, out)
		}
	}
}

func TestLsWideShowsEveryServiceAndItsVolumes(t *testing.T) {
	var buf bytes.Buffer
	lsTable(NewOutput(&buf, false), lsFixture(), true)
	out := buf.String()

	for _, want := range []string{
		"mailpit",                   // defined, never started
		"not started",               //   and said so
		"off  --with celery",        // off on purpose, with the flag to start it
		"none",                      // a running service publishing no host port
		"0.0.0.0:5432->5432/tcp",    // and one that does
		"on disk",                   // the block naming what the universe owns
		"/w/universes/1/api",        // the worktree the code is in
		"acme-u1-api",               // the app's own compose project
		"acme-u1-api_postgres_data", // the volume, named in full
	} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in the wide table:\n%s", want, out)
		}
	}
}

func TestParseLsFormatCombines(t *testing.T) {
	for spec, want := range map[string]lsFormat{
		"":                lsFormat{table: true},
		"table":           lsFormat{table: true},
		"wide":            lsFormat{table: true, wide: true},
		"table,wide":      lsFormat{table: true, wide: true},
		"list, wide":      lsFormat{list: true, wide: true},
		"compact":         lsFormat{list: true},
		"table,list,wide": lsFormat{table: true, list: true, wide: true},
	} {
		got, err := parseLsFormat(spec)
		if err != nil || got != want {
			t.Errorf("%q: got %+v, %v; want %+v", spec, got, err, want)
		}
	}
	if _, err := parseLsFormat("table,yaml"); err == nil {
		t.Error("want an error naming the unknown format")
	}
}

func TestShortPathUsesTilde(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if got, want := shortPath(home+"/w/universes/3/api"), "~/w/universes/3/api"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// A path outside the home directory, and an empty one, are left alone.
	for _, path := range []string{"/srv/universes/3/api", ""} {
		if got := shortPath(path); got != path {
			t.Errorf("shortPath(%q) = %q", path, got)
		}
	}
}

// Every row of a group names its app, so a row is never orphaned from its
// owner and `ls | grep <app>` keeps all of them.
func TestLsRowsNameTheirApp(t *testing.T) {
	for _, wide := range []bool{false, true} {
		var buf bytes.Buffer
		lsTable(NewOutput(&buf, false), lsFixture(), wide)
		body, _, _ := strings.Cut(buf.String(), "on disk")
		var rows, named int
		for _, line := range strings.Split(body, "\n") {
			if line == "" || strings.HasPrefix(line, "APP") || strings.HasPrefix(line, "universe") ||
				strings.HasPrefix(line, "─") || strings.HasPrefix(line, "docs") ||
				strings.HasPrefix(line, "created ") {
				continue
			}
			rows++
			if strings.HasPrefix(line, "api") {
				named++
			}
		}
		if rows == 0 || named != rows {
			t.Errorf("wide=%v: %d of %d rows name their app:\n%s", wide, named, rows, body)
		}
	}
}

// The default answers the question somebody who typed `ls` has: which app, on
// which branch, at which address. Everything else is what -o wide is for.
func TestLsTableIsJustTheAddresses(t *testing.T) {
	var buf bytes.Buffer
	lsTable(NewOutput(&buf, false), lsFixture(), false)
	out := buf.String()

	header := ""
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "APP") {
			header = line
			break
		}
	}
	if fields := strings.Fields(header); len(fields) != 3 ||
		fields[0] != "APP" || fields[1] != "BRANCH" || fields[2] != "URL" {
		t.Errorf("want app, branch and url alone, got %q", header)
	}
	for _, gone := range []string{"SERVICE", "RUNNING", "PATH", "HOST"} {
		if strings.Contains(header, gone) {
			t.Errorf("%s belongs to -o wide now, got %q", gone, header)
		}
	}
	// What the addresses are for is still there.
	if !strings.Contains(out, "http://api-u1.example.test") {
		t.Errorf("want the addresses:\n%s", out)
	}
}

// A host that is also forwarded to a port has two addresses. They belong on
// one row, beside each other, and the column is there only when one is.
func TestAForwardedAddressSitsBesideTheOneItMirrors(t *testing.T) {
	plain := lsFixture()
	var buf bytes.Buffer
	lsTable(NewOutput(&buf, false), plain, false)
	if strings.Contains(buf.String(), "FORWARDED") {
		t.Errorf("want no column when nothing is forwarded:\n%s", buf.String())
	}

	forwarded := lsFixture()
	apps := forwarded[0].Apps
	apps[0].URLs = append(apps[0].URLs, app.HostURL{
		Host: "api:forward", Service: "web", URL: "http://192.168.1.20:33001",
	})
	buf.Reset()
	lsTable(NewOutput(&buf, false), forwarded, false)
	out := buf.String()

	if !strings.Contains(out, "FORWARDED") {
		t.Fatalf("want the column once something is forwarded:\n%s", out)
	}
	var row string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "http://api-u1.example.test") {
			row = line
		}
	}
	if !strings.Contains(row, "http://192.168.1.20:33001") {
		t.Errorf("want both addresses on one row, got %q", row)
	}
	// And not as a row of its own, pretending to be another host.
	if strings.Contains(out, "api:forward") {
		t.Errorf("the marker is plumbing, not something to read:\n%s", out)
	}
}

// start picks the universe without being told when there is no doubt: none
// yet means a new one, exactly one means that one. Several is a question.
func TestStartPicksTheUniverseWhenThereIsNoDoubt(t *testing.T) {
	for _, c := range []struct {
		name string
		have []string
		want string
		asks bool
	}{
		{name: "none yet", have: nil, want: "1"},
		{name: "only one", have: []string{"3"}, want: "3"},
		{name: "several", have: []string{"1", "fix/login"}, asks: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := runner{g: &Globals{}, out: NewOutput(io.Discard, false), f: Factory{Prompter: func() app.Prompter { return noTerminal{} }}}
			s := startService(c.have)
			got, err := r.startUniverse(&cobra.Command{}, s, "")
			switch {
			case c.asks && err == nil:
				t.Fatalf("want it to ask, got %q", got)
			case c.asks:
				// And the way out says which flag, since the universe is one.
				if !strings.Contains(err.Error(), "start -u") {
					t.Errorf("want the flag named, got %q", err)
				}
				for _, name := range c.have {
					if !strings.Contains(err.Error(), name) {
						t.Errorf("want %q among the choices, got %q", name, err)
					}
				}
			case err != nil:
				t.Fatalf("startUniverse: %v", err)
			case got != c.want:
				t.Errorf("= %q, want %q", got, c.want)
			}
		})
	}
}

// Naming one skips every question, which is what a script does.
func TestStartTakesTheUniverseItIsGiven(t *testing.T) {
	r := runner{g: &Globals{}, out: NewOutput(io.Discard, false), f: Factory{Prompter: func() app.Prompter { return noTerminal{} }}}
	got, err := r.startUniverse(&cobra.Command{}, startService([]string{"1", "2"}), "2")
	if err != nil || got != "2" {
		t.Fatalf("= %q, %v", got, err)
	}
}

// startService is a project with the given universes and nothing else.
func startService(names []string) *app.Service {
	states := make([]domain.UniverseState, len(names))
	for i, n := range names {
		states[i] = domain.UniverseState{ID: n, Name: n, Branches: map[string]string{"api": "u" + n}}
	}
	m := &domain.Manifest{Name: "acme", Repos: map[string]domain.Repo{"api": {}}}
	return app.New(app.Deps{Store: startStore(states), Git: noBranches{}, Files: nothingOnDisk{}, Out: app.Output(discard{})}, "/w", m, "test")
}

type startStore []domain.UniverseState

func (s startStore) List() ([]domain.UniverseState, error) { return s, nil }
func (s startStore) Load(id string) (domain.UniverseState, error) {
	for _, st := range s {
		if st.ID == id {
			return st, nil
		}
	}
	return domain.UniverseState{}, domain.ErrNotFound
}
func (s startStore) Save(domain.UniverseState) error { return nil }
func (s startStore) Delete(string) error             { return nil }

type discard struct{}

func (discard) Info(string, ...any)        {}
func (discard) OK(string, ...any)          {}
func (discard) Warn(string, ...any)        {}
func (discard) Plain(string, ...any)       {}
func (discard) Table([]string, [][]string) {}

// noBranches and nothingOnDisk say no universe has left anything behind, so
// the next free number is simply the next one.
type noBranches struct{ app.Git }

func (noBranches) RefExists(context.Context, string, string) (app.RefKind, error) {
	return app.RefNone, nil
}

type nothingOnDisk struct{ app.Files }

func (nothingOnDisk) Exists(string) bool { return false }

// The tree is read off the commands themselves, so the way to test it is
// against them: every command the tool has must be in it, nested where it is
// typed, and the ones that are hidden on purpose must not be.
func TestHelpTreeShowsEveryCommandWhereItIsTyped(t *testing.T) {
	root, _ := New(Factory{})
	var buf bytes.Buffer
	run := runner{g: &Globals{}, out: NewOutput(&buf, false), f: Factory{}}
	cmd := run.helpTreeCmd(root)
	cmd.SetContext(context.Background())
	if err := cmd.Flags().Set("flat", "true"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("help-tree: %v", err)
	}
	flat := buf.String()

	for _, want := range []string{
		"start  —", "universe rm  —", "universe proxy up  —",
		"project install  —", "server pair  —", "agents skill install  —",
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("want %q in:\n%s", want, flat)
		}
	}
	// Cobra's own help command is not the tool's.
	if strings.Contains(flat, "\nhelp ") || strings.HasPrefix(flat, "help ") {
		t.Errorf("the help command is plumbing, not part of the tree:\n%s", flat)
	}
}

func TestHelpTreeNestsAndNamesAliases(t *testing.T) {
	root, _ := New(Factory{})
	var buf bytes.Buffer
	run := runner{g: &Globals{}, out: NewOutput(&buf, false), f: Factory{}}
	cmd := run.helpTreeCmd(root)
	cmd.SetContext(context.Background())
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("help-tree: %v", err)
	}
	out := buf.String()
	// Aliases, which no help screen puts beside the name they belong to.
	if !strings.Contains(out, "rm (remove, destroy)") {
		t.Errorf("want the aliases of rm beside it:\n%s", out)
	}
	if !strings.Contains(out, "universe (u)") {
		t.Errorf("want universe's alias:\n%s", out)
	}
	// Nested: proxy's commands hang under proxy, which hangs under universe.
	if !strings.Contains(out, "│   ├── proxy") && !strings.Contains(out, "│   │   ├── down") {
		t.Errorf("want proxy's children one level below it:\n%s", out)
	}
	// One column for every description, whatever level it is drawn at.
	var cols []int
	for _, d := range []string{"Print the skill", "Print version, commit", "Stop the proxy"} {
		for _, line := range strings.Split(out, "\n") {
			if i := strings.Index(line, d); i >= 0 {
				cols = append(cols, len([]rune(line[:i])))
			}
		}
	}
	if len(cols) != 3 || cols[0] != cols[1] || cols[1] != cols[2] {
		t.Errorf("descriptions are not in one column: %v\n%s", cols, out)
	}
	if !strings.Contains(out, " commands") {
		t.Errorf("want a count:\n%s", out)
	}
}

// What somebody runs first leads the help, not the housekeeping it is
// alphabetised among.
func TestInitLeadsTheHelp(t *testing.T) {
	root, _ := New(Factory{})
	groups := root.Groups()
	if len(groups) == 0 || groups[0].ID != groupStart {
		t.Fatalf("want the get-started group first, got %+v", groups)
	}
	init, _, err := root.Find([]string{"init"})
	if err != nil || init.GroupID != groupStart {
		t.Errorf("init is in group %q, want %q (%v)", init.GroupID, groupStart, err)
	}
	var help bytes.Buffer
	root.SetOut(&help)
	if err := root.Help(); err != nil {
		t.Fatal(err)
	}
	text := help.String()
	at := strings.Index(text, "init ")
	for _, after := range []string{"server ", "status ", "universe", "cleanup ", "completion ", "catalog "} {
		if i := strings.Index(text, after); i >= 0 && i < at {
			t.Errorf("%q is listed before init:\n%s", after, text)
		}
	}
}

func TestInitLeadsTheTreeAtTheTopLevelOnly(t *testing.T) {
	root, _ := New(Factory{})
	nodes := commandTree(root)
	if len(nodes) == 0 || nodes[0].Name != "init" {
		t.Fatalf("want init first, got %v", nodes)
	}
	// The rest stay alphabetical, so the tree is still easy to look things up in.
	for i := 2; i < len(nodes); i++ {
		if nodes[i-1].Name > nodes[i].Name {
			t.Errorf("%s comes after %s", nodes[i-1].Name, nodes[i].Name)
		}
	}
	// `project init` is a different command and is not pulled to the front of
	// its own group just for sharing a name.
	for _, n := range nodes {
		if n.Name != "project" {
			continue
		}
		if n.Children[0].Name == "init" {
			t.Errorf("project init was pinned too: %v", n.Children)
		}
	}
}
