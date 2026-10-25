package cli

import (
	"os"

	"github.com/spf13/cobra"
)

// The three things a person does with this tool: work in universes, set the
// tool up, and manage which projects exist and which are on this machine.
const (
	groupSetup    = "setup"
	groupCatalog  = "catalog"
	groupUniverse = "universe"
)

var commandGroups = map[string]string{
	"init": groupSetup, "self-update": groupSetup, "version": groupSetup, "completion": groupSetup,
	"catalog": groupCatalog, "install": groupCatalog, "uninstall": groupCatalog,
	"use": groupCatalog, "status": groupCatalog, "update": groupCatalog,
	"new": groupUniverse, "up": groupUniverse, "down": groupUniverse, "rm": groupUniverse,
	"ls": groupUniverse, "urls": groupUniverse, "env": groupUniverse, "run": groupUniverse,
	"exec": groupUniverse, "doctor": groupUniverse, "proxy": groupUniverse,
}

// groupCommands sorts the help into those groups and colours it. Each group
// has its own colour, so the three read as three places at a glance; colour
// follows the same rules as everywhere else (NO_COLOR, --no-color, a pipe).
func groupCommands(root *cobra.Command, g *Globals) {
	root.AddGroup(
		&cobra.Group{ID: groupUniverse, Title: "Universe management:"},
		&cobra.Group{ID: groupSetup, Title: "Tool initialization:"},
		&cobra.Group{ID: groupCatalog, Title: "Catalog and project management:"},
	)
	for _, c := range root.Commands() {
		if id, ok := commandGroups[c.Name()]; ok {
			c.GroupID = id
		}
	}
	root.SetHelpCommandGroupID(groupSetup)
	root.SetCompletionCommandGroupID(groupSetup)

	paint := func() *Output { return NewOutput(os.Stdout, ColorEnabled(os.Stdout, g.NoColor)) }
	cobra.AddTemplateFunc("heading", func(s string) string { return paint().Bold(s) })
	cobra.AddTemplateFunc("command", func(s string) string { return paint().Bold(s) })
	cobra.AddTemplateFunc("groupTitle", func(id, title string) string {
		o := paint()
		switch id {
		case groupUniverse:
			return o.Bold(o.Green(title))
		case groupCatalog:
			return o.Bold(o.Accent(title))
		}
		return o.Bold(o.Yellow(title))
	})
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
  {{command (rpad .Name .NamePadding)}} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{groupTitle $group.ID $group.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{command (rpad .Name .NamePadding)}} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

{{heading "Additional Commands:"}}{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{command (rpad .Name .NamePadding)}} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

{{heading "Flags:"}}
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

{{heading "Global Flags:"}}
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

{{heading "Additional help topics:"}}{{range .Commands}}{{if .IsAdditionalHelpTopic}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{.CommandPath}} [command] --help" for more information about a command.{{end}}
`
