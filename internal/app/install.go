package app

import (
	"errors"
	"fmt"
	"strings"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// Leaves picks the workspaces to install: everything under each path, which
// may name a single workspace or a whole subtree, or a choice from the tree
// when no path is given. Unavailable layers are skipped, with a warning, so
// one private catalog this machine cannot reach does not block the rest.
func (c *Catalogs) Leaves(tree domain.Node, paths []string) ([]domain.Node, error) {
	for _, n := range unreadable(tree) {
		c.Out.Warn("%s is unavailable: %s", n.Path, n.Unavailable)
	}
	var picked []domain.Node
	if len(paths) == 0 {
		all := tree.Leaves()
		if len(all) == 0 {
			return nil, errors.New("the catalog lists no workspaces yet; add one with `catalog add workspace`")
		}
		labels := make([]string, len(all))
		for i, n := range all {
			labels[i] = n.Path
		}
		idx, err := c.Prompt.MultiSelect("Which workspaces should be installed?", labels)
		if errors.Is(err, ErrNotInteractive) {
			return nil, fmt.Errorf("%w (name them: install <path>...)", err)
		}
		if err != nil {
			return nil, err
		}
		for _, i := range idx {
			picked = append(picked, all[i])
		}
	}
	for _, p := range paths {
		n, ok := tree.Find(strings.Trim(p, "/"))
		if !ok {
			return nil, fmt.Errorf("nothing at %q in the catalog; see `catalog tree`", p)
		}
		if n.Unavailable != "" {
			return nil, fmt.Errorf("%q is unavailable: %s", p, n.Unavailable)
		}
		leaves := n.Leaves()
		if len(leaves) == 0 {
			return nil, fmt.Errorf("%q has no workspaces under it", p)
		}
		picked = append(picked, leaves...)
	}
	return dedupe(picked), nil
}

func dedupe(nodes []domain.Node) []domain.Node {
	seen := map[string]bool{}
	out := nodes[:0:0]
	for _, n := range nodes {
		if !seen[n.Path] {
			seen[n.Path] = true
			out = append(out, n)
		}
	}
	return out
}

func unreadable(n domain.Node) []domain.Node {
	var out []domain.Node
	for _, ch := range n.Children {
		if ch.Unavailable != "" {
			out = append(out, ch)
		}
		out = append(out, unreadable(ch)...)
	}
	return out
}

// WorkspacePath is the directory a catalog path installs into. A segment that
// repeats the one before it is dropped, so a project called fichero whose
// workspace is also called fichero lands in fichero/, not fichero/fichero/.
func WorkspacePath(path string) string {
	var out []string
	for _, seg := range strings.Split(path, "/") {
		if len(out) == 0 || out[len(out)-1] != seg {
			out = append(out, seg)
		}
	}
	return strings.Join(out, "/")
}

// WorkspaceSlug turns a catalog path into the name a workspace is recorded
// under: acme/backend/payments becomes acme-backend-payments.
func WorkspaceSlug(path string) string { return strings.ReplaceAll(WorkspacePath(path), "/", "-") }
