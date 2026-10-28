package trace

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

type sink struct{ strings.Builder }

func TestEachLevelSaysOnlyWhatItWasAskedFor(t *testing.T) {
	cmd := exec.Command("git", "-C", "/my repo", "fetch")
	cmd.Dir = "/ws"
	for _, c := range []struct {
		level int
		want  []string
		not   []string
	}{
		{1, nil, []string{"git"}},
		{2, []string{"+ git -C '/my repo' fetch", "(in /ws)"}, []string{"ok in"}},
		{3, []string{"+ git -C '/my repo' fetch", "failed after"}, nil},
	} {
		var w sink
		New(c.level, &w).Begin(cmd)(errors.New("exit status 1"))
		for _, s := range c.want {
			if !strings.Contains(w.String(), s) {
				t.Errorf("level %d: want %q in %q", c.level, s, w.String())
			}
		}
		for _, s := range c.not {
			if strings.Contains(w.String(), s) {
				t.Errorf("level %d: did not want %q in %q", c.level, s, w.String())
			}
		}
	}
}

func TestANilTracerSaysNothingAndDoesNotFail(t *testing.T) {
	var tr *Tracer
	tr.Begin(exec.Command("true"))(nil)
}
