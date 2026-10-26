package cli

import (
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// The three things a person does with this tool: work in universes, set the
// tool up, and manage which projects exist and which are on this machine.
const (
	groupSetup    = "setup"
	groupCatalog  = "catalog"
	groupUniverse = "universe"
	// groupStart is first, and is what somebody who has just installed this
	// reads first: the commands to run before any other makes sense.
	groupStart = "start-here"
)

// startHere is what leads the help and the tree of commands, in this order. A
// help lists commands alphabetically within a group, which puts `init` after
// `completion` and `agents`; the first thing to run should not have to be
// found among the housekeeping.
var startHere = []string{"init"}

var commandGroups = map[string]string{
	"help-tree": groupSetup, "init": groupStart, "reset": groupSetup, "cleanup": groupSetup, "self-update": groupSetup, "version": groupSetup, "completion": groupSetup, "agents": groupSetup,
	"catalog": groupCatalog, "project": groupCatalog,
	"start": groupUniverse, "system": groupUniverse, "universe": groupUniverse, "status": groupUniverse, "server": groupUniverse,
}

// groupCommands sorts the help into those groups and colours it. Each group
// has its own colour, so the three read as three places at a glance; colour
// follows the same rules as everywhere else (NO_COLOR, --no-color, a pipe).
func groupCommands(root *cobra.Command, g *Globals) {
	root.AddGroup(
		&cobra.Group{ID: groupStart, Title: "Get started:"},
		&cobra.Group{ID: groupUniverse, Title: "Universe management:"},
		&cobra.Group{ID: groupSetup, Title: "Tool initialization:"},
		&cobra.Group{ID: groupCatalog, Title: "Catalog management:"},
	)
	for _, c := range root.Commands() {
		if id, ok := commandGroups[c.Name()]; ok {
			c.GroupID = id
		}
	}
	root.SetHelpCommandGroupID(groupSetup)

	paint := func() *Output { return NewOutput(os.Stdout, ColorEnabled(os.Stdout, g.NoColor)) }
	cobra.AddTemplateFunc("heading", func(s string) string { return paint().Bold(s) })
	cobra.AddTemplateFunc("command", func(s string) string { return paint().Bold(s) })
	cobra.AddTemplateFunc("groupTitle", func(id, title string) string {
		o := paint()
		switch id {
		case groupStart:
			return o.paint(bold+cyan, title)
		case groupUniverse:
			return o.Bold(o.Green(title))
		case groupCatalog:
			return o.Bold(o.Accent(title))
		}
		return o.Bold(o.Yellow(title))
	})
	cobra.AddTemplateFunc("label", commandLabel)
	root.SetUsageTemplate(usageTemplate)
}

// usageTemplate is cobra's own, with the headings and command names passed
// through the colour functions above.
const usageTemplate = `{{heading "Usage:"}}{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

{{heading "Aliases:"}}
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

{{heading "Examples:"}}
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

{{heading "Available Commands:"}}{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{command (label .)}} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{groupTitle $group.ID $group.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{command (label .)}} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

{{heading "Additional Commands:"}}{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{command (label .)}} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

{{heading "Flags:"}}
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

{{heading "Global Flags:"}}
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

{{heading "Additional help topics:"}}{{range .Commands}}{{if .IsAdditionalHelpTopic}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{.CommandPath}} [command] --help" for more information about a command.{{end}}
`

// commandLabel is a command's name followed by its aliases, padded so the
// descriptions in a list line up: "universe, u". Cobra shows aliases only on a
// command's own page, which is the last place anyone looks for a shorter name.
func commandLabel(c *cobra.Command) string {
	label := func(c *cobra.Command) string {
		return strings.Join(append([]string{c.Name()}, c.Aliases...), ", ")
	}
	width := 0
	if p := c.Parent(); p != nil {
		for _, s := range p.Commands() {
			if s.IsAvailableCommand() || s.Name() == "help" {
				width = max(width, len(label(s)))
			}
		}
	}
	return label(c) + strings.Repeat(" ", max(0, width-len(label(c))))
}
