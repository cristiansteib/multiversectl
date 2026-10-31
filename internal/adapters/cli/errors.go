package cli

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cristiansteib/multiversectl/internal/app"
)

var (
	optionalPart = regexp.MustCompile(`\[[^\]]*\]`)
	placeholder  = regexp.MustCompile(`<[^>]+>`)
)

// explainArgs replaces cobra's argument errors ("accepts 1 arg(s), received 0")
// with ones that say what is missing, how the command is used, and where to
// look next. The validators stay cobra's; only the words change.
func explainArgs(c *cobra.Command) {
	if check := c.Args; check != nil {
		c.Args = func(cmd *cobra.Command, args []string) error {
			if err := check(cmd, args); err != nil {
				return argError(cmd, args)
			}
			return nil
		}
	}
	for _, sub := range c.Commands() {
		explainArgs(sub)
	}
}

// argError names the placeholders still unfilled, or the surplus words.
func argError(cmd *cobra.Command, args []string) error {
	if cmd.HasAvailableSubCommands() && len(args) > 0 {
		var names []string
		for _, sub := range cmd.Commands() {
			if sub.IsAvailableCommand() {
				names = append(names, sub.Name())
			}
		}
		msg := fmt.Sprintf("`%s` has no command `%s`", cmd.CommandPath(), args[0])
		if cmd.SuggestionsMinimumDistance <= 0 {
			cmd.SuggestionsMinimumDistance = 2 // cobra sets this only on its own lookup path
		}
		// A command that took over the old name is the answer; spelling guesses
		// would only add noise beside it ("restart" for "start").
		var near []string
		for _, sub := range cmd.Commands() {
			if slices.Contains(sub.SuggestFor, args[0]) {
				near = append(near, sub.Name())
			}
		}
		if len(near) == 0 {
			near = cmd.SuggestionsFor(args[0])
		}
		if len(near) > 0 {
			msg += fmt.Sprintf(" — did you mean `%s`?", strings.Join(near, "` or `"))
		}
		return fmt.Errorf("%s\n  commands: %s\n  more:     `%s --help`", msg, strings.Join(names, ", "), cmd.CommandPath())
	}
	_, use, _ := strings.Cut(cmd.Use, " ")
	required := placeholder.FindAllString(optionalPart.ReplaceAllString(use, ""), -1)

	var what string
	switch {
	case len(args) < len(required):
		what = fmt.Sprintf("`%s` needs `%s`", cmd.CommandPath(), strings.Join(required[len(args):], " "))
		if len(args) > 0 {
			what = fmt.Sprintf("`%s` is missing `%s`", cmd.CommandPath(), strings.Join(required[len(args):], " "))
		}
	case len(required) == 0 && !strings.Contains(use, "["):
		what = fmt.Sprintf("`%s` takes no arguments, but got: `%s`", cmd.CommandPath(), strings.Join(args, " "))
	default:
		what = fmt.Sprintf("`%s` got more arguments than it takes: `%s`", cmd.CommandPath(), strings.Join(args, " "))
	}

	var b strings.Builder
	b.WriteString(what)
	fmt.Fprintf(&b, "\n  usage:   `%s`", cmd.UseLine())
	if ex := firstExample(cmd); ex != "" {
		fmt.Fprintf(&b, "\n  example: `%s`", ex)
	}
	fmt.Fprintf(&b, "\n  more:    `%s --help`", cmd.CommandPath())
	return fmt.Errorf("%s", b.String())
}

func firstExample(cmd *cobra.Command) string {
	for _, line := range strings.Split(cmd.Example, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			// An example may carry a trailing "# comment"; the command is what
			// someone copies.
			cmdline, _, _ := strings.Cut(line, "  #")
			return strings.TrimSpace(cmdline)
		}
	}
	return ""
}

// flagError says where the flags of a command are listed. A bare --at is the one
// that gets more: what it wanted was a place, so the places are listed.
func (r runner) flagError(cmd *cobra.Command, err error) error {
	msg := fmt.Sprintf("%v\n  flags: `%s --help`", err, cmd.CommandPath())
	if strings.Contains(err.Error(), "flag needs an argument: --at") {
		msg = "--at needs the path of a catalog to put it in, and / for the root"
		if r.f.Catalogs == nil {
			return fmt.Errorf("%s\n  leave --at out to choose from a list", msg)
		}
		if c, cerr := r.catalogs(false); cerr == nil {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			if tree, terr := c.TreeCached(ctx); terr == nil {
				msg += "\n  the places are: " + app.Places(tree)
			}
		}
		msg += "\n  leave --at out to choose from a list"
	}
	return fmt.Errorf("%s", msg)
}
