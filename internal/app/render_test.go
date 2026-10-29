package app_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cristiansteib/multiversectl/internal/adapters/fsx"
	"github.com/cristiansteib/multiversectl/internal/adapters/gitcli"
	"github.com/cristiansteib/multiversectl/internal/app"
	"github.com/cristiansteib/multiversectl/internal/domain"
)

// These tests use the real filesystem and the real git: what is promised is
// that git does not see the file, and only the program that sees both can say so.

type renderStore struct {
	app.Store
	st domain.UniverseState
}

func (l renderStore) Load(string) (domain.UniverseState, error) { return l.st, nil }

type renderLines struct{ got []string }

func (l *renderLines) Info(f string, a ...any)  { l.got = append(l.got, "info: "+fmt.Sprintf(f, a...)) }
func (l *renderLines) OK(f string, a ...any)    { l.got = append(l.got, "ok: "+fmt.Sprintf(f, a...)) }
func (l *renderLines) Warn(f string, a ...any)  { l.got = append(l.got, "warn: "+fmt.Sprintf(f, a...)) }
func (l *renderLines) Plain(f string, a ...any) { l.got = append(l.got, fmt.Sprintf(f, a...)) }
func (*renderLines) Table([]string, [][]string) {}
func (l *renderLines) text() string             { return strings.Join(l.got, "\n") }

func renderGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=T", "-c", "user.email=t@example.test", "-c", "commit.gpgsign=false"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

type renderRig struct {
	svc      *app.Service
	out      *renderLines
	repoDir  string
	manifest string // the manifest's directory
}

