package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

func (r runner) catalogCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "catalog",
		Short: "Browse the index of workspaces this machine can install",
	}
	cmd.AddCommand(r.catalogTreeCmd(), r.catalogSyncCmd(), r.catalogUseCmd())
	return cmd
}

func (r runner) catalogTreeCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "tree [git-url-or-file]",
		Short:   "Show the catalog as a tree; defaults to the root catalog",
		Example: "  multiversectl catalog tree\n  multiversectl catalog tree ./catalog.yaml",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.f.Catalogs(*r.g, r.out)
			if err != nil {
				return err
			}
			node, err := c.Tree(cmd.Context(), localOrURL(args), false)
			if err != nil {
				return err
			}
			for _, line := range renderTree(node) {
				r.out.Plain("%s", line)
			}
			return nil
		},
	}
}

func (r runner) catalogSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Fetch every repository the root catalog refers to, then show what is unavailable",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := r.f.Catalogs(*r.g, r.out)
			if err != nil {
				return err
			}
			node, err := c.Tree(cmd.Context(), "", true)
			if err != nil {
				return err
			}
			bad := unavailable(node)
			for _, n := range bad {
				r.out.Warn("%s: %s", n.Path, n.Unavailable)
			}
			r.out.OK("synced %d workspaces, %d unavailable", len(node.Leaves()), len(bad))
			return nil
		},
	}
}

func (r runner) catalogUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "use <git-url-or-file>",
		Short:   "Record the root catalog for this machine",
		Example: "  multiversectl catalog use git@github.com:me/multiverse-index.git",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.f.Catalogs(*r.g, r.out)
			if err != nil {
				return err
			}
			return c.Use(localOrURL(args))
		},
	}
}

// localOrURL makes a file argument absolute so the saved root still works
// from another directory; URLs and the empty argument pass through.
func localOrURL(args []string) string {
	if len(args) == 0 {
		return ""
	}
	if strings.Contains(args[0], "://") || strings.HasPrefix(args[0], "git@") {
		return args[0]
	}
	if abs, err := filepath.Abs(args[0]); err == nil {
		return abs
	}
	return args[0]
}

func renderTree(root domain.Node) []string {
	lines := []string{root.Name}
	var walk func(n domain.Node, depth int)
	walk = func(n domain.Node, depth int) {
		for _, c := range n.Children {
			line := fmt.Sprintf("%s%-*s %s", strings.Repeat("  ", depth), 24-2*depth, c.Name, c.Kind)
			switch {
			case c.Unavailable != "":
				line += "  unavailable: " + c.Unavailable
			case c.Kind == domain.WorkspaceNode:
				line += "  " + sourceLabel(c.Source)
			}
			lines = append(lines, "  "+strings.TrimRight(line, " "))
			walk(c, depth+1)
		}
	}
	walk(root, 0)
	return lines
}

func sourceLabel(s domain.Source) string {
	label := s.Repo
	if label == "" {
		return s.Path
	}
	if s.Ref != "" {
		label += "@" + s.Ref
	}
	return label + " " + s.Path
}

func unavailable(n domain.Node) []domain.Node {
	var out []domain.Node
	for _, c := range n.Children {
		if c.Unavailable != "" {
			out = append(out, c)
		}
		out = append(out, unavailable(c)...)
	}
	return out
}
