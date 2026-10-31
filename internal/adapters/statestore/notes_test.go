package statestore

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// journalDir is where a universe's notes are kept, one file to a note.
func journalDir(root, id string) string {
	return filepath.Join(root, "universes", id, ownDir, notesDir)
}

func saved(t *testing.T, root, id string) *Store {
	t.Helper()
	s := New(root)
	if err := s.Save(domain.UniverseState{Name: id, ID: id, Slug: "u" + id}); err != nil {
		t.Fatal(err)
	}
	return s
}

// A note is its own file, the journal reads back in the order it was written,
// and it goes with the universe when that is removed.
func TestAJournalIsADirectoryOfNotes(t *testing.T) {
	root := t.TempDir()
	s := saved(t, root, "3")
	if notes, err := s.Notes("3"); err != nil || notes != nil {
		t.Fatalf("a universe nobody wrote about is no error: %v %v", notes, err)
	}
	at := time.Date(2026, 10, 30, 18, 50, 0, 0, time.UTC)
	// Out of order on purpose: what orders the journal is the id, not the
	// order a directory happens to list its files in.
	for _, n := range []domain.Note{
		{ID: "n2", At: at.Add(time.Minute), Text: "login redirect fixed\nin api"},
		{ID: "n10", At: at.Add(2 * time.Minute), Text: "and in web"},
		{ID: "n1", At: at, By: "claude", Text: "made it, started it"},
	} {
		if err := s.CreateNote("3", n); err != nil {
			t.Fatal(err)
		}
	}
	notes, err := s.Notes("3")
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 3 || notes[0].ID != "n1" || notes[1].ID != "n2" || notes[2].ID != "n10" {
		t.Fatalf("read back in id order, not n10 before n2: %+v", notes)
	}
	if notes[1].Text != "login redirect fixed\nin api" {
		t.Errorf("a note of several lines is one note: %q", notes[1].Text)
	}
	if !notes[0].At.Equal(at) || notes[0].By != "claude" {
		t.Errorf("when and who did not survive: %+v", notes[0])
	}
	// One file to a note, named for it: nothing another process writes can
	// land inside one.
	entries, err := os.ReadDir(journalDir(root, "3"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if strings.Join(names, " ") != "n1.json n10.json n2.json" {
		t.Errorf("the journal's directory holds %v", names)
	}
	if err := s.Delete("3"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "universes", "3", ownDir)); !os.IsNotExist(err) {
		t.Errorf("the journal should go with the universe: %v", err)
	}
}

// Two writers that pick the same id: the second is told, and the first note
// stays as it was.
func TestATakenIDIsRefusedAndNotWrittenOver(t *testing.T) {
	root := t.TempDir()
	s := saved(t, root, "3")
	first := domain.Note{ID: "n1", At: time.Now().UTC(), By: "them", Text: "theirs"}
	if err := s.CreateNote("3", first); err != nil {
		t.Fatal(err)
	}
	err := s.CreateNote("3", domain.Note{ID: "n1", At: time.Now().UTC(), By: "me", Text: "mine"})
	if !errors.Is(err, domain.ErrExists) {
		t.Fatalf("want the id is taken: %v", err)
	}
	notes, err := s.Notes("3")
	if err != nil || len(notes) != 1 || notes[0].Text != "theirs" {
		t.Fatalf("the note that got there first should stand: %+v %v", notes, err)
	}
	// Changing one note leaves the others alone, down to their files.
	if err := s.CreateNote("3", domain.Note{ID: "n2", At: time.Now().UTC(), Text: "second"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveNote("3", domain.Note{ID: "n1", At: first.At, Removed: true}); err != nil {
		t.Fatal(err)
	}
	if notes, err = s.Notes("3"); err != nil || len(notes) != 2 || !notes[0].Removed || notes[1].Text != "second" {
		t.Fatalf("after a removal: %+v %v", notes, err)
	}
}

// A note for a universe that is not there is refused, and does not bring its
// directory back: `rm` and a note written at the same moment is the case.
func TestANoteForANoUniverseIsRefused(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	err := s.CreateNote("gone", domain.Note{ID: "n1", At: time.Now().UTC(), Text: "hello?"})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("want not found: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "universes", "gone")); !os.IsNotExist(err) {
		t.Errorf("it made the universe's directory: %v", err)
	}
}

// A note nobody can parse says which file it is, so it can be dealt with by
// hand; an empty one is an id taken by a write that never landed, and the rest
// of the journal still reads.
func TestWhatIsNotANoteDoesNotStopTheJournal(t *testing.T) {
	root := t.TempDir()
	s := saved(t, root, "3")
	if err := s.CreateNote("3", domain.Note{ID: "n1", At: time.Now().UTC(), Text: "fine"}); err != nil {
		t.Fatal(err)
	}
	dir := journalDir(root, "3")
	for name, data := range map[string]string{"n2.json": "", "notes.txt": "a file somebody left here"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	notes, err := s.Notes("3")
	if err != nil || len(notes) != 1 || notes[0].ID != "n1" {
		t.Fatalf("an empty note and a file that is not one: %+v %v", notes, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "n3.json"), []byte("not a note"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Notes("3"); err == nil || !strings.Contains(err.Error(), "n3.json") {
		t.Errorf("want the file it is in: %v", err)
	}
}

// A note longer than anything meant to be written must still read back: the
// note that would repair a journal somebody pasted a log into is the one in it.
func TestALongNoteStillReads(t *testing.T) {
	root := t.TempDir()
	s := saved(t, root, "3")
	// Written by hand, or by a version that had no limit.
	if err := s.CreateNote("3", domain.Note{ID: "n1", At: time.Now().UTC(), Text: strings.Repeat("x", 2<<20)}); err != nil {
		t.Fatal(err)
	}
	notes, err := s.Notes("3")
	if err != nil || len(notes) != 1 || len(notes[0].Text) != 2<<20 {
		t.Fatalf("a journal with a very long note in it: %d notes, %v", len(notes), err)
	}
	// And it can be taken out again.
	if err := s.SaveNote("3", domain.Note{ID: "n1", At: notes[0].At, Removed: true}); err != nil {
		t.Fatal(err)
	}
	if notes, err = s.Notes("3"); err != nil || len(notes) != 1 || notes[0].Text != "" {
		t.Fatalf("repaired: %+v %v", notes, err)
	}
}
