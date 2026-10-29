package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const multiverseOfThree = `apiVersion: multiverse/v1alpha1
kind: Multiverse
metadata: {name: acme}
spec:
  repos:
    - {name: api, url: "https://example.test/acme/api.git"}
    - {name: ui, url: "https://example.test/acme/ui.git"}
    - {name: docs, url: "https://example.test/acme/docs.git"}
`

func part(body string) string {
	return "apiVersion: multiverse/v1alpha1\nkind: Multiverse\nmetadata: {name: x}\nspec:\n" + body
}

func put(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestARepositorySaysWhatItRunsInItsOwnFile(t *testing.T) {
	root := t.TempDir()
	put(t, root, "api/.multiverse.yaml", part(`  proxy:
    entrypoints:
      - {name: devapi, port: 8001}
  apps:
    - services: [web, mailpit]
      secrets: [.env]
      optional:
        - {name: celery, services: [worker]}
`))
	m, err := Decode([]byte(multiverseOfThree), root)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Apps["api"].Derived || !m.Apps["ui"].Derived {
		t.Fatalf("before the merge each repository runs by convention: %+v", m.Apps)
	}
	if err := MergeParts(m, root); err != nil {
		t.Fatal(err)
	}
	api := m.Apps["api"]
	if api.Derived || api.Repo != "api" || len(api.Services) != 2 || api.Secrets[0] != ".env" || len(api.Optional["celery"]) != 1 {
		t.Errorf("api = %+v: want what its own file says", api)
	}
	if !m.Apps["ui"].Derived {
		t.Error("a repository with no file of its own stays as the multiverse describes it")
	}
	if m.Proxy.Entrypoints["devapi"] != 8001 || m.Proxy.Entrypoints["web"] != 80 {
		t.Errorf("entrypoints = %v", m.Proxy.Entrypoints)
	}
}

func TestARepositorysOwnFileSpeaksOnlyForItself(t *testing.T) {
	for name, tc := range map[string]struct{ body, want string }{
		"lists repositories":       {"  repos:\n    - {name: other, url: x}\n", "describes only itself"},
		"sets the whole domain":    {"  domain: example.test\n", "belong to the multiverse"},
		"names another repo":       {"  apps:\n    - {name: x, repo: ui}\n", "that repository's"},
		"takes another's app":      {"  apps:\n    - {name: ui}\n", `app "ui" is already defined for repo "ui"`},
		"two apps, one unnamed":    {"  apps:\n    - {services: [a]}\n    - {name: y}\n", "name is required"},
		"entrypoint on a new port": {"  proxy:\n    entrypoints:\n      - {name: web, port: 8080}\n", `"web" is 8080 here and 80 elsewhere`},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			put(t, root, "api/.multiverse.yaml", part(tc.body))
			m, err := Decode([]byte(multiverseOfThree), root)
			if err != nil {
				t.Fatal(err)
			}
			err = MergeParts(m, root)
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "repo api") {
				t.Fatalf("err = %v, want one saying %q and naming the repo", err, tc.want)
			}
		})
	}
}

func TestARepositoryCanAskForRenderedFilesInItsOwnFile(t *testing.T) {
	root := t.TempDir()
	put(t, root, "api/.multiverse.yaml", part(`  apps:
    - render:
        - {to: config.json, template: '{}'}
`))
	m, err := Decode([]byte(multiverseOfThree), root)
	if err != nil {
		t.Fatal(err)
	}
	if err := MergeParts(m, root); err != nil {
		t.Fatal(err)
	}
	if got := m.Apps["api"].Render; len(got) != 1 || got[0].To != "config.json" {
		t.Errorf("render = %+v", got)
	}
}

