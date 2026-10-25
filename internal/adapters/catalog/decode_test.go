package catalog

import (
	"strings"
	"testing"
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
