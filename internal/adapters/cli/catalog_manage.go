package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cristiansteib/multiversectl/internal/app"
	"github.com/cristiansteib/multiversectl/internal/domain"
)

// catalogManageCmd is a menu over what the catalog commands do, for whoever does
// not remember them: each choice is also a command of its own, and the menu says
// which, so it teaches as it goes.
func (r runner) catalogManageCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "manage",
		Short: "Manage the catalog with a menu: install, ignore, add, remove, update",
		Long: "A menu over the catalog, one choice at a time, until you quit:\n\n" +
			"  install    what the catalog lists and is not installed here (a multiverse, or\n" +
			"             everything under a catalog, with a tick for each)\n" +
			"  ignore     a branch of the tree on this machine, or bring one back\n" +
			"  add, remove an entry, update what is installed, show the tree\n\n" +
			"It needs a terminal. Each choice is a command of its own, which it names when\n" +
			"it is done, so the same can be scripted.",
		Example: "  multiversectl catalog manage",
		Args:    cobra.NoArgs,
	}
	cmd.RunE = func(cmd *cobra.Command, _ []string) error { return r.manage(cmd) }
	return cmd
}

// manageChoice is one line of the menu: what it does, and the command it is.
type manageChoice struct {
	label, command string
	run            func(cmd *cobra.Command, c *app.Catalogs) error
}

func (r runner) manage(cmd *cobra.Command) error {
	choices := []manageChoice{
		{"Install what the catalog lists", "start <path> --install-only", r.manageInstall},
		{"Leave a branch of the tree out on this machine", "catalog ignore <path>", r.manageIgnore},
		{"Bring back what was left out", "catalog unignore <path>", r.manageUnignore},
		{"Add an entry", "catalog add <owner/repo>", func(cmd *cobra.Command, _ *app.Catalogs) error { return r.delegate(cmd, r.catalogAddCmd()) }},
		{"Remove an entry", "catalog rm <path>", func(cmd *cobra.Command, _ *app.Catalogs) error { return r.delegate(cmd, r.catalogRmCmd()) }},
		{"Bring what is installed up to date", "catalog update", func(cmd *cobra.Command, _ *app.Catalogs) error { return r.delegate(cmd, r.updateCmd()) }},
		{"Show the tree", "catalog list", func(cmd *cobra.Command, _ *app.Catalogs) error { return r.showTree(cmd, nil, false, "tree", "") }},
	}
	opts := make([]app.Option, 0, len(choices)+1)
	for _, ch := range choices {
		opts = append(opts, app.Option{Label: ch.label, Note: "multiversectl " + ch.command})
	}
	opts = append(opts, app.Option{Label: "Quit"})

	for {
		i, err := r.f.Prompter().Select("What do you want to do with the catalog?", opts, 0)
		switch {
		case errors.Is(err, app.ErrNotInteractive):
			return errors.New("catalog manage is a menu and needs a terminal; the same things one at a time: `catalog list`, `start <path> --install-only`, `catalog ignore <path>`, `catalog add`, `catalog rm <path>`, `catalog update`")
		case errors.Is(err, context.Canceled):
			return nil
		case err != nil:
			return err
		}
		if i >= len(choices) {
			return nil
		}
		// Built when a choice needs it, so the menu itself asks nothing of the machine.
		c, err := r.catalogs(false)
		if err == nil {
			err = choices[i].run(cmd, c)
		}
		if errors.Is(err, context.Canceled) {
			return nil
		}
		if err != nil {
			// One choice failing is not the end of the session.
			r.out.Warn("%v", err)
		}
	}
}

// delegate runs a command of the catalog as it would be run with no arguments:
// asking for what it needs, as it does by itself.
func (r runner) delegate(cmd *cobra.Command, sub *cobra.Command) error {
	sub.SetContext(cmd.Context())
	return sub.RunE(sub, nil)
}

// installable is what the catalog lists that has something not installed here:
// a multiverse that is not, a catalog with some of its multiverses not. What is
// left out on this machine or cannot be read is not offered.
func installable(tree domain.Node, installed map[string]string) []domain.TreeRow {
	var out []domain.TreeRow
	for _, row := range tree.Rows() {
		n := row.Node
		if n.Path == "" || n.Ignored || n.Unavailable != "" {
			continue
		}
		if len(notInstalled(n, installed)) > 0 {
			out = append(out, row)
		}
	}
	return out
}

// notInstalled is the multiverses under n that this machine does not have and may take.
func notInstalled(n domain.Node, installed map[string]string) []domain.Node {
	var out []domain.Node
	for _, l := range n.Leaves() {
		if _, ok := installed[l.Path]; !ok && !l.Ignored && l.Unavailable == "" {
			out = append(out, l)
		}
	}
	return out
}