func TestAnAppOnDemandIsSaidInTheMultiverseOrInTheRepositorysOwnFile(t *testing.T) {
	root := t.TempDir()
	put(t, root, "api/.multiverse.yaml", part(`  apps:
    - autostart: false
`))
	m, err := Decode([]byte(multiverseOfThree+`  apps:
    - {name: ui, repo: ui, autostart: false}
    - {name: docs, repo: docs, autostart: true}
`), root)
	if err != nil {
		t.Fatal(err)
	}
	if err := MergeParts(m, root); err != nil {
		t.Fatal(err)
	}
	for app, want := range map[string]bool{"api": true, "ui": true, "docs": false} {
		if got := m.Apps[app].OnDemand; got != want {
			t.Errorf("%s: on demand = %v, want %v", app, got, want)
		}
	}
	if got := strings.Join(m.AutostartApps(), ","); got != "docs" {
		t.Errorf("an unqualified start runs %q, want docs", got)
	}
}

// A multiverse that declares the app its repository's own file also declares
// sets what it says on top of the repository's, rather than refusing to load.
func TestAMultiverseAppIsLayeredOverTheRepositorysOwn(t *testing.T) {
	root := t.TempDir()
	put(t, root, "api/.multiverse.yaml", part(`  apps:
    - name: api
      services: [web, worker]
      secrets: [.env]
      autostart: false
      env:
        - {name: LOG_LEVEL, value: info}
        - {name: WORKERS, value: "2"}
`))
	m, err := Decode([]byte(multiverseOfThree+`  apps:
    - name: api
      repo: api
      autostart: true
      env:
        - {name: LOG_LEVEL, value: debug}
      hosts:
        - {name: api, service: web, port: 8000}
`), root)
	if err != nil {
		t.Fatal(err)
	}
	if err := MergeParts(m, root); err != nil {
		t.Fatal(err)
	}
	api := m.Apps["api"]
	if len(api.Services) != 2 || len(api.Secrets) != 1 || api.Hosts["api"].Port != 8000 || api.OnDemand {
		t.Errorf("api = %+v: want the repository's services and secrets, the multiverse's hosts and autostart", api)
	}
	if api.Env["LOG_LEVEL"] != "debug" || api.Env["WORKERS"] != "2" {
		t.Errorf("env = %v: want the multiverse's value over the repository's, key by key", api.Env)
	}
}

// What the repository said about services and secrets the multiverse replaced
// goes with them, rather than making the layered app invalid.
func TestALayerDropsWhatOnlyTheReplacedListsNeeded(t *testing.T) {
	root := t.TempDir()
	put(t, root, "api/.multiverse.yaml", part(`  apps:
    - name: api
      services: [web, worker]
      defaultService: worker
      secrets: [.env]
      secretTemplates: {.env: env.sample}
`))
	m, err := Decode([]byte(multiverseOfThree+`  apps:
    - {name: api, repo: api, services: [web], secrets: [.env.local]}
`), root)
	if err != nil {
		t.Fatal(err)
	}
	if err := MergeParts(m, root); err != nil {
		t.Fatal(err)
	}
	if api := m.Apps["api"]; api.DefaultService != "" || len(api.SecretTemplates) != 0 {
		t.Errorf("api = %+v", api)
	}
}

// A universe reads the file from its own checkout and the shared clone only
// for a repository it has none of.
func TestAPartIsReadFromTheFirstDirectoryThatHasOne(t *testing.T) {
	root := t.TempDir()
	put(t, root, "worktree/api/.multiverse.yaml", part("  apps:\n    - services: [web]\n"))
	put(t, root, "clone/api/.multiverse.yaml", part("  apps:\n    - services: [old]\n"))
	put(t, root, "clone/ui/.multiverse.yaml", part("  apps:\n    - services: [front]\n"))
	m, err := Decode([]byte(multiverseOfThree), root)
	if err != nil {
		t.Fatal(err)
	}
	dirs := func(key string) []string {
		return []string{filepath.Join(root, "worktree", key), filepath.Join(root, "clone", key)}
	}
	if err := MergePartsFrom(m, dirs); err != nil {
		t.Fatal(err)
	}
	if got := m.Apps["api"].Services; len(got) != 1 || got[0] != "web" {
		t.Errorf("api read %v, want its worktree's file", got)
	}
	if got := m.Apps["ui"].Services; len(got) != 1 || got[0] != "front" {
		t.Errorf("ui read %v, want the clone's file as it has no checkout", got)
	}
}
