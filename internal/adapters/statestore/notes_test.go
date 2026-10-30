package statestore

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// A journal is appended to, read back in the order it was written, and goes
// with the universe when that is removed.
func TestTheJournalIsAppendedToAndGoesWithTheUniverse(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	st := domain.UniverseState{Name: "3", ID: "3", Slug: "u3"}
	if err := s.Save(st); err != nil {
		t.Fatal(err)
	}
	if notes, err := s.Notes("3"); err != nil || notes != nil {
		t.Fatalf("a universe nobody wrote about is no error: %v %v", notes, err)
	}
	at := time.Date(2026, 10, 30, 18, 50, 0, 0, time.UTC)
	for _, n := range []domain.Note{
		{ID: "n1", At: at, By: "claude", Text: "made it, started it"},
		{ID: "n2", At: at.Add(time.Minute), Text: "login redirect fixed\nin api"},
	} {
		if err := s.AppendNote("3", n); err != nil {
			t.Fatal(err)
		}
	}
	notes, err := s.Notes("3")
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 2 || notes[0].ID != "n1" || notes[1].Text != "login redirect fixed\nin api" {
		t.Fatalf("read back %+v", notes)
	}
	if !notes[0].At.Equal(at) || notes[0].By != "claude" {
		t.Errorf("when and who did not survive: %+v", notes[0])
	}
	// One line per note, so another process appending cannot land inside one.
	data, err := os.ReadFile(filepath.Join(root, "universes", "3", notesFile))
	if err != nil {
		t.Fatal(err)
	}
	if lines := len(data) - len(stripNewlines(data)); lines != 2 {
		t.Errorf("want two lines, got %d:\n%s", lines, data)
	}
	if err := s.SaveNotes("3", notes[:1]); err != nil {
		t.Fatal(err)
	}
	if notes, err = s.Notes("3"); err != nil || len(notes) != 1 {
		t.Fatalf("rewritten journal: %+v %v", notes, err)
	}
	if err := s.Delete("3"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "universes", "3", notesFile)); !os.IsNotExist(err) {
		t.Errorf("the journal should go with the universe: %v", err)
	}
}

func stripNewlines(b []byte) []byte {
	var out []byte
	for _, c := range b {
		if c != '\n' {
			out = append(out, c)
		}
	}
	return out
}
