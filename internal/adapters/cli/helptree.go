package cli

import (
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// helpTreeCmd draws every command there is, nested as it is typed.
//
// The help of one command names its children and no further, so the shape of
// the whole tool — what lives under what, what is a command and what only
// groups others — is something nobody can see without opening each in turn.
// This walks the command tree itself, so it is the tree that is installed and
// cannot disagree with it, and it shows the aliases, which no help screen
// puts next to the name they belong to.
func (r runner) helpTreeCmd(root *cobra.Command) *cobra.Command {
	var flat bool
	cmd := &cobra.Command{
		Use:   "help-tree",
		Short: "Show every command and subcommand, nested",
		Long: "Every command, under the command it belongs to, with the aliases it answers\n" +
			"to in brackets and a line on what it does.",
		Example: "  multiversectl help-tree\n  multiversectl help-tree --flat     # one full command per line, to grep",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			nodes := commandTree(root)
			if flat {
				for _, line := range flatten(nodes, "") {
					r.out.Plain("%s", line)
				}
				return nil
			}
			r.out.Plain("%s", r.out.Bold(root.Name()))
			drawTree(r.out, nodes, "")
			r.out.Plain("")
			r.out.Info("%d commands", countTree(nodes))
			return nil
		},
	}
	cmd.Flags().BoolVar(&flat, "flat", false, "one full command per line instead of a tree, for grep")
	return cmd
}

// treeNode is one command, with what is under it.
type treeNode struct {
	Name     string
	Aliases  []string
	Short    string
	Children []treeNode
}

// commandTree reads the commands under c, in the order they are typed about:
// by name, since that is how somebody looks one up.
func commandTree(c *cobra.Command) []treeNode {
	var out []treeNode
	for _, k := range c.Commands() {
		// Hidden commands are hidden for a reason, and the help command and
		// the completion plumbing are cobra's, not the tool's.
		if k.Hidden || k.Name() == "help" || !k.IsAvailableCommand() {
			continue
		}
		kids := commandTree(k)
		out = append(out, treeNode{
			Name: k.Name(), Aliases: k.Aliases, Short: k.Short, Children: kids,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// treeRow is one line of the drawing before it is padded: the branch
// characters, the name, the aliases after it, and what the command does.
type treeRow struct {
	prefix, branch, name, aliases, short string
}

// width is how much of the line the description has to sit after.
func (r treeRow) width() int {
	return len([]rune(r.prefix + r.branch + r.name + r.aliases))
}

func drawTree(o *Output, nodes []treeNode, _ string) {
	rows := treeRows(nodes, "")
	// One column for every description, not one per level: a tree whose
	// descriptions step in and out is a tree that is hard to read down.
	col := 0
	for _, r := range rows {
		if w := r.width(); w > col {
			col = w
		}
	}
	for _, r := range rows {
		pad := strings.Repeat(" ", col-r.width())
		o.Plain("%s%s%s%s%s  %s", o.Dim(r.prefix+r.branch), o.Bold(r.name), o.Dim(r.aliases), pad, "", r.short)
	}
}

func treeRows(nodes []treeNode, prefix string) []treeRow {
	var rows []treeRow
	for i, n := range nodes {
		branch, next := "├── ", "│   "
		if i == len(nodes)-1 {
			branch, next = "└── ", "    "
		}
		aliases := ""
		if len(n.Aliases) > 0 {
			aliases = " (" + strings.Join(n.Aliases, ", ") + ")"
		}
		rows = append(rows, treeRow{prefix: prefix, branch: branch, name: n.Name, aliases: aliases, short: n.Short})
		rows = append(rows, treeRows(n.Children, prefix+next)...)
	}
	return rows
}

func flatten(nodes []treeNode, path string) []string {
	var out []string
	for _, n := range nodes {
		full := strings.TrimSpace(path + " " + n.Name)
		out = append(out, full+"  — "+n.Short)
		out = append(out, flatten(n.Children, full)...)
	}
	return out
}

func countTree(nodes []treeNode) int {
	n := len(nodes)
	for _, k := range nodes {
		n += countTree(k.Children)
	}
	return n
}
