package server

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"testing"
	"time"
)

// TestDevUI serves the web view over an invented project, for looking at it
// and for the screenshots in the documentation. It is skipped unless MVCTL_DEV_UI
// is set, and the data is the example one the docs use — acme on example.test —
// never anything from the machine it runs on.
//
//	MVCTL_DEV_UI=1 go test ./internal/adapters/server -run TestDevUI -timeout 0
type demo struct{ *fake }

func (d demo) FleetStats(_ context.Context, _ string) (fleetStatsView, error) {
	t := float64(time.Now().UnixMilli()) / 1000
	row := func(u, app, service string, base, swing, mem float64, seed float64) fleetStatRow {
		cpu := base + swing*math.Sin(t/9+seed) + swing/3*math.Sin(t/2.3+seed*2)
		if cpu < 0.2 {
			cpu = 0.2
		}
		return fleetStatRow{Workspace: "acme", Universe: u, App: app, Service: service,
			statView: statView{Name: "acme-u" + u + "-" + app + "-" + service + "-1", CPU: cpu,
				Mem: mem * (1 + 0.04*math.Sin(t/17+seed)), MemLimit: 8 << 30, Memory: mem / float64(uint64(8)<<30) * 100,
				MemUsage: "x / 8GiB", NetIO: "12.4MB / 3.1MB", PIDs: 24}}
	}
	return fleetStatsView{At: time.Now().UTC().Format(time.RFC3339), Rows: []fleetStatRow{
		row("3", "api", "web", 24, 14, 512<<20, 0),
		row("3", "api", "worker", 9, 7, 268<<20, 1.4),
		row("3", "api", "db", 3, 2, 190<<20, 2.6),
		row("3", "ui", "app", 17, 11, 740<<20, 3.3),
		row("4", "api", "web", 12, 9, 430<<20, 4.1),
		row("4", "api", "db", 2, 1.5, 186<<20, 5),
		row("4", "ui", "app", 7, 5, 612<<20, 5.8),
		row("7", "api", "web", 31, 18, 560<<20, 6.5),
		row("7", "ui", "app", 11, 9, 690<<20, 7.2),
	}}, nil
}

func (d demo) Stats(_ context.Context, _, universe string) ([]statView, error) {
	all, _ := d.FleetStats(context.Background(), "")
	var out []statView
	for _, r := range all.Rows {
		if r.Universe == universe {
			out = append(out, r.statView)
		}
	}
	return out, nil
}

// Logs writes the kind of thing a dev server writes: colour, a table, and a
// spinner that rewrites its own line.
func (d demo) Logs(ctx context.Context, _, _, appKey, service string, tail int, follow bool, w io.Writer) error {
	at := time.Now().Add(-9 * time.Minute)
	say := func(f string, a ...any) {
		at = at.Add(7 * time.Second)
		fmt.Fprintf(w, "%s %s\n", at.UTC().Format(time.RFC3339Nano), fmt.Sprintf(f, a...))
	}
	say("\x1b[2m[acme/%s]\x1b[22m \x1b[32mstarting\x1b[39m %s", appKey, service)
	say("\x1b[1mBuilt files\x1b[22m          \x1b[2m|\x1b[22m \x1b[1mNames\x1b[22m             \x1b[2m|\x1b[22m \x1b[1mRaw size\x1b[22m")
	for _, f := range [][2]string{{"main-7KQ2F4.js", "main"}, {"polyfills-3B1A.js", "polyfills"}, {"styles-9CDE.css", "styles"}} {
		say("\x1b[36m%-22s\x1b[39m \x1b[2m|\x1b[22m %-17s \x1b[2m|\x1b[22m \x1b[33m%8s\x1b[39m", f[0], f[1], "214.70 kB")
	}
	say("-\x1b[1G\\\x1b[1G|\x1b[1G/\x1b[1Gbundle generation complete \x1b[2m[2.918 seconds]\x1b[22m")
	say("\x1b[32m✓\x1b[39m listening on \x1b[4mhttps://%s-3.example.test\x1b[24m", appKey)
	say("\x1b[2m12:04:11\x1b[22m \x1b[36mGET\x1b[39m  /api/invoices            \x1b[32m200\x1b[39m  \x1b[2m31ms\x1b[22m")
	say("\x1b[2m12:04:12\x1b[22m \x1b[36mGET\x1b[39m  /api/invoices/8821       \x1b[32m200\x1b[39m  \x1b[2m12ms\x1b[22m")
	say("\x1b[2m12:04:14\x1b[22m \x1b[35mPOST\x1b[39m /api/invoices/8821/send  \x1b[32m201\x1b[39m  \x1b[2m244ms\x1b[22m")
	say("\x1b[33mwarning\x1b[39m deprecated: the tax column moves to its own table in 4.0")
	say("\x1b[2m12:04:31\x1b[22m \x1b[36mGET\x1b[39m  /api/reports/monthly     \x1b[31m500\x1b[39m  \x1b[2m1.9s\x1b[22m")
	say("\x1b[31mERROR\x1b[39m report.monthly: division by zero at totals.py:118")
	say("\x1b[2m12:04:49\x1b[22m \x1b[36mGET\x1b[39m  /healthz                 \x1b[32m200\x1b[39m  \x1b[2m2ms\x1b[22m")
	paths := []string{"/api/invoices", "/api/invoices/8821", "/api/customers?page=2", "/api/reports/monthly",
		"/api/invoices/8821/lines", "/healthz", "/api/customers/412", "/api/invoices?open=1"}
	for i, p := range paths {
		say("\x1b[2m12:0%d:%02d\x1b[22m \x1b[36mGET\x1b[39m  %-24s \x1b[32m200\x1b[39m  \x1b[2m%dms\x1b[22m", 5+i/6, (i*7)%60, p, 9+i*13%180)
	}
	say("\x1b[2m12:06:02\x1b[22m \x1b[35mPOST\x1b[39m /api/invoices            \x1b[32m201\x1b[39m  \x1b[2m188ms\x1b[22m")
	say("\x1b[33mwarning\x1b[39m worker queue is 1,204 deep")
	for i, p := range paths {
		say("\x1b[2m12:0%d:%02d\x1b[22m \x1b[36mGET\x1b[39m  %-24s \x1b[32m200\x1b[39m  \x1b[2m%dms\x1b[22m", 6+i/6, (i*11)%60, p, 11+i*7%140)
	}
	if follow {
		for n := 1; ; n++ {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(3 * time.Second):
				say("\x1b[2m12:05:%02d\x1b[22m \x1b[36mGET\x1b[39m  /api/invoices            \x1b[32m200\x1b[39m  \x1b[2m%dms\x1b[22m", n%60, 20+n%40)
			}
		}
	}
	return nil
}

