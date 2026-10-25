package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

func TestRenderTree(t *testing.T) {
	root := domain.Node{Name: "me", Kind: domain.CatalogNode, Source: domain.Source{Repo: "git@example.test:me/index.git"}, Children: []domain.Node{
		{Name: "acme", Path: "acme", Kind: domain.CatalogNode, Source: domain.Source{Repo: "git@example.test:acme/index.git"}, Children: []domain.Node{
			{Name: "payments", Path: "acme/payments", Kind: domain.WorkspaceNode,
				Source: domain.Source{Repo: "git@example.test:acme/pay.git", Ref: "v1", Path: "multiverse.yaml"}},
		}},
		{Name: "private", Path: "private", Kind: domain.CatalogNode, Unavailable: "no access or not found: acme/private"},
	}}
	var buf bytes.Buffer
	r := runner{out: NewOutput(&buf, false)}
	got := strings.Join(r.renderTree(root, root.Source.Repo, map[string]string{"acme/payments": "payments"}), "\n")
	for _, want := range []string{"me  ", "├─ acme", "│  └─ payments", "└─ private",
		"acme/pay@v1", "✓ installed as payments", "✗ no access or not found: acme/private", "acme/index"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
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
