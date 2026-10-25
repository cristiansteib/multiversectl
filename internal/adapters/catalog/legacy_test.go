package catalog

import (
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// A catalog written before projects were called that must keep working, and must
// not be half moved to the new name by an edit.
const legacy = `apiVersion: multiverse/v1alpha1
kind: Catalog
metadata:
  name: me
spec:
  workspaces:
    - name: old
      source: {repo: "git@example.test:me/old.git"}
`

func TestACatalogThatSaysWorkspacesStillDecodes(t *testing.T) {
	c, err := Decoder{}.Decode([]byte(legacy))
	if err != nil || len(c.Workspaces) != 1 || c.Workspaces[0].Name != "old" {
		t.Fatalf("%+v, %v", c, err)
	}
	both := strings.Replace(legacy, "  workspaces:", "  projects:\n    - name: new\n      source: {repo: \"git@example.test:me/new.git\"}\n  workspaces:", 1)
	c, err = Decoder{}.Decode([]byte(both))
	if err != nil || len(c.Workspaces) != 2 {
		t.Fatalf("a file may use both while it is moved: %+v, %v", c, err)
	}
	dup := strings.Replace(both, "name: new", "name: old", 1)
	if _, err := (Decoder{}).Decode([]byte(dup)); err == nil || !strings.Contains(err.Error(), "used twice") {
		t.Errorf("names stay unique across both lists: %v", err)
	}
}

func TestAnEditAddsToTheListTheFileAlreadyUses(t *testing.T) {
	ref := domain.WorkspaceRef{Name: "added", Source: domain.Source{Repo: "git@example.test:me/added.git"}}
	out, err := Editor{}.AddWorkspaceRef([]byte(legacy), ref)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(out); !strings.Contains(got, "workspaces:") || strings.Contains(got, "projects:") {
		t.Errorf("a file that says workspaces must not be half moved:\n%s", got)
	}
	fresh, err := Editor{}.AddWorkspaceRef(Editor{}.Scaffold("me"), ref)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(fresh); !strings.Contains(got, "projects:") || strings.Contains(got, "workspaces:") {
		t.Errorf("a new list is called projects:\n%s", got)
	}
	if c, err := (Decoder{}).Decode(out); err != nil || len(c.Workspaces) != 2 {
		t.Errorf("%+v, %v", c, err)
	}
	removed, err := Editor{}.RemoveRef(out, "old")
	if err != nil {
		t.Fatal(err)
	}
	if c, _ := (Decoder{}).Decode(removed); len(c.Workspaces) != 1 || c.Workspaces[0].Name != "added" {
		t.Errorf("removing from the old list: %+v", c)
	}
}

func TestAManifestThatSaysWorkspaceIsStillAProject(t *testing.T) {
	for _, kind := range []string{"Workspace", "Project"} {
		name, err := Editor{}.WorkspaceName([]byte("apiVersion: multiverse/v1alpha1\nkind: " + kind + "\nmetadata:\n  name: acme\n"))
		if err != nil || name != "acme" {
			t.Errorf("kind %s: %q, %v", kind, name, err)
		}
	}
}
