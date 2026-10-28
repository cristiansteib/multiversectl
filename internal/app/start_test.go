package app

import "testing"

// said keeps what a test's Output was told, on the one line kind that -v uses.
type said struct {
	discardOut
	plain []string
}

func (o *said) Plain(string, ...any) { o.plain = append(o.plain, "x") }

// -v asks for the steps, -vvv for the detail behind them; a command that was
// not asked to be talkative prints only its result.
func TestAStepIsSaidOnlyToThoseWhoAskedForIt(t *testing.T) {
	for _, c := range []struct {
		asked, step int
		said        bool
	}{{0, 1, false}, {1, 1, true}, {1, 3, false}, {3, 3, true}} {
		out := &said{}
		(&Service{Deps: Deps{Out: out, Verbosity: c.asked}}).say(c.step, "step")
		if got := len(out.plain) > 0; got != c.said {
			t.Errorf("asked %d, step at %d: said=%v, want %v", c.asked, c.step, got, c.said)
		}
	}
}
