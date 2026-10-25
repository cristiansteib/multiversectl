package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/app"
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
		// Wide names worktrees in its own block, the plain table in a column;
		// either way the path must be somewhere.
		if !strings.Contains(out, "/w/universes/1/docs") {
			t.Errorf("wide=%v: want the docs worktree named:\n%s", wide, out)
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
		"acme-u1-api",               // the app's own runtime project
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
