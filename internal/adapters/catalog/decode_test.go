package catalog

import (
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

const valid = `apiVersion: multiverse/v1alpha1
kind: Catalog
metadata:
  name: me
spec:
  catalogs:
    - name: acme
      source: {repo: git@example.test:acme/index.git, ref: main}
    - name: personal
      source: {path: catalogs/personal.yaml}
  workspaces:
    - name: fichero
      source: {repo: git@example.test:me/fichero.git}
`

func TestDecode(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{name: "valid", yaml: valid},
		{name: "empty spec", yaml: "apiVersion: multiverse/v1alpha1\nkind: Catalog\nmetadata: {name: me}\n"},
		{name: "workspace file", yaml: strings.Replace(valid, "Catalog", "Workspace", 1), wantErr: `unsupported kind "Workspace"`},
		{name: "version", yaml: strings.Replace(valid, "v1alpha1", "v9", 1), wantErr: "unsupported apiVersion"},
		{name: "unknown field", yaml: strings.Replace(valid, "ref:", "rev:", 1), wantErr: "field rev not found"},
		{name: "bad name", yaml: strings.Replace(valid, "name: acme", "name: Acme", 1), wantErr: `name "Acme" must match`},
		{name: "duplicate across lists", yaml: strings.Replace(valid, "name: fichero", "name: acme", 1), wantErr: `"acme" is used twice`},
		{name: "ref without repo", yaml: strings.Replace(valid, "{path: catalogs/personal.yaml}", "{ref: main}", 1), wantErr: "source.ref needs source.repo"},
		{name: "escapes repo", yaml: strings.Replace(valid, "catalogs/personal.yaml", "../other.yaml", 1), wantErr: "must stay inside the repository"},
		{name: "absolute path", yaml: strings.Replace(valid, "catalogs/personal.yaml", "/etc/x.yaml", 1), wantErr: "must stay inside the repository"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decoder{}.Decode([]byte(tt.yaml))
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestDecodeDefaultsPaths(t *testing.T) {
	c, err := Decoder{}.Decode([]byte(valid))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Catalogs[0].Source.Path; got != DefaultCatalogPath {
		t.Errorf("catalog path = %q", got)
	}
	if got := c.Workspaces[0].Source.Path; got != DefaultWorkspacePath {
		t.Errorf("workspace path = %q", got)
	}
}

func TestEditorKeepsComments(t *testing.T) {
	in := "# my index\n" + valid + "# trailing note\n"
	out, err := Editor{}.AddWorkspaceRef([]byte(in), domainWS("orders", "git@example.test:me/orders.git", "main", "multiverse.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	for _, want := range []string{"# my index", "# trailing note", "name: orders", "repo: git@example.test:me/orders.git", "ref: main"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "path: multiverse.yaml") {
		t.Errorf("the default path should be left out:\n%s", got)
	}
	if _, err := (Decoder{}).Decode(out); err != nil {
		t.Errorf("edited catalog no longer decodes: %v\n%s", err, got)
	}
}

func TestEditorFillsAnEmptyScaffold(t *testing.T) {
	e := Editor{}
	out, err := e.AddCatalogRef(e.Scaffold("me"), domainCat("personal", "", "", "catalogs/personal.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	out, err = e.AddWorkspaceRef(out, domainWS("fichero", "git@example.test:me/fichero.git", "", "multiverse.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	c, err := Decoder{}.Decode(out)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if len(c.Catalogs) != 1 || len(c.Workspaces) != 1 || c.Catalogs[0].Source.Path != "catalogs/personal.yaml" {
		t.Errorf("unexpected catalog: %+v\n%s", c, out)
	}
	if strings.Contains(string(out), "{}") || strings.Contains(string(out), "[]") {
		t.Errorf("flow-style leftovers:\n%s", out)
	}
}

func TestEditorRefusesDuplicatesAndRemoves(t *testing.T) {
	e := Editor{}
	if _, err := e.AddWorkspaceRef([]byte(valid), domainWS("acme", "git@example.test:x.git", "", "multiverse.yaml")); err == nil {
		t.Error("a name already used by a catalog entry must be refused")
	}
	out, err := e.RemoveRef([]byte(valid), "fichero")
	if err != nil {
		t.Fatal(err)
	}
	if c, err := (Decoder{}).Decode(out); err != nil || len(c.Workspaces) != 0 || len(c.Catalogs) != 2 {
		t.Errorf("after remove: %+v err=%v\n%s", c, err, out)
	}
	if _, err := e.RemoveRef([]byte(valid), "nope"); err == nil {
		t.Error("removing an unknown name must fail")
	}
}

func TestWorkspaceName(t *testing.T) {
	name, err := Editor{}.WorkspaceName([]byte("apiVersion: multiverse/v1alpha1\nkind: Workspace\nmetadata:\n  name: acme\n"))
	if err != nil || name != "acme" {
		t.Fatalf("name=%q err=%v", name, err)
	}
	if _, err := (Editor{}).WorkspaceName([]byte(valid)); err == nil {
		t.Error("a Catalog is not a workspace manifest")
	}
}

func domainWS(name, repo, ref, path string) domain.WorkspaceRef {
	return domain.WorkspaceRef{Name: name, Source: domain.Source{Repo: repo, Ref: ref, Path: path}}
}

func domainCat(name, repo, ref, path string) domain.CatalogRef {
	return domain.CatalogRef{Name: name, Source: domain.Source{Repo: repo, Ref: ref, Path: path}}
}
