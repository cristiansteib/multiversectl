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
kind: Project
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
			yaml:    strings.Replace(minimal, "Project", "Cluster", 1),
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
	if got := m.Repos["api"].Runtime; got != domain.RuntimeCompose {
		t.Errorf("repo runtime = %q, want compose", got)
	}
	if got := m.Proxy.Entrypoints["web"]; got != 80 {
		t.Errorf("default web entrypoint = %d, want 80", got)
	}
	if got := m.Apps["api"].Hosts["api"].Entrypoint; got != "web" {
		t.Errorf("host entrypoint = %q, want web", got)
	}
}

func TestDecodeExample(t *testing.T) {
	m, err := Load(filepath.Join("..", "..", "..", "examples", "multiverse.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "acme" || len(m.Repos) != 4 || len(m.Apps) != 3 {
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

// A repository that runs something and that no app names gets an app of its
// own, so the manifest no longer has to repeat the repository's name twice.
func TestRepoWithoutAnAppGetsOne(t *testing.T) {
	m := decodeOrFail(t, `
apiVersion: multiverse/v1alpha1
kind: Project
metadata:
  name: acme
spec:
  branch: dev/u{{universe}}
  repos:
    - name: worker
      url: git@github.com:acme/worker.git
    - name: notes
      url: git@github.com:acme/notes.git
      runtime: none
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
	if _, ok := m.Apps["notes"]; ok {
		t.Error("a repository with runtime none must stay unrunnable")
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
kind: Project
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
	data, err := Writer{}.Workspace(app.ProjectDraft{
		Name: "acme",
		Repos: []app.DraftRepo{
			{Key: "api", URL: "https://example.test/acme/acme-api.git", Dir: "acme-api"},
			{Key: "web", URL: "git@example.test:acme/web.git", Dir: "web"},
			{Key: "docs", URL: "https://example.test/acme/docs.git", Dir: "docs", NoRuntime: true},
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
	if got := strings.Join(m.RunnableApps(), ","); got != "api,web" {
		t.Errorf("runnable apps = %s, want api,web (docs has no runtime)", got)
	}
}
