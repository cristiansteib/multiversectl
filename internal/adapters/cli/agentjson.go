package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// jsonFlag is the global --json: the same as -o json on a command that has an
// -o, and the whole of --help as a document on any command. An agent reads
// that instead of scraping help text.
const jsonFlag = "json"

// wantsJSON reports whether --json was given.
func wantsJSON(cmd *cobra.Command) bool {
	f := cmd.Flags().Lookup(jsonFlag)
	return f != nil && f.Value.String() == "true"
}

// applyJSON turns --json into -o json for a command that has an -o. Any other
// command runs as usual and prints its messages and tables as one JSON object
// per line, so an agent can read what happened without scraping text.
func applyJSON(cmd *cobra.Command, out *Output) error {
	if !wantsJSON(cmd) || cmd.Name() == "help" {
		return nil
	}
	if o := cmd.Flags().Lookup("output"); o != nil {
		if !o.Changed {
			return cmd.Flags().Set("output", "json")
		}
		return nil
	}
	out.events = true
	return nil
}

// CommandDoc is one command as --help --json prints it.
type CommandDoc struct {
	Command  string       `json:"command"`
	Short    string       `json:"short,omitempty"`
	Long     string       `json:"long,omitempty"`
	Usage    string       `json:"usage"`
	Aliases  []string     `json:"aliases,omitempty"`
	Examples []string     `json:"examples,omitempty"`
	Flags    []FlagDoc    `json:"flags,omitempty"`
	Commands []CommandDoc `json:"commands,omitempty"`
}

// FlagDoc is one flag.
type FlagDoc struct {
	Name      string `json:"name"`
	Shorthand string `json:"shorthand,omitempty"`
	Type      string `json:"type"`
	Default   string `json:"default,omitempty"`
	Usage     string `json:"usage"`
	Global    bool   `json:"global,omitempty"`
}

func commandDoc(cmd *cobra.Command, top bool) CommandDoc {
	d := CommandDoc{
		Command: cmd.CommandPath(), Short: cmd.Short, Long: strings.TrimSpace(cmd.Long),
		Usage: cmd.UseLine(), Aliases: cmd.Aliases,
	}
	for _, l := range strings.Split(strings.TrimSpace(cmd.Example), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			d.Examples = append(d.Examples, l)
		}
	}
	add := func(set *pflag.FlagSet, global bool) {
		set.VisitAll(func(f *pflag.Flag) {
			if f.Hidden || f.Name == "help" {
				return
			}
			d.Flags = append(d.Flags, FlagDoc{Name: f.Name, Shorthand: f.Shorthand, Type: f.Value.Type(), Default: f.DefValue, Usage: f.Usage, Global: global})
		})
	}
	add(cmd.LocalNonPersistentFlags(), false)
	// The global flags are the same everywhere: say them once, at the top of
	// the document asked for, not on every command of a tree.
	if top {
		add(cmd.InheritedFlags(), true)
		add(cmd.PersistentFlags(), true)
	}
	for _, c := range cmd.Commands() {
		if c.Hidden || !c.IsAvailableCommand() && c.Name() != "help" {
			continue
		}
		if c.Name() == "help" {
			continue
		}
		d.Commands = append(d.Commands, commandDoc(c, false))
	}
	return d
}

// jsonHelp makes -h/--help (and `help <command>`) print the command's document
// when --json is given, and the usual text otherwise.
func jsonHelp(root *cobra.Command, _ any) {
	text := root.HelpFunc()
	root.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		if !wantsJSON(cmd) {
			text(cmd, args)
			return
		}
		b, _ := json.Marshal(commandDoc(cmd, true))
		fmt.Fprintln(cmd.OutOrStdout(), string(b))
	})
}
