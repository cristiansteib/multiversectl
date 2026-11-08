package cli

import (
	"context"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cristiansteib/multiversectl/internal/app"
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

// completeUniverseThen completes a universe first and what follows it after:
// the apps (more says whether any number may be named, or just one), and
// nothing for the rest, which is a command for a container or free text and
// must not be offered this directory's files.
func (r runner) completeUniverseThen(apps, more bool) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		const none = cobra.ShellCompDirectiveNoFileComp
		s, err := r.f.Multiverse(*r.g, quietOut{})
		if err != nil {
			return nil, none
		}
		switch {
		case len(args) == 0:
			return universeCandidates(s, toComplete), none
		case apps && (more || len(args) == 1):
			return appCandidates(s, args[1:], toComplete), none
		}
		return nil, none
	}
}

// completeUniverse is for the commands that take a universe and nothing else
// worth completing.
func (r runner) completeUniverse(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return r.completeUniverseThen(false, false)(cmd, args, toComplete)
}

// universeCandidates are the universes, each with the branches it is on and
// what it is for, so the right one can be told apart without an `ls`.
func universeCandidates(s *app.Service, toComplete string) []string {
	states, err := s.Store.List()
	if err != nil {
		return nil
	}
	var out []string
	for _, st := range states {
		if !strings.HasPrefix(st.Name, toComplete) {
			continue
		}
		note := joinPairs(st.Branches)
		if st.Note != "" {
			note += "   " + st.Note
		}
		out = append(out, st.Name+"\t"+truncate(note, 60))
	}
	return out
}

// appCandidates are the manifest's apps, leaving out the ones already named.
func appCandidates(s *app.Service, named []string, toComplete string) []string {
	var out []string
	for _, name := range sortedKeys(s.Manifest.Apps) {
		if strings.HasPrefix(name, toComplete) && !slices.Contains(named, name) {
			out = append(out, name)
		}
	}
	return out
}

// completeServices is for --service: those of the app the command already has
// on its line (the universe and the app, in that order).
func (r runner) completeServices(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	const none = cobra.ShellCompDirectiveNoFileComp
	if len(args) < 2 {
		return nil, none
	}
	s, err := r.f.Multiverse(*r.g, quietOut{})
	if err != nil {
		return nil, none
	}
	var out []string
	for _, name := range s.ServiceNames(context.Background(), args[0], args[1]) {
		if strings.HasPrefix(name, toComplete) {
			out = append(out, name)
		}
	}
	return out, none
}

// completeGroups is for --with, a comma separated list: the optional service
// groups any app defines, offered after whatever has already been typed up to
// the last comma.
func (r runner) completeGroups(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	const none = cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace
	s, err := r.f.Multiverse(*r.g, quietOut{})
	if err != nil {
		return nil, none
	}
	head, tail := "", toComplete
	if i := strings.LastIndex(toComplete, ","); i >= 0 {
		head, tail = toComplete[:i+1], toComplete[i+1:]
	}
	seen := map[string]bool{}
	for _, a := range s.Manifest.Apps {
		for group := range a.Optional {
			seen[group] = true
		}
	}
	var out []string
	for _, group := range sortedKeys(seen) {
		if strings.HasPrefix(group, tail) && !strings.Contains(","+head, ","+group+",") {
			out = append(out, head+group)
		}
	}
	return out, none
}
