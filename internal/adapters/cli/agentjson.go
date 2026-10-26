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

func flagOn(cmd *cobra.Command, name string) bool {
	f := cmd.Flags().Lookup(name)
	return f != nil && f.Value.String() == "true"
}

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
			v := "json"
			if flagOn(cmd, "less") {
				v = "json,less"
			}
			return cmd.Flags().Set("output", v)
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

// commandDoc describes a command. By default it is lean, for an agent's
// context: what the command is for, its usage, its own flags and, for a group,
// the names and one-liners of its subcommands (ask one of them for more).
// full is the whole tree with every long text and example.
func commandDoc(cmd *cobra.Command, top, full bool) CommandDoc {
	d := CommandDoc{Command: cmd.CommandPath(), Short: cmd.Short, Usage: cmd.UseLine(), Aliases: cmd.Aliases}
	if full {
		d.Long = strings.TrimSpace(cmd.Long)
	}
	leaf := !cmd.HasAvailableSubCommands()
	if full || leaf {
		for _, l := range strings.Split(strings.TrimSpace(cmd.Example), "\n") {
			if l = strings.TrimSpace(l); l != "" {
				d.Examples = append(d.Examples, l)
			}
		}
	}
	add := func(set *pflag.FlagSet, global bool) {
		set.VisitAll(func(f *pflag.Flag) {
			if f.Hidden || f.Name == "help" {
				return
			}
			fd := FlagDoc{Name: f.Name, Shorthand: f.Shorthand, Type: f.Value.Type(), Default: f.DefValue, Usage: f.Usage, Global: global}
			if fd.Type == "bool" {
				fd.Type = "" // a switch: the type says nothing
			}
			if fd.Default == "false" || fd.Default == "[]" {
				fd.Default = ""
			}
			d.Flags = append(d.Flags, fd)
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
		if c.Hidden || c.Name() == "help" || !c.IsAvailableCommand() {
			continue
		}
		if full {
			d.Commands = append(d.Commands, commandDoc(c, false, true))
		} else {
			d.Commands = append(d.Commands, CommandDoc{Command: c.CommandPath(), Short: c.Short})
		}
	}
	return d
}

// jsonHelp makes -h/--help (and `help <command>`) print the command's document
// when --json is given, and the usual text otherwise.
func jsonHelp(root *cobra.Command, _ any) {
	root.PersistentFlags().Bool("less", false, "with --json: only what an agent must have (listings drop everything it can work out)")
	root.PersistentFlags().Bool("full", false, "with --help --json: every command with its long text and examples, not the lean index")
	text := root.HelpFunc()
	root.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		if !wantsJSON(cmd) {
			text(cmd, args)
			return
		}
		b, _ := json.Marshal(commandDoc(cmd, true, flagOn(cmd, "full")))
		fmt.Fprintln(cmd.OutOrStdout(), string(b))
	})
}
