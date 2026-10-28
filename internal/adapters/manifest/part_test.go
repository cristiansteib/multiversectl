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
