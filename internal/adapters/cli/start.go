package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cristiansteib/multiversectl/internal/app"
)

// startCmd is the whole of getting going, in one command.
//
// Everything it does can be done with `universe new` and `universe up` once the
// multiverse is installed, and those stay the commands for doing one of those
// things.
// This is for the first five minutes, when somebody has been handed a
// repository and wants to see the thing run before learning what a universe
// is: one command, each step announced as it happens, and an address at the
// end.
func (r runner) startCmd() *cobra.Command {
	var base, universe string
	var open, yes bool
	cmd := &cobra.Command{
		Use:   "start [source]",
		Short: "Install a multiverse, make a universe and start it, in one command",
		Long: "From nothing to something running. The source is read in this order:\n" +
			"  1. a node in your catalog\n" +
			"  2. a path on this machine: a multiverse file, a catalog, or a directory\n" +
			"  3. a git URL\n" +
			"  4. owner/repo, meaning GitHub; owner/repo:name picks one multiverse of a\n" +
			"     repository that defines several (multiverse.yaml, multiverse/*.yaml)\n" +
			"A multiverse already installed is used as it is. With no source, the one\n" +
			"-m names or the folder you are in; failing that, you pick from your catalog.\n\n" +
			"Then it picks the universe: the one you name, the only one there is, or a new\n" +
			"one when the multiverse has none. It creates it if it has to, starts it, and\n" +
			"prints the addresses it answers on.",
		Example: "  multiversectl start acme/multiverse\n" +
			"  multiversectl start acme/multiverse --open\n" +
			"  multiversectl start acme/platform:billing # one of several multiverses in a repository\n" +
			"  multiversectl start                      # the multiverse you are in, or pick one\n" +
			"  multiversectl start -u 3",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: r.completeNodes(false),
	}
	cmd.Flags().StringVar(&base, "base", "", "directory multiverses go under (default $MULTIVERSECTL_ROOT or ~/multiverse)")
	cmd.Flags().StringVarP(&universe, "universe", "u", "", "the universe to start (default: the only one, or a new one)")
	cmd.Flags().BoolVar(&open, "open", false, "open the first address in a browser")
	cmd.Flags().BoolVar(&yes, "yes", false, "accept every default and confirmation")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		s, err := r.startProject(cmd, args, base, yes)
		if err != nil {
			return err
		}
		name, err := r.startUniverse(cmd, s, universe)
		if err != nil {
			return err
		}
		if _, err := s.Resolve(name); err != nil {
			r.out.Info("universe `%s`: a worktree of every repository", name)
			if _, err := s.Create(ctx, app.CreateInput{Name: name}); err != nil {
				return err
			}
		}
		r.out.Info("starting universe `%s`", name)
		if err := s.Start(ctx, app.StartInput{Universe: name}); err != nil {
			return err
		}
		return r.startDone(ctx, s, name, open)
	}
	return cmd
}

// startProject is the multiverse to act on: the source installed when one was
// given, else the one -m names or the folder says, else one picked from the
// catalog.
func (r runner) startProject(cmd *cobra.Command, args []string, base string, yes bool) (*app.Service, error) {
	if len(args) == 0 {
		s, err := r.f.Multiverse(*r.g, r.out)
		if err == nil || r.g.Multiverse != "" || r.g.Manifest != "" {
			return s, err
		}
		// Nothing says which one: what the catalog lists is the menu. A
		// machine with no catalog has nothing to offer, so it is told what to
		// type, not what could not be resolved.
		c, cerr := r.catalogs(yes)
		if cerr != nil {
			return nil, cerr
		}
		cfg, cerr := c.Config.Load()
		if cerr != nil || cfg.Catalog == "" {
			return nil, errors.New("nothing is installed yet — say what to start: `multiversectl start <owner/repo>`  (or `multiversectl init` for your own catalog to pick from)")
		}
	}
	// Where it goes is asked only when nothing has said, as `start`
	// does it.
	ask := !yes && r.g.Root == "" && base == "" && os.Getenv("MULTIVERSECTL_ROOT") == ""
	r.quietHints = true // it is about to make the universe and start it
	planned, err := r.install(cmd, args, app.InstallOptions{
		Base: firstNonEmpty(base, os.Getenv("MULTIVERSECTL_ROOT"), "~/multiverse"),
		Root: r.g.Root,
	}, yes, ask)
	if err != nil {
		return nil, err
	}
	if len(planned) == 0 {
		return nil, errors.New("nothing was picked to install")
	}
	// The first of them, whether it was installed just now or was already
	// here: a source naming several multiverses starts the one it named first.
	bare, err := r.f.Bare(*r.g, r.out)
	if err != nil {
		return nil, err
	}
	w, err := bare.MultiverseNamed(planned[0].Name)
	if err != nil {
		return nil, err
	}
	return r.f.At(*r.g, r.out, w.Root, w.Manifest)
}

// startUniverse is which universe to start: the one named, the only one, or a
// new one. Several and nothing named is a question, and a terminal is asked.
func (r runner) startUniverse(cmd *cobra.Command, s *app.Service, named string) (string, error) {
	if named != "" {
		return named, nil
	}
	states, err := s.Store.List()
	if err != nil {
		return "", err
	}
	switch len(states) {
	case 0:
		n, skipped, err := s.NextFree(cmd.Context())
		if err != nil {
			return "", err
		}
		for _, sk := range skipped {
			r.out.Info("skipped %s", sk)
		}
		return strconv.Itoa(n), nil
	case 1:
		return states[0].Name, nil
	}
	// Not universeArgs: that answers for commands whose universe is an
	// argument, and says "pass the argument" when there is no terminal. Here
	// it is a flag, and saying so is the whole of the help.
	opts := make([]app.Option, len(states))
	names := make([]string, len(states))
	for i, st := range states {
		opts[i] = app.Option{Label: st.Name, Note: joinPairs(st.Branches)}
		names[i] = st.Name
	}
	i, err := r.f.Prompter().Select("Which universe should start?", opts, 0)
	if errors.Is(err, app.ErrNotInteractive) {
		return "", fmt.Errorf("there are %d universes — say which one: `multiversectl start -u <universe>` (%s)",
			len(states), strings.Join(names, ", "))
	}
	if err != nil {
		return "", err
	}
	return states[i].Name, nil
}

// startDone prints what to open, which is the point of the whole command.
func (r runner) startDone(ctx context.Context, s *app.Service, name string, open bool) error {
	list, err := s.List(ctx, app.ListOptions{})
	if err != nil {
		return err
	}
	var urls []string
	for _, u := range list {
		if u.Name != name {
			continue
		}
		for _, a := range u.Apps {
			for _, h := range a.URLs {
				urls = append(urls, h.URL)
			}
		}
	}
	r.out.OK("universe `%s` is up", name)
	for _, u := range urls {
		r.out.Plain("  %s", u)
	}
	if len(urls) == 0 {
		r.out.Info("it serves no address; `multiversectl universe ls -o wide` says what is running")
		return nil
	}
	if open {
		openBrowser(ctx, urls[0])
	}
	r.out.Info("next: `multiversectl universe ls`, or `multiversectl universe new` for a second one")
	return nil
}
