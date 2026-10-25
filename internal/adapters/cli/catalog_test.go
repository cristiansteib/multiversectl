package cli

import (
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

func TestRenderTree(t *testing.T) {
	root := domain.Node{Name: "me", Kind: domain.CatalogNode, Children: []domain.Node{
		{Name: "acme", Path: "acme", Kind: domain.CatalogNode, Children: []domain.Node{
			{Name: "payments", Path: "acme/payments", Kind: domain.WorkspaceNode,
				Source: domain.Source{Repo: "git@example.test:acme/pay.git", Ref: "v1", Path: "multiverse.yaml"}},
		}},
		{Name: "private", Path: "private", Kind: domain.CatalogNode, Unavailable: "clone failed"},
	}}
	got := strings.Join(renderTree(root), "\n")
	for _, want := range []string{"me\n", "acme", "payments", "git@example.test:acme/pay.git@v1 multiverse.yaml", "unavailable: clone failed"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if bad := unavailable(root); len(bad) != 1 || bad[0].Path != "private" {
		t.Errorf("unavailable = %+v", bad)
	}
}
