package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cristiansteib/multiversectl/internal/app"
	"github.com/cristiansteib/multiversectl/internal/domain"
)

// notesCmd is a universe's journal: a place to say what has been done in it,
// in the order it happened. `note` is the one line saying what the universe is
// for; this is the account underneath it. Nothing is obliged to write here.
func (r runner) notesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "notes <universe>",
		Aliases: []string{"journal"},
		Short:   "The journal of a universe: what was done in it, in the order it happened",
		Long: "Every note with its id, when it was added and who by. A note is appended and\n" +
			"never rewritten by another, so the journal is an account of the work rather\n" +
			"than a single line that the next writer overwrites — which is what `universe\n" +
			"note` is for. Writing here is nobody's duty: a universe with an empty journal\n" +
			"is a universe nobody wrote about. The web page shows the same journal.",
		Example: "  multiversectl universe notes 3\n" +
			"  multiversectl universe notes add 3 \"login redirect fixed in api\"\n" +
			"  multiversectl universe notes add 3 --by claude -\n" +
			"  multiversectl universe notes set 3 n2 \"and in web\"\n" +
			"  multiversectl universe notes rm 3 n2",
		Args: cobra.ExactArgs(1),
	}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return r.withService(func(_ context.Context, s *app.Service) error {
			notes, err := s.Journal(args[0])
			if err != nil {
				return err
			}
			if wantsJSON(cmd) {
				// A note is prose and may have several lines, so the journal is a
				// document rather than a table: the text goes over whole.
				return writeCompactJSON(cmd.OutOrStdout(), map[string]any{"notes": notes})
			}
			if len(notes) == 0 {
				r.out.Info("no notes on %s: `multiversectl universe notes add %s \"<what you did>\"`", args[0], args[0])
				return nil
			}
			r.out.Plain("%s", journalLines(notes))
			return nil
		})(cmd, args)
	}
	cmd.AddCommand(r.notesAddCmd(), r.notesSetCmd(), r.notesRmCmd())
	return cmd
}

func (r runner) notesAddCmd() *cobra.Command {
	var by string
	cmd := &cobra.Command{
		Use:   "add <universe> <text>",
		Short: "Add a note to a universe's journal",
		Long: "The id and the time are the tool's to fill in. `-` as the text reads it from\n" +
			"standard input, for a note longer than a command line wants.",
		Example: "  multiversectl universe notes add 3 \"migrations run, mail still failing\"\n" +
			"  multiversectl universe notes add 3 --by claude \"started on the redirect\"",
		Args: cobra.MinimumNArgs(2),
	}
	cmd.Flags().StringVar(&by, "by", "", "who is saying it: an agent's name, a person's (default: nobody)")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return r.withService(func(ctx context.Context, s *app.Service) error {
			text, err := noteText(cmd, args[1:])
			if err != nil {
				return err
			}
			n, err := s.AddNote(ctx, args[0], text, by)
			if err != nil {
				return err
			}
			r.out.OK("universe %s: %s %s", args[0], n.ID, n.Text)
			return nil
		})(cmd, args)
	}
	return cmd
}

func (r runner) notesSetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "set <universe> <id> <text>",
		Short:             "Replace what one note says, keeping its id and its time",
		Example:           "  multiversectl universe notes set 3 n2 \"fixed in api and in web\"",
		Args:              cobra.MinimumNArgs(3),
		ValidArgsFunction: r.completeNoteIDs,
	}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return r.withService(func(ctx context.Context, s *app.Service) error {
			text, err := noteText(cmd, args[2:])
			if err != nil {
				return err
			}
			n, err := s.EditNote(ctx, args[0], args[1], text)
			if err != nil {
				return err
			}
			r.out.OK("universe %s: %s %s", args[0], n.ID, n.Text)
			return nil
		})(cmd, args)
	}
	return cmd
}

func (r runner) notesRmCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "rm <universe> <id>",
		Short:             "Take one note out of a journal",
		Example:           "  multiversectl universe notes rm 3 n2",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: r.completeNoteIDs,
	}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return r.withService(func(ctx context.Context, s *app.Service) error {
			n, err := s.RemoveNote(ctx, args[0], args[1])
			if err != nil {
				return err
			}
			r.out.OK("universe %s: %s removed (%s)", args[0], n.ID, n.Text)
			return nil
		})(cmd, args)
	}
	return cmd
}

// journalLines is the journal as a person reads it: a note a line, its id,
// when it was made and who by lined up, and the second and later lines of a
// note under the first rather than back at the margin.
func journalLines(notes []domain.Note) string {
	width, by := 0, 0
	for _, n := range notes {
		width = max(width, visibleLen(n.ID))
		by = max(by, visibleLen(n.By))
	}
	var b strings.Builder
	for _, n := range notes {
		head := pad(n.ID, width) + "  " + n.At.Local().Format("2006-01-02 15:04") + "  "
		if by > 0 {
			head += pad(n.By, by) + "  "
		}
		for i, line := range strings.Split(n.Text, "\n") {
			if i > 0 {
				head = strings.Repeat(" ", visibleLen(head))
			}
			b.WriteString(head + line + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// noteText is the words of a note: the rest of the command line, or standard
// input when it is `-`, so a long note need not be quoted into a shell.
func noteText(cmd *cobra.Command, args []string) (string, error) {
	if len(args) == 1 && args[0] == "-" {
		in := cmd.InOrStdin()
		if in == os.Stdin {
			if stat, err := os.Stdin.Stat(); err == nil && stat.Mode()&os.ModeCharDevice != 0 {
				return "", fmt.Errorf("`-` reads the note from standard input, and there is none: %w", app.ErrNotInteractive)
			}
		}
		text, err := io.ReadAll(in)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(text)), nil
	}
	return strings.Join(args, " "), nil
}

// completeNoteIDs completes the ids a universe's journal has, with what each
// says, so an id never has to be looked up first. Like every completion here
// it reads what is on this machine and nothing else: no manifest notice on the
// candidate list the shell parses, and no fetch behind a tab key.
func (r runner) completeNoteIDs(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) != 1 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	s, err := r.f.Multiverse(*r.g, quietOut{})
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	notes, err := s.Journal(args[0])
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	out := make([]string, 0, len(notes))
	for _, n := range notes {
		out = append(out, n.ID+"\t"+firstLine(n.Text))
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

// firstLine is a note's first line, clipped the way this package clips any
// text that has to fit on one.
func firstLine(text string) string {
	line, _, _ := strings.Cut(text, "\n")
	return truncate(line, 60)
}