func (r runner) manageInstall(cmd *cobra.Command, c *app.Catalogs) error {
	ctx := cmd.Context()
	tree, err := c.TreeCached(ctx)
	if err != nil {
		return err
	}
	installed, err := c.InstalledIn(tree)
	if err != nil {
		return err
	}
	rows := installable(tree, installed)
	if len(rows) == 0 {
		r.out.Info("everything the catalog lists is installed (or left out on this machine)")
		return nil
	}
	opts := make([]app.Option, len(rows))
	for i, row := range rows {
		n := row.Node
		note := "multiverse  ○ not installed"
		if n.Kind == domain.CatalogNode {
			have := len(n.Leaves()) - len(notInstalled(n, installed))
			note = fmt.Sprintf("catalog  %d/%d installed", have, len(n.Leaves()))
		}
		opts[i] = app.Option{Label: row.Prefix + n.Name, Note: note}
	}
	pick, err := r.f.Prompter().Select("Install which?", opts, 0)
	if err != nil {
		return err
	}
	node := rows[pick].Node

	// A catalog stands for several: say which, since each is a clone of its repositories.
	todo := notInstalled(node, installed)
	var exclude []string
	if node.Kind == domain.CatalogNode && len(todo) > 1 {
		boxes := make([]app.Option, len(todo))
		for i, l := range todo {
			boxes[i] = app.Option{Label: l.Name, Note: l.Path, Checked: true}
		}
		chosen, err := r.f.Prompter().MultiSelect("Which of these should be installed?  (nothing ticked: back)", boxes)
		if err != nil {
			return err
		}
		if len(chosen) == 0 {
			return nil
		}
		keep := map[int]bool{}
		for _, i := range chosen {
			keep[i] = true
		}
		var picked []domain.Node
		for i, l := range todo {
			if keep[i] {
				picked = append(picked, l)
			} else {
				exclude = append(exclude, l.Path)
			}
		}
		todo = picked
	}
	base := firstNonEmpty(os.Getenv("MULTIVERSECTL_ROOT"), "~/multiverse")
	ask := len(todo) == 1 && r.g.Root == "" && os.Getenv("MULTIVERSECTL_ROOT") == ""
	if _, err := r.install(cmd, []string{node.Path}, app.InstallOptions{Base: base, Root: r.g.Root, Exclude: exclude}, false, ask); err != nil {
		return err
	}
	same := "multiversectl start " + node.Path + " --install-only"
	for _, p := range exclude {
		same += " --exclude " + p
	}
	r.out.Info("the same, as a command: `%s`", same)
	return nil
}

func (r runner) manageIgnore(cmd *cobra.Command, c *app.Catalogs) error {
	tree, err := c.TreeCached(cmd.Context())
	if err != nil {
		return err
	}
	var rows []domain.TreeRow
	for _, row := range tree.Rows() {
		if row.Node.Path != "" && !row.Node.Ignored {
			rows = append(rows, row)
		}
	}
	if len(rows) == 0 {
		r.out.Info("the catalog has nothing in it to leave out")
		return nil
	}
	opts := make([]app.Option, len(rows))
	for i, row := range rows {
		note := string(row.Node.Kind)
		if row.Node.Unavailable != "" {
			note += "  ✗ " + row.Node.Unavailable
		}
		opts[i] = app.Option{Label: row.Prefix + row.Node.Name, Note: note}
	}
	pick, err := r.f.Prompter().Select("Leave which out on this machine?  (it is not read, so it costs no wait, and is not offered for installing)", opts, 0)
	if err != nil {
		return err
	}
	path := rows[pick].Node.Path
	if err := c.Ignore(cmd.Context(), []string{path}, true); err != nil {
		return err
	}
	r.out.OK("ignoring %s", path)
	r.out.Info("the same, as a command: `multiversectl catalog ignore %s`", path)
	return nil
}

func (r runner) manageUnignore(cmd *cobra.Command, c *app.Catalogs) error {
	tree, err := c.TreeCached(cmd.Context())
	if err != nil {
		return err
	}
	var rows []domain.TreeRow
	for _, row := range tree.Rows() {
		if row.Node.Ignored {
			rows = append(rows, row)
		}
	}
	if len(rows) == 0 {
		r.out.Info("nothing is left out on this machine")
		return nil
	}
	opts := make([]app.Option, len(rows))
	for i, row := range rows {
		opts[i] = app.Option{Label: row.Prefix + row.Node.Name, Note: strings.TrimSpace(string(row.Node.Kind) + "  " + shortRepoName(row.Node.Source.Repo))}
	}
	pick, err := r.f.Prompter().Select("Bring which back?", opts, 0)
	if err != nil {
		return err
	}
	path := rows[pick].Node.Path
	if err := c.Ignore(cmd.Context(), []string{path}, false); err != nil {
		return err
	}
	r.out.OK("no longer ignoring %s", path)
	r.out.Info("the same, as a command: `multiversectl catalog unignore %s`", path)
	return nil
}
