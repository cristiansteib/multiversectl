package cli

import (
	"context"
	"errors"
	"os"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/cristiansteib/multiversectl/internal/app"
	"github.com/cristiansteib/multiversectl/internal/domain"
)

// A command that is missing something it needs asks for it, with a list to
// choose from, instead of failing. Without a terminal to ask on there is
// nobody to ask, so it fails with the usage, as it would have.

// asked turns "no terminal to ask on" into what the command needed.
func asked(cmd *cobra.Command, args []string, err error) error {
	if errors.Is(err, app.ErrNotInteractive) {
		return argError(cmd, args)
	}
	return err
}

// universeArgs is the universe to act on: the first argument, or the one the
// person picks from the multiverse's.
func (r runner) universeArgs(cmd *cobra.Command, args []string, s *app.Service) ([]string, error) {
	if len(args) > 0 {
		return args, nil
	}
	states, err := s.Store.List()
	if err != nil {
		return nil, err
	}
	if len(states) == 0 {
		return nil, errors.New("no universes yet — create one with: `multiversectl universe new`")
	}
	opts := make([]app.Option, len(states))
	for i, st := range states {
		note := joinPairs(st.Branches)
		if st.Note != "" {
			note += "   " + st.Note
		}
		opts[i] = app.Option{Label: st.Name, Note: note}
	}
	i, err := r.f.Prompter().Select("Which universe?", opts, 0)
	if err = asked(cmd, args, err); err != nil {
		return nil, err
	}
	return []string{states[i].Name}, nil
}

// newName is the universe to create: the argument, or the next number that is
// free, which steps over a number whose branch a removed universe left behind.
// A person can accept or change it; a script takes it.
func (r runner) newName(cmd *cobra.Command, args []string, s *app.Service) ([]string, error) {
	if len(args) > 0 {
		return args, nil
	}
	n, skipped, err := s.NextFree(cmd.Context())
	if err != nil {
		return nil, err
	}
	for _, sk := range skipped {
		r.out.Info("skipped %s", sk)
	}
	next := strconv.Itoa(n)
	name, err := r.f.Prompter().Input("Universe number or name  (a name becomes the new branch)", next)
	if errors.Is(err, app.ErrNotInteractive) {
		return []string{next}, nil
	}
	if err != nil {
		return nil, err
	}
	if name == "" {
		name = next
	}
	return []string{name}, nil
}

// textArg is a value the command needs: the argument, or what the person types.
func (r runner) textArg(cmd *cobra.Command, args []string, label, def string) (string, error) {
	if len(args) > 0 {
		return args[0], nil
	}
	text, err := r.f.Prompter().Input(label, def)
	if err = asked(cmd, args, err); err != nil {
		return "", err
	}
	if text == "" {
		return "", argError(cmd, args)
	}
	return text, nil
}

// nodeArg is an entry of the catalog: the argument, or the one the person
// picks from a drawing of the tree. The root is not offered: it is the catalog
// itself.
func (r runner) nodeArg(cmd *cobra.Command, args []string, c *app.Catalogs, label string) (string, error) {
	if len(args) > 0 {
		return args[0], nil
	}
	tree, err := c.TreeCached(cmd.Context())
	if err != nil {
		return "", err
	}
	rows := tree.Rows()[1:] // not the root: it is the catalog itself
	if len(rows) == 0 {
		return "", errors.New("the catalog has nothing in it yet: `multiversectl catalog add <owner/repo>`")
	}
	opts := make([]app.Option, len(rows))
	for i, row := range rows {
		note := string(row.Node.Kind)
		if row.Node.Source.Repo != "" {
			note += "  " + shortRepoName(row.Node.Source.Repo)
		}
		opts[i] = app.Option{Label: row.Prefix + row.Node.Name, Note: note, Disabled: row.Node.Unavailable != ""}
		if row.Node.Unavailable != "" {
			opts[i].Note = row.Node.Unavailable
		}
	}
	i, err := r.f.Prompter().Select(label, opts, 0)
	if err = asked(cmd, args, err); err != nil {
		return "", err
	}
	return rows[i].Node.Path, nil
}

func shortRepoName(repo string) string {
	home, _ := os.UserHomeDir()
	return domain.Locate(repo, "").Short(home)
}

// multiverseArg is an installed multiverse: the argument, or the one picked.
func (r runner) multiverseArg(cmd *cobra.Command, args []string, bare *app.Service, label string) (string, error) {
	if len(args) > 0 {
		return args[0], nil
	}
	list, err := bare.Multiverses()
	if err != nil {
		return "", err
	}
	if len(list) == 0 {
		return "", errors.New("nothing is installed: `multiversectl start <owner/repo>`")
	}
	opts := make([]app.Option, len(list))
	def := 0
	for i, w := range list {
		note := firstNonEmpty(w.From, w.Root)
		opts[i] = app.Option{Label: w.Name, Note: note}
	}
	i, err := r.f.Prompter().Select(label, opts, def)
	if err = asked(cmd, args, err); err != nil {
		return "", err
	}
	return list[i].Name, nil
}

// spin runs fn under a spinner when show is set and there is a terminal to draw
// on. fn must not print.
func (r runner) spin(show bool, label string, fn func() error) error {
	if !show || r.f.Progress == nil {
		return fn()
	}
	return r.f.Progress().Do(label, func(context.Context) error { return fn() })
}
