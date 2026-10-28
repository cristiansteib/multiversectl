// Package trace says, on stderr, which programs this one runs and how they
// ended, for a person who wants to see what a command is doing.
package trace

import (
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// Levels of detail. 1 belongs to the use cases, which say what they are doing;
// the commands they run come at 2, and how each one ended at 3.
const (
	Commands = 2
	Results  = 3
)

// Tracer writes what it is told when the level asked for is high enough. A nil
// Tracer is one that says nothing, so an adapter built without it needs no care.
type Tracer struct {
	level int
	w     io.Writer
}

func New(level int, w io.Writer) *Tracer { return &Tracer{level: level, w: w} }

// Begin says that cmd is about to run and returns what to call with its error
// once it has. It goes on stderr, so a command's own output stays what a pipe
// reads.
func (t *Tracer) Begin(cmd *exec.Cmd) func(error) {
	if t == nil || t.level < Commands {
		return func(error) {}
	}
	where := ""
	if cmd.Dir != "" {
		where = "  (in " + cmd.Dir + ")"
	}
	fmt.Fprintf(t.w, "  + %s%s\n", line(cmd.Args), where)
	if t.level < Results {
		return func(error) {}
	}
	start := time.Now()
	return func(err error) {
		took := time.Since(start).Round(time.Millisecond)
		if err != nil {
			fmt.Fprintf(t.w, "    failed after %s: %v\n", took, err)
			return
		}
		fmt.Fprintf(t.w, "    ok in %s\n", took)
	}
}

// line is the command as a shell would take it, quoting what has a space in it.
func line(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		if a == "" || strings.ContainsAny(a, " \t\n'\"$") {
			a = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
		quoted[i] = a
	}
	return strings.Join(quoted, " ")
}