func TestDevUI(t *testing.T) {
	if os.Getenv("MVCTL_DEV_UI") == "" {
		t.Skip("set MVCTL_DEV_UI=1 to serve the example fleet")
	}
	b := demo{fake: demoFleet()}
	err := Serve(context.Background(), b, Options{Addr: "127.0.0.1:7791", Version: "1.4.0", Token: "devtoken"},
		func(u string) { fmt.Println("example fleet on", u) })
	if err != nil {
		t.Fatal(err)
	}
}

func demoFleet() *fake {
	svc := func(name, state, detail string) serviceView {
		return serviceView{Name: name, State: state, Detail: detail}
	}
	app := func(key, branch string, running, expected int, hosts []urlView, services ...serviceView) appView {
		return appView{Key: key, Repo: key, Branch: branch, Path: "/home/me/acme/universes/" + branch[1:] + "/" + key,
			Running: running, Expected: expected, URLs: hosts, Services: services}
	}
	url := func(host, service, u string) urlView { return urlView{Host: host, Service: service, URL: u} }
	now := time.Now().UTC()
	return &fake{
		fleet: fleetView{Workspaces: []fleetWorkspace{{Name: "acme", Current: true, Root: "/home/me/acme", Universes: []universeView{
			{Name: "3", Slug: "u3", Project: "acme-u3", Domain: "example.test", Dir: "/home/me/acme/universes/3",
				Running: 4, Created: now.Add(-52 * time.Hour).Format(time.RFC3339), LastStarted: now.Add(-3 * time.Hour).Format(time.RFC3339),
				Note: "invoice tax column", Branches: map[string]string{"api": "u3", "ui": "u3"},
				Apps: []appView{
					app("api", "u3", 3, 3, []urlView{url("api", "web", "https://api-3.example.test")},
						svc("web", "running", "Up 3 hours"), svc("worker", "running", "Up 3 hours"), svc("db", "running", "Up 3 hours (healthy)")),
					app("ui", "u3", 1, 1, []urlView{url("ui", "app", "https://ui-3.example.test")}, svc("app", "running", "Up 3 hours")),
				},
				Checkouts: []checkoutView{{Repo: "api", Branch: "u3", Path: "/home/me/acme/universes/3/api", App: "api"},
					{Repo: "ui", Branch: "u3", Path: "/home/me/acme/universes/3/ui", App: "ui"}}},
			{Name: "4", Slug: "u4", Project: "acme-u4", Domain: "example.test", Dir: "/home/me/acme/universes/4",
				Running: 3, Created: now.Add(-26 * time.Hour).Format(time.RFC3339), LastStarted: now.Add(-40 * time.Minute).Format(time.RFC3339),
				Branches: map[string]string{"api": "u4", "ui": "u4"}, Behind: 6,
				Apps: []appView{
					app("api", "u4", 2, 3, []urlView{url("api", "web", "https://api-4.example.test")},
						svc("web", "running", "Up 40 minutes"), svc("worker", "stopped", "Exited (0)"), svc("db", "running", "Up 40 minutes")),
					app("ui", "u4", 1, 1, []urlView{url("ui", "app", "https://ui-4.example.test")}, svc("app", "running", "Up 40 minutes")),
				},
				Checkouts: []checkoutView{{Repo: "api", Branch: "u4", Path: "/home/me/acme/universes/4/api", App: "api", Follows: "main", Behind: 6},
					{Repo: "ui", Branch: "u4", Path: "/home/me/acme/universes/4/ui", App: "ui", Follows: "main", Behind: 6}}},
			{Name: "7", Slug: "u7", Project: "acme-u7", Domain: "example.test", Dir: "/home/me/acme/universes/7",
				Running: 2, Created: now.Add(-3 * time.Hour).Format(time.RFC3339), LastStarted: now.Add(-15 * time.Minute).Format(time.RFC3339),
				Note: "reviewing #148", Branches: map[string]string{"api": "u7", "ui": "u7"},
				Apps: []appView{
					app("api", "u7", 1, 3, []urlView{url("api", "web", "https://api-7.example.test")},
						svc("web", "running", "Up 15 minutes"), svc("worker", "not started", ""), svc("db", "running", "Up 15 minutes")),
					app("ui", "u7", 1, 1, []urlView{url("ui", "app", "https://ui-7.example.test")}, svc("app", "running", "Up 15 minutes")),
				},
				Checkouts: []checkoutView{{Repo: "api", Branch: "u7", Path: "/home/me/acme/universes/7/api", App: "api"},
					{Repo: "ui", Branch: "u7", Path: "/home/me/acme/universes/7/ui", App: "ui"}}},
		}}}},
		code: codeView{Rows: []codeRow{
			{Repo: "api", Branch: "u3", Follows: "main",
				Work: &diffView{Files: 12, Added: 248, Removed: 96}, Ahead: 7,
				Review: &reviewView{Number: 142, Mark: "#", Title: "Invoices keep the tax column", URL: "https://example.test/acme/api/pull/142", Author: "robin", Updated: now.Add(-2 * time.Hour).Format(time.RFC3339)}},
			{Repo: "ui", Branch: "u3", Follows: "main",
				Work: &diffView{Files: 3, Added: 31, Removed: 4}, Ahead: 2, Dirty: true,
				Uncommitted: &diffView{Files: 1, Added: 14, Removed: 2},
				Review:      &reviewView{Number: 148, Mark: "#", Title: "Show the tax column in the invoice table", URL: "https://example.test/acme/ui/pull/148", Draft: true, Author: "robin", Updated: now.Add(-25 * time.Minute).Format(time.RFC3339)}},
		}},
		containers: []containerView{
			{App: "api", Service: "web", Name: "acme-u3-api-web-1", Status: "Up 3 hours", Running: true, Ports: "8000/tcp"},
			{App: "api", Service: "worker", Name: "acme-u3-api-worker-1", Status: "Up 3 hours", Running: true},
			{App: "api", Service: "db", Name: "acme-u3-api-db-1", Status: "Up 3 hours (healthy)", Running: true, Ports: "5432/tcp"},
			{App: "ui", Service: "app", Name: "acme-u3-ui-app-1", Status: "Up 3 hours", Running: true, Ports: "4200/tcp"},
		},
		logHold: true,
		system: systemView{Server: versionInfo{Version: "1.4.0", Commit: "9f21c0a", Date: "2026-10-20"},
			Format: "multiverse.dev/v1alpha1", Listen: "127.0.0.1:7777", Started: now.Add(-3 * time.Hour).Format(time.RFC3339),
			Latest: latestView{Version: "1.4.0"},
			Tools: []toolVersion{
				{Name: "multiversectl", Version: "1.4.0", Path: "/usr/local/bin/multiversectl", OK: true},
				{Name: "git", Version: "2.47.1", Path: "/usr/bin/git", OK: true, Note: "worktrees"},
				{Name: "docker", Version: "27.3.1", Path: "/usr/bin/docker", OK: true, Note: "containers"},
				{Name: "docker compose", Version: "2.30.3", Path: "/usr/libexec/docker/cli-plugins", OK: true, Note: "apps"},
				{Name: "gh", Version: "2.63.2", Path: "/usr/bin/gh", OK: true, Note: "pull requests"},
				{Name: "glab", OK: false, Note: "not installed"},
			}},
		next: nextView{Number: 8},
	}
}
