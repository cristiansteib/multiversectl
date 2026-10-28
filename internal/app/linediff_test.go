package app

import "testing"

func TestDiffLinesShowsWhatWasAddedAndTakenOut(t *testing.T) {
	got := DiffLines("a\nb\nc\n", "a\nx\nc\nd\n")
	var s string
	for _, l := range got {
		s += string(l.Op) + l.Text + "|"
	}
	if want := " a|-b|+x| c|+d|"; s != want {
		t.Fatalf("diff = %q, want %q", s, want)
	}
	if len(DiffLines("same\n", "same\n")) != 1 || DiffLines("same\n", "same\n")[0].Op != ' ' {
		t.Error("identical texts have only kept lines")
	}
	if got := DiffLines("", "new\n"); len(got) != 1 || got[0].Op != '+' {
		t.Errorf("from nothing: %v", got)
	}
}
