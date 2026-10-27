package manifest

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/app"
	"github.com/cristiansteib/multiversectl/internal/domain"
)

const minimal = `apiVersion: multiverse/v1alpha1
kind: Multiverse
metadata:
  name: acme
spec:
  repos:
    - name: api
      url: git@example.test:acme/api.git
  apps:
    - name: api
      repo: api
      compose: [docker-compose.yml]
      hosts:
        - name: api
          service: web
          port: 8000
`

func TestDecode(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{name: "minimal", yaml: minimal},
		{
			name:    "unknown apiVersion",
			yaml:    strings.Replace(minimal, "v1alpha1", "v9", 1),
			wantErr: `unsupported apiVersion "multiverse/v9"; supported: multiverse/v1alpha1`,
		},
		{
			name:    "unknown kind",
			yaml:    strings.Replace(minimal, "Multiverse", "Cluster", 1),
			wantErr: `unsupported kind "Cluster"`,
		},
		{
			name:    "unknown field",
			yaml:    strings.Replace(minimal, "compose:", "composee:", 1),
			wantErr: "composee",
		},
		{
			name:    "duplicate repo",
			yaml:    strings.Replace(minimal, "  apps:", "    - name: api\n      url: git@example.test:acme/other.git\n  apps:", 1),
			wantErr: `duplicate name "api"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decode([]byte(tt.yaml), "/ws")
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tt.wantErr != "" && err == nil:
				t.Fatalf("want error containing %q, got nil", tt.wantErr)
			case tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr):
				t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestDecodeAppliesDefaults(t *testing.T) {
	m, err := Decode([]byte(minimal), "/ws")
	if err != nil {
		t.Fatal(err)
	}
	if m.Dir != "/ws" || m.Domain != "auto" || m.Proxy.Kind != "traefik" || m.Proxy.Network != "multiversenet" {
		t.Errorf("manifest defaults not applied: %+v", m)
	}
	if got := m.Proxy.Entrypoints["web"]; got != 80 {
		t.Errorf("default web entrypoint = %d, want 80", got)
	}
	if got := m.Apps["api"].Hosts["api"].Entrypoint; got != "web" {
		t.Errorf("host entrypoint = %q, want web", got)
	}
}

func TestAnAppSaysWhatToRunOnceItsServicesAreUp(t *testing.T) {
	with := func(extra string) string {
		return strings.Replace(minimal, "      compose: [docker-compose.yml]\n", "      compose: [docker-compose.yml]\n      services: [web]\n"+extra, 1)
	}
	m, err := Decode([]byte(with("      hooks:\n        after:\n          - once: true\n            container:\n              service: web\n              command: [python, manage.py, createfixture]\n")), "/ws")
	if err != nil {
		t.Fatal(err)
	}
	got := m.Apps["api"].After
	if len(got) != 1 || got[0].Service != "web" || !got[0].Once || strings.Join(got[0].Command, " ") != "python manage.py createfixture" {
		t.Errorf("hooks = %+v", got)
	}
	for name, bad := range map[string]string{
		"no command":      "      hooks:\n        after:\n          - container:\n              service: web\n",
		"unknown service": "      hooks:\n        after:\n          - container:\n              service: db\n              command: [psql]\n",
		"a host command":  "      hooks:\n        after:\n          - host:\n              command: [make]\n",
		"the old name":    "      setup:\n        - command: [make]\n",
	} {
		if _, err := Decode([]byte(with(bad)), "/ws"); err == nil {
			t.Errorf("%s: a bad hook was accepted", name)
		}
	}
}

func TestDecodeExample(t *testing.T) {
	m, err := Load(filepath.Join("..", "..", "..", "examples", "multiverse.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "acme" || len(m.Repos) != 4 || len(m.Apps) != 4 {
		t.Errorf("unexpected example: name=%s repos=%d apps=%d", m.Name, len(m.Repos), len(m.Apps))
	}
	if got := m.Apps["api"].Hosts["devapi"].AlsoMatch; len(got) != 2 {
		t.Errorf("alsoMatch = %v", got)
	}
	// The example declares neither compose files nor services: the defaults
	// cover both, and that is the point of the shorter form.
	if got := m.Apps["api"].Compose; len(got) != 0 {
		t.Errorf("compose = %v, want unset", got)
	}
	if got := m.Apps["api"].Services; len(got) != 0 {
		t.Errorf("services = %v, want unset", got)
	}
	if got := m.Proxy.Entrypoints["devapi"]; got != 8001 {
		t.Errorf("devapi entrypoint = %d", got)
	}
}

func TestLocate(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Locate(nested); !errors.Is(err, domain.ErrNoManifest) {
		t.Fatalf("want ErrNoManifest, got %v", err)
	}
	want := filepath.Join(root, "multiverse.yml")
	if err := os.WriteFile(want, []byte(minimal), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Locate(nested)
	if err != nil || got != want {
		t.Fatalf("Locate = %q, %v; want %q", got, err, want)
	}
}

// A repository that no app names gets an app of its own, so the manifest no
// longer has to repeat the repository's name twice. Whether there is anything
// to run is for the files on disk to say, not the manifest.
func TestRepoWithoutAnAppGetsOne(t *testing.T) {
	m := decodeOrFail(t, `
apiVersion: multiverse/v1alpha1
kind: Multiverse
metadata:
  name: acme
spec:
  branch: dev/u{{universe}}
  repos:
    - name: worker
      url: git@github.com:acme/worker.git
    - name: notes
      url: git@github.com:acme/notes.git
    - name: mono
      url: git@github.com:acme/mono.git
  apps:
    - name: web
      repo: mono
    - name: jobs
      repo: mono
`)
	app, ok := m.Apps["worker"]
	if !ok || app.Repo != "worker" || !app.Derived {
		t.Fatalf("worker: got %+v, %v", app, ok)
	}
	if app, ok := m.Apps["notes"]; !ok || !app.Derived {
		t.Error("every repository gets an app; one with no compose file is skipped when acted on")
	}
	// mono is already named by two apps, so it does not get a third.
	if _, ok := m.Apps["mono"]; ok {
		t.Error("a repository an app already names must not get a derived app")
	}
	if got, want := m.DefaultBranchFor(3), "dev/u3"; got != want {
		t.Errorf("branch = %q, want %q", got, want)
	}
}

// Without a branch template a universe is still checked out on its own slug.
func TestDefaultBranchFallsBackToTheSlug(t *testing.T) {
	m := decodeOrFail(t, `
apiVersion: multiverse/v1alpha1
kind: Multiverse
metadata:
  name: acme
spec:
  repos:
    - name: api
      url: git@github.com:acme/api.git
`)
	if got, want := m.DefaultBranchFor(7), "u7"; got != want {
		t.Errorf("branch = %q, want %q", got, want)
	}
}

func decodeOrFail(t *testing.T, yaml string) *domain.Manifest {
	t.Helper()
	m, err := Decode([]byte(yaml), "/ws")
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestWriterOutputLoadsBack(t *testing.T) {
	data, err := Writer{}.Multiverse(app.ProjectDraft{
		Name: "acme",
		Repos: []app.DraftRepo{
			{Key: "api", URL: "https://example.test/acme/acme-api.git", Dir: "acme-api"},
			{Key: "web", URL: "git@example.test:acme/web.git", Dir: "web"},
			{Key: "docs", URL: "https://example.test/acme/docs.git", Dir: "docs"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	m, err := Decode(data, t.TempDir())
	if err != nil {
		t.Fatalf("%v\n%s", err, data)
	}
	if m.Name != "acme" || len(m.Repos) != 3 || m.Repos["api"].Dir != "acme-api" {
		t.Errorf("round trip lost something: %+v\n%s", m, data)
	}
	if got := strings.Join(m.RunnableApps(), ","); got != "api,docs,web" {
		t.Errorf("runnable apps = %s, want api,docs,web", got)
	}
	if strings.Contains(string(data), "dir:") {
		t.Errorf("a directory is written only when it differs from the repository's own name:\n%s", data)
	}
}

// A repository is cloned under its own name, as git would, unless the manifest
// says otherwise; the key it is known by in the manifest does not decide that.
func TestARepositoryIsClonedUnderItsOwnName(t *testing.T) {
	m := decodeOrFail(t, `
apiVersion: multiverse/v1alpha1
kind: Multiverse
metadata:
  name: acme
spec:
  repos:
    - {name: api, url: "https://github.com/acme/acme-api.git"}
    - {name: ui, url: "git@github.com:acme/acme-ui"}
    - {name: skills, url: "https://github.com/acme/.skills.git"}
    - {name: odd, url: "https://github.com/acme/other.git", dir: elsewhere}
`)
	for key, want := range map[string]string{"api": "acme-api", "ui": "acme-ui", "skills": ".skills", "odd": "elsewhere"} {
		if got := m.Repos[key].Dir; got != want {
			t.Errorf("%s is cloned into %q, want %q", key, got, want)
		}
	}
}

func TestAManifestHostCanAskForAPortForward(t *testing.T) {
	m := decodeOrFail(t, `
apiVersion: multiverse/v1alpha1
kind: Multiverse
metadata:
  name: acme
spec:
  repos:
    - {name: api, url: "https://github.com/acme/api.git"}
  apps:
    - name: api
      repo: api
      hosts:
        - {name: api, service: web, port: 8000, portForward: auto}
        - {name: docs, service: web, port: 8001}
`)
	hosts := m.Apps["api"].Hosts
	if hosts["api"].PortForward != "auto" || hosts["docs"].PortForward != "" {
		t.Errorf("hosts = %+v", hosts)
	}
}
