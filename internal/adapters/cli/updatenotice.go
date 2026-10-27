package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cristiansteib/multiversectl/internal/app"
)

const (
	// updateGrace is how long a command that has finished waits for a look that
	// is still on its way. Past it the answer is not worth a person noticing the
	// wait; the look is kept if it lands, and said on the next command.
	updateGrace = 100 * time.Millisecond
	// updateLookLimit bounds the look itself, which runs beside the command.
	updateLookLimit = 5 * time.Second
	// updateLookCommand is the hidden command a detached copy of this program
	// runs to finish a look its parent had to leave.
	updateLookCommand = "update-check"
)

// mayCheckForUpdates is whether this command may look for a newer release and
// say so. It stays out of the way of anything a program reads, and of the
// commands whose job is to update or to answer the shell.
func (r runner) mayCheckForUpdates(cmd *cobra.Command) bool {
	if r.f.Bare == nil || !app.Released(r.f.Version) || os.Getenv(app.UpdateOptOutEnv) != "" || wantsJSON(cmd) {
		return false
	}
	switch name := cmd.Name(); {
	case name == "self-update", name == "version", name == updateLookCommand, name == "completion", name == "help",
		strings.HasPrefix(name, cobra.ShellCompRequestCmd),
		cmd.Parent() != nil && cmd.Parent().Name() == "completion":
		return false
	}
	return true
}

// checksForUpdates adds that somebody is there to read it: a notice on stderr
// that nobody is reading is noise in a log.
func (r runner) checksForUpdates(cmd *cobra.Command) bool {
	if !r.mayCheckForUpdates(cmd) {
		return false
	}
	info, err := os.Stderr.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// startUpdateCheck reads what an earlier look found, which is a file read, and
// starts another beside the command when that is old. It returns what to call
// when the command has succeeded.
//
// The first command on a machine therefore says nothing: the look is still
// out when it ends. The next one finds the answer waiting, and no command is
// slowed to get it.
func (r runner) startUpdateCheck(cmd *cobra.Command) func() {
	if !r.checksForUpdates(cmd) {
		return func() {}
	}
	s, err := r.f.Bare(*r.g, r.out)
	if err != nil {
		return func() {}
	}
	rec, _ := s.CachedUpdate()
	// Where the claim cannot be kept every command would find the record old
	// and look again, so there is no look at all.
	if !rec.Stale(time.Now()) || !s.ClaimUpdateLook(time.Now()) {
		return func() {
			if n, ok := rec.Notice(r.f.Version); ok {
				r.printUpdate(n)
			}
		}
	}
	var (
		found app.UpdateRecord
		done  = make(chan struct{})
	)
	go func() {
		defer close(done)
		ctx, cancel := context.WithTimeout(context.Background(), updateLookLimit)
		defer cancel()
		// A failed look returns what was known, and keeps the time it was made.
		found, _ = s.CheckForUpdate(ctx, time.Now())
	}()
	return func() {
		n, ok := rec.Notice(r.f.Version)
		// With nothing to say yet a look that is nearly done is worth a moment;
		// with something already known there is no reason to wait for a newer one.
		wait := updateGrace
		if ok {
			wait = 0
		}
		if lookFinished(done, wait) {
			n, ok = found.Notice(r.f.Version)
		} else if r.f.Detach != nil {
			// This process is about to end and take the look with it. A quick
			// command would otherwise never leave an answer behind, and a newer
			// release than the one known would never be learned.
			r.f.Detach(updateLookCommand)
		}
		if ok {
			r.printUpdate(n)
		}
	}
}

// lookFinished says whether the look is done, waiting up to wait for it.
func lookFinished(done <-chan struct{}, wait time.Duration) bool {
	select {
	case <-done:
		return true
	default:
	}
	if wait <= 0 {
		return false
	}
	select {
	case <-done:
		return true
	case <-time.After(wait):
		return false
	}
}

func (r runner) printUpdate(n app.UpdateNotice) {
	color := ColorEnabled(os.Stderr, r.g.NoColor)
	mark := "!"
	if color {
		mark = "\x1b[33m!\x1b[0m"
	}
	fmt.Fprintln(os.Stderr, mark+" "+Markup(updateLine(n), color))
}

// updateLine is the one line a command ends with when a release is out.
func updateLine(n app.UpdateNotice) string {
	what := ""
	switch {
	case n.Fixes > 0:
		what = counted(n.Fixes, "fix", "fixes", n.More)
	case n.Changes > 0:
		what = counted(n.Changes, "change", "changes", n.More)
	}
	if what != "" {
		what = "; " + what + " since"
	}
	return fmt.Sprintf("multiversectl %s is out (this is %s%s): `multiversectl self-update`", n.Latest, n.Current, what)
}

// counted writes "3 fixes"; atLeast makes it "10+ fixes".
func counted(n int, one, many string, atLeast bool) string {
	plus := ""
	if atLeast {
		plus = "+"
	}
	if n == 1 && !atLeast {
		return "1 " + one
	}
	return fmt.Sprintf("%d%s %s", n, plus, many)
}

// updateLookCmd is what the detached copy runs: one look, kept for the next
// command, and nothing said.
func (r runner) updateLookCmd() *cobra.Command {
	return &cobra.Command{
		Use:    updateLookCommand,
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := r.f.Bare(*r.g, r.out)
			if err != nil {
				return nil
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), updateLookLimit)
			defer cancel()
			_, _ = s.CheckForUpdate(ctx, time.Now())
			return nil
		},
	}
}
