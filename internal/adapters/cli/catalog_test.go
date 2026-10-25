package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

func TestRenderTree(t *testing.T) {
	t.Setenv("HOME", "/home/me")
	root := domain.Node{Name: "index", Kind: domain.CatalogNode, Source: domain.Source{Repo: "https://github.com/me/index.git", Path: "catalog.yaml"}, Children: []domain.Node{
		// The same repository as the root, told apart by its file.
		{Name: "personal", Path: "personal", Kind: domain.CatalogNode, Source: domain.Source{Repo: "https://github.com/me/index.git", Path: "catalogs/personal.yaml"}, Children: []domain.Node{
			{Name: "notes", Path: "personal/notes", Kind: domain.WorkspaceNode, Source: domain.Source{Path: "/home/me/notes/multiverse.yaml"}},
		}},
		{Name: "acme", Path: "acme", Kind: domain.CatalogNode, Source: domain.Source{Repo: "git@gitlab.com:acme/index.git", Path: "catalog.yaml"}, Children: []domain.Node{
			{Name: "payments", Path: "acme/payments", Kind: domain.WorkspaceNode,
				Source: domain.Source{Repo: "git@gitlab.com:acme/pay.git", Ref: "v1", Path: "multiverse.yaml"}},
		}},
		{Name: "private", Path: "private", Kind: domain.CatalogNode, Unavailable: "no access or not found: github:acme/private"},
	}}
	var buf bytes.Buffer
	r := runner{out: NewOutput(&buf, false)}
	got := strings.Join(r.renderTree(root, map[string]string{"acme/payments": "payments"}), "\n")
	for _, want := range []string{
		"index", "root", "github:me/index",
		"├─ personal", "github:me/index catalogs/personal.yaml", // same repository as the root, and the file says which catalog
		"file:~/notes/multiverse.yaml",
		"gitlab:acme/index", "gitlab:acme/pay@v1", "project", "✓ installed as payments",
		"✗ no access or not found: github:acme/private",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "https://") || strings.Contains(got, "git@") {
		t.Errorf("a URL is how it was written, not where it is:\n%s", got)
	}
}

// A command added without a group would land under "Additional Commands",
// outside the three places the help is organised into.
func TestEveryCommandIsInAGroup(t *testing.T) {
	root, _ := New(Factory{})
	known := map[string]bool{}
	for _, g := range root.Groups() {
		known[g.ID] = true
	}
	for _, c := range root.Commands() {
		if c.Hidden {
			continue
		}
		if !known[c.GroupID] {
			t.Errorf("%s has group %q; add it to commandGroups", c.Name(), c.GroupID)
		}
	}
}
