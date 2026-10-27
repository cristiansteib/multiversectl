package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// helpCmd is `help`, which shows one command's help as it always did, and with
// --tree draws every command there is, nested as it is typed.
//
// The help of one command names its children and no further, so the shape of
// the whole tool — what lives under what, what is a command and what only
// groups others — is something nobody can see without opening each in turn.
// The tree walks the command tree itself, so it is the tree that is installed
// and cannot disagree with it, and it shows the aliases, which no help screen
// puts next to the name they belong to.
func (r runner) helpCmd(root *cobra.Command) *cobra.Command {
	var tree, flat bool
	cmd := &cobra.Command{
		Use:   "help [command]",
		Short: "Help about any command, or --tree for all of them",
		Long: "Help about any command. With --tree, every command under the command it\n" +
			"belongs to, with the aliases it answers to in brackets and a line on what it\n" +
			"does.",
		Example: "  multiversectl help universe\n  multiversectl help --tree\n  multiversectl help --flat     # one full command per line, to grep",
		ValidArgsFunction: func(c *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
			if tree || flat {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			var names []string
			target, _, _ := c.Root().Find(args)
			if target == nil {
				target = c.Root()
			}
			for _, k := range target.Commands() {
				if k.IsAvailableCommand() || k.Name() == "help" {
					names = append(names, k.Name()+"\t"+k.Short)
				}
			}
			return names, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if tree || flat {
				if len(args) > 0 {
					return fmt.Errorf("--tree shows every command; it takes no command name")
				}
				r.printTree(root, flat)
				return nil
			}
			target, _, err := root.Find(args)
			if target == nil || err != nil {
				cmd.Printf("Unknown help topic %#q\n", args)
				return root.Usage()
			}
			target.InitDefaultHelpFlag()
			return target.Help()
		},
	}
	cmd.Flags().BoolVar(&tree, "tree", false, "every command and subcommand, nested")
	cmd.Flags().BoolVar(&flat, "flat", false, "with --tree: one full command per line instead of a tree, for grep (implies --tree)")
	return cmd
}

func (r runner) printTree(root *cobra.Command, flat bool) {
	nodes := commandTree(root)
	if flat {
		for _, line := range flatten(nodes, "") {
			r.out.Plain("%s", line)
		}
		return
	}
	r.out.Plain("%s", r.out.Bold(root.Name()))
	drawTree(r.out, nodes)
	r.out.Plain("")
	r.out.Info("%d commands", countTree(nodes))
}

// treeNode is one command, with what is under it.
type treeNode struct {
	Name     string
	Aliases  []string
	Short    string
	Children []treeNode
}

// commandTree reads the commands under c: by name, since that is how somebody
// looks one up, except that what leads the help (startHere) leads here too.
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
	pinned := func(name string) int {
		if c.HasParent() {
			return len(startHere) // only the top level has a front of the queue
		}
		for i, n := range startHere {
			if n == name {
				return i
			}
		}
		return len(startHere)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if pi, pj := pinned(out[i].Name), pinned(out[j].Name); pi != pj {
			return pi < pj
		}
		return out[i].Name < out[j].Name
	})
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

func drawTree(o *Output, nodes []treeNode) {
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
		o.Plain("%s%s%s%s  %s", o.Dim(r.prefix+r.branch), o.Bold(r.name), o.Dim(r.aliases), pad, r.short)
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