// newRenderRig makes universe "1" of a multiverse with one repository, api, that is a
// plain checkout (or, with linked, a worktree of a clone) holding tracked.json.
func newRenderRig(t *testing.T, linked bool, renders ...domain.Render) *renderRig {
	t.Helper()
	root := t.TempDir()
	repoDir := filepath.Join(root, "universes", "1", "api")
	if err := os.MkdirAll(filepath.Dir(repoDir), 0o755); err != nil {
		t.Fatal(err)
	}
	clone := repoDir
	if linked {
		clone = filepath.Join(root, "api")
	}
	renderGit(t, root, "init", "-q", "-b", "main", clone)
	if err := os.WriteFile(filepath.Join(clone, "tracked.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	renderGit(t, clone, "add", ".")
	renderGit(t, clone, "commit", "-qm", "seed")
	if linked {
		renderGit(t, clone, "worktree", "add", "-q", "-b", "u1", repoDir)
	}
	manifestDir := filepath.Join(root, "manifest")
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		t.Fatal(err)
	}
	m := &domain.Manifest{
		Name: "acme", Dir: manifestDir,
		Proxy: domain.Proxy{Entrypoints: map[string]int{"web": 80}},
		Repos: map[string]domain.Repo{"api": {URL: "https://example.test/acme/api.git"}},
		Apps:  map[string]domain.App{"api": {Repo: "api", Render: renders}},
	}
	st := domain.UniverseState{
		Name: "1", ID: "1", Slug: "u1", Number: 1, Domain: "example.test",
		Hosts: map[string]string{"api": "api-u1.example.test", "web": "web-u1.example.test"},
		Rules: map[string]domain.RouteRule{
			"api": {Hostname: "api-u1.example.test", Entrypoint: "web", PortForward: "auto", ForwardURL: "http://192.0.2.7:41000"},
			"web": {Hostname: "web-u1.example.test", Entrypoint: "web"},
		},
	}
	out := &renderLines{}
	svc := app.New(app.Deps{Git: gitcli.New(nil), Files: fsx.New(), Store: renderStore{st: st}, Out: out, System: renderSys{}}, root, m, "test")
	return &renderRig{svc: svc, out: out, repoDir: repoDir, manifest: manifestDir}
}

type renderSys struct{ app.System }

func (renderSys) UID() int             { return 1000 }
func (renderSys) GID() int             { return 1000 }
func (renderSys) Getenv(string) string { return "" }

func (r *renderRig) render(t *testing.T) error {
	t.Helper()
	return r.svc.Render(context.Background(), app.RenderInput{Universe: "1"})
}

func (r *renderRig) read(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(r.repoDir, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const jsonConfig = `{
  "apiUrl": {{ url "api" | toJson }},
  "callback": {{ forward "api" | toJson }},
  "origins": {{ .Origins | toJson }},
  "cors": {{ origins "space" | toJson }},
  "universe": {{ .Universe | toJson }},
  "mode": {{ env "UNIVERSE_SLUG" | toJson }}
}
`

func TestARenderedJSONConfigHasTheUniversesAddresses(t *testing.T) {
	r := newRenderRig(t, false, domain.Render{To: "config/app.json", Template: jsonConfig, Mode: 0o600})
	if err := r.render(t); err != nil {
		t.Fatal(err)
	}
	want := `{
  "apiUrl": "http://api-u1.example.test",
  "callback": "http://192.0.2.7:41000",
  "origins": ["http://api-u1.example.test","http://web-u1.example.test"],
  "cors": "http://api-u1.example.test http://web-u1.example.test",
  "universe": "1",
  "mode": "u1"
}
`
	if got := r.read(t, "config/app.json"); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	if info, _ := os.Stat(filepath.Join(r.repoDir, "config/app.json")); info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", info.Mode().Perm())
	}
	if !strings.Contains(r.out.text(), "ok: api: rendered config/app.json") {
		t.Errorf("no line for the file:\n%s", r.out.text())
	}
}

func TestARenderedFileIsHiddenFromGitOnceAndTheGitignoreIsLeftAlone(t *testing.T) {
	for _, linked := range []bool{false, true} {
		t.Run(fmt.Sprintf("linked=%v", linked), func(t *testing.T) {
			r := newRenderRig(t, linked,
				domain.Render{To: "config.json", Template: "{}"},
				domain.Render{To: "odd [name]*.txt", Template: "x"})
			for range 3 {
				r.out.got = nil
				if err := r.render(t); err != nil {
					t.Fatal(err)
				}
			}
			if status := renderGit(t, r.repoDir, "status", "--porcelain"); status != "" {
				t.Errorf("git sees the rendered files:\n%s", status)
			}
			if _, err := os.Stat(filepath.Join(r.repoDir, ".gitignore")); err == nil {
				t.Error("a .gitignore appeared")
			}
			excl := renderGit(t, r.repoDir, "rev-parse", "--path-format=absolute", "--git-path", "info/exclude")
			b, _ := os.ReadFile(excl)
			for _, line := range []string{"/config.json", `/odd \[name]\*.txt`} {
				if n := strings.Count(string(b), line+"\n"); n != 1 {
					t.Errorf("%q appears %d times in %s:\n%s", line, n, excl, b)
				}
			}
		})
	}
}

func TestARenderThatChangesNothingWritesNothing(t *testing.T) {
	r := newRenderRig(t, false, domain.Render{To: "config.json", Template: `{"u": {{ .Universe | toJson }}}`})
	if err := r.render(t); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(r.repoDir, "config.json")
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	r.out.got = nil
	if err := r.render(t); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(path); !info.ModTime().Equal(old) {
		t.Errorf("the file was rewritten: %v", info.ModTime())
	}
	if !strings.Contains(r.out.text(), "config.json is up to date") {
		t.Errorf("not reported as up to date:\n%s", r.out.text())
	}
	// A leftover of the atomic write would show up beside it.
	if _, err := os.Stat(filepath.Join(r.repoDir, ".config.json.multiverse-tmp")); err == nil {
		t.Error("a temporary file was left behind")
	}
}

func TestAMissingValueNamesTheFileAndTheKeyAndWritesNothing(t *testing.T) {
	cases := map[string]struct{ tpl, want string }{
		"unknown host":       {`{{ url "billing" }}`, `no host "billing"`},
		"unforwarded host":   {`{{ forward "web" }}`, `host "web" is not forwarded`},
		"unset env":          {`{{ env "NOPE" }}`, `"NOPE" is not set`},
		"unknown field":      {`{{ .Nope }}`, "Nope"},
		"required and empty": {`{{ required "SECRET is needed" (index .Env "SECRET") }}`, "SECRET is needed"},
		"unknown separator":  {`{{ origins "dash" }}`, `unknown separator "dash"`},
		"a syntax error":     {`{{ url "api" `, "unclosed action"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRenderRig(t, false, domain.Render{To: "config.json", Template: c.tpl})
			err := r.render(t)
			if err == nil {
				t.Fatal("rendered")
			}
			if !strings.Contains(err.Error(), "config.json") || !strings.Contains(err.Error(), c.want) {
				t.Errorf("error does not name the file and the key: %v", err)
			}
			if _, statErr := os.Stat(filepath.Join(r.repoDir, "config.json")); statErr == nil {
				t.Error("a file was written")
			}
		})
	}
}

func TestDefaultFillsAnEmptyValue(t *testing.T) {
	r := newRenderRig(t, false, domain.Render{To: "x.txt", Template: `{{ default "fallback" (index .Env "SECRET") }}`})
	if err := r.render(t); err != nil {
		t.Fatal(err)
	}
	if got := r.read(t, "x.txt"); got != "fallback" {
		t.Errorf("got %q", got)
	}
}

func TestARenderNeverWritesOutsideTheCheckout(t *testing.T) {
	outside := t.TempDir()
	r := newRenderRig(t, false, domain.Render{To: "link/x.json", Template: "{}"}, domain.Render{To: "evil.json", Template: "{}"})
	if err := os.Symlink(outside, filepath.Join(r.repoDir, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "target"), filepath.Join(r.repoDir, "evil.json")); err != nil {
		t.Fatal(err)
	}
	if err := r.render(t); err == nil || !strings.Contains(err.Error(), "outside the checkout") {
		t.Fatalf("a directory link out was followed: %v", err)
	}
	// And a link standing where the file goes is not written through.
	r.svc.Manifest.Apps["api"] = domain.App{Repo: "api", Render: []domain.Render{{To: "evil.json", Template: "{}"}}}
	if err := r.render(t); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("a link in place of the file was followed: %v", err)
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Errorf("something was written outside: %v", entries)
	}
}

