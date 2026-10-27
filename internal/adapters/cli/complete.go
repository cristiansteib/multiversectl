package cli

import (
	"context"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// Completion reads only what is already on this machine: it has to answer at
// once, and a tab key must never start a clone or a fetch.

func (r runner) completeMultiverses(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	bare, err := r.f.Bare(*r.g, r.out)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	list, err := bare.Multiverses()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var out []string
	for _, w := range list {
		for _, candidate := range []string{w.Name, w.From} {
			if candidate != "" && strings.HasPrefix(candidate, toComplete) {
				out = append(out, candidate)
			}
		}
	}
	sort.Strings(out)
	return out, cobra.ShellCompDirectiveNoFileComp
}

// completeNodes offers the paths in the root catalog as last fetched.
// onlyCatalogs is for --at, which names a place and not a multiverse; "/" is the
// root.
func (r runner) completeNodes(onlyCatalogs bool) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		directive := cobra.ShellCompDirectiveNoFileComp
		if !onlyCatalogs && cmd.Name() == "install" {
			directive = cobra.ShellCompDirectiveDefault // an install source can also be a file
		}
		c, err := r.catalogs(false)
		if err != nil {
			return nil, directive
		}
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		tree, err := c.TreeCached(ctx)
		if err != nil {
			return nil, directive
		}
		var out []string
		if onlyCatalogs && strings.HasPrefix("/", toComplete) {
			out = append(out, "/")
		}
		for _, row := range tree.Rows() {
			n := row.Node
			if n.Path == "" || n.Unavailable != "" || (onlyCatalogs && n.Kind != domain.CatalogNode) {
				continue
			}
			if strings.HasPrefix(n.Path, toComplete) {
				out = append(out, n.Path)
			}
		}
		return out, directive
	}
}
