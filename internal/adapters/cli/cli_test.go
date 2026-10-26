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