func TestATrackedFileIsNotOverwrittenUnlessAsked(t *testing.T) {
	r := newRenderRig(t, false, domain.Render{To: "tracked.json", Template: `{"rendered": true}`})
	err := r.render(t)
	if err == nil || !strings.Contains(err.Error(), "git tracks it") || !strings.Contains(err.Error(), "overwrite: true") {
		t.Fatalf("err = %v", err)
	}
	if got := r.read(t, "tracked.json"); got != "{}" {
		t.Errorf("the tracked file was changed: %q", got)
	}
	r.svc.Manifest.Apps["api"] = domain.App{Repo: "api", Render: []domain.Render{{To: "tracked.json", Template: `{"rendered": true}`, Overwrite: true}}}
	if err := r.render(t); err != nil {
		t.Fatal(err)
	}
	if got := r.read(t, "tracked.json"); got != `{"rendered": true}` {
		t.Errorf("got %q", got)
	}
	// A tracked file is not hidden: it is meant to show up as a change.
	if status := renderGit(t, r.repoDir, "status", "--porcelain"); !strings.Contains(status, "tracked.json") {
		t.Errorf("the change is hidden: %q", status)
	}
}

func TestATemplateCanComeFromTheRepositoryOrTheManifestDirectory(t *testing.T) {
	r := newRenderRig(t, false,
		domain.Render{To: "a.txt", From: "tpl/a.tmpl"},
		domain.Render{To: "b.txt", From: "@templates/b.tmpl"},
		domain.Render{To: "c.txt", From: "../c.tmpl"})
	for path, body := range map[string]string{
		filepath.Join(r.repoDir, "tpl/a.tmpl"):        `a={{ host "api" }}`,
		filepath.Join(r.manifest, "templates/b.tmpl"): `b={{ .Slug }}`,
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Validation refuses ../ before this runs; the app still guards itself.
	err := r.render(t)
	if err == nil || !strings.Contains(err.Error(), "c.txt") {
		t.Fatalf("an escaping from was accepted: %v", err)
	}
	if r.read(t, "a.txt") != "a=api-u1.example.test" || r.read(t, "b.txt") != "b=u1" {
		t.Errorf("a=%q b=%q", r.read(t, "a.txt"), r.read(t, "b.txt"))
	}
}

func TestPrintShowsTheFileAndWritesNothing(t *testing.T) {
	r := newRenderRig(t, false, domain.Render{To: "config.json", Template: `{"u": {{ url "api" | toJson }}}`})
	if err := r.svc.Render(context.Background(), app.RenderInput{Universe: "1", Print: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.out.text(), `{"u": "http://api-u1.example.test"}`) {
		t.Errorf("not shown:\n%s", r.out.text())
	}
	if _, err := os.Stat(filepath.Join(r.repoDir, "config.json")); err == nil {
		t.Error("written")
	}
	excl := renderGit(t, r.repoDir, "rev-parse", "--path-format=absolute", "--git-path", "info/exclude")
	if b, _ := os.ReadFile(excl); strings.Contains(string(b), "config.json") {
		t.Error("the exclude file was touched")
	}
}

type startRuntime struct {
	app.Runtime
	configAtUp []string // what the file held when the services were started; "" if absent
	path       string
	ups, downs []string        // compose projects started and brought down
	running    []app.Container // what Containers reports
	// health is what Health reports on each call, the last one repeating.
	health [][]app.ServiceHealth
	asked  int
}

func (r *startRuntime) Health(context.Context, app.RuntimeRequest) ([]app.ServiceHealth, error) {
	r.asked++
	if len(r.health) == 0 {
		return nil, nil
	}
	h := r.health[0]
	if len(r.health) > 1 {
		r.health = r.health[1:]
	}
	return h, nil
}

func (r *startRuntime) Down(_ context.Context, req app.RuntimeRequest, _ bool) error {
	r.downs = append(r.downs, req.Project)
	return nil
}

func (r *startRuntime) Containers(context.Context, string) ([]app.Container, error) {
	return r.running, nil
}

func (r *startRuntime) Inspect(context.Context, app.RuntimeRequest) (app.ProjectConfig, error) {
	return app.ProjectConfig{Services: map[string]app.ServiceConfig{"web": {}}}, nil
}

func (r *startRuntime) Up(_ context.Context, req app.RuntimeRequest) error {
	r.ups = append(r.ups, req.Project)
	b, _ := os.ReadFile(r.path)
	r.configAtUp = append(r.configAtUp, string(b))
	return nil
}

type startStore struct{ renderStore }

func (startStore) Save(domain.UniverseState) error       { return nil }
func (startStore) List() ([]domain.UniverseState, error) { return nil, nil }
func (startStore) SaveConfig(string, []byte) error       { return nil }

type startProxy struct{ app.Proxy }

func (startProxy) EnsureNetwork(context.Context, string) error { return nil }
func (startProxy) Status(context.Context) (app.ProxyStatus, error) {
	return app.ProxyStatus{Running: true, Bind: "0.0.0.0", Entrypoints: map[string]int{"web": 80}}, nil
}
func (startProxy) EnsureUp(context.Context, app.ProxyRequest) error { return nil }

type startRouter struct{}

func (startRouter) Materialize(context.Context, app.OverrideRequest) ([]string, error) {
	return nil, nil
}

// startRig is a universe that can be started, with the services' start observed.
func startRig(t *testing.T, tpl string) (*renderRig, *startRuntime) {
	t.Helper()
	r := newRenderRig(t, false, domain.Render{To: "config.json", Template: tpl})
	if err := os.WriteFile(filepath.Join(r.repoDir, "compose.yaml"), []byte("services: {web: {image: x}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifestFile := filepath.Join(r.manifest, "multiverse.yaml")
	if err := os.WriteFile(manifestFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := r.svc.Manifest
	m.Path, m.Domain, m.Expose = manifestFile, "example.test", domain.ExposeLocal
	a := m.Apps["api"]
	a.Hosts = map[string]domain.HostRule{"api": {Service: "web", Port: 80, Entrypoint: "web"}}
	m.Apps["api"] = a
	rt := &startRuntime{path: filepath.Join(r.repoDir, "config.json")}
	st, _ := r.svc.Resolve("1")
	r.svc = app.New(app.Deps{Git: gitcli.New(nil), Files: fsx.New(), Store: startStore{renderStore{st: st}},
		Runtime: rt, Proxy: startProxy{}, Router: startRouter{}, Out: r.out, System: renderSys{}}, filepath.Dir(filepath.Dir(filepath.Dir(r.repoDir))), m, "test")
	return r, rt
}

func TestTheFileExistsWhenTheServicesStart(t *testing.T) {
	r, rt := startRig(t, `{"api": {{ url "api" | toJson }}}`)
	if err := r.svc.Start(context.Background(), app.StartInput{Universe: "1"}); err != nil {
		t.Fatal(err)
	}
	if len(rt.configAtUp) != 1 || rt.configAtUp[0] != `{"api": "http://api-u1.example.test"}` {
		t.Errorf("at start the file held %q", rt.configAtUp)
	}
}

func TestAStartThatCannotRenderStartsNothing(t *testing.T) {
	r, rt := startRig(t, `{{ url "billing" }}`)
	err := r.svc.Start(context.Background(), app.StartInput{Universe: "1"})
	if err == nil || !strings.Contains(err.Error(), `render config.json`) || !strings.Contains(err.Error(), `no host "billing"`) {
		t.Fatalf("err = %v", err)
	}
	if len(rt.configAtUp) != 0 {
		t.Error("the services were started")
	}
}
