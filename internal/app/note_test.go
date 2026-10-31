package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// journalStore keeps a journal the way the real store does: a note at a time,
// under an id no second note can take.
type journalStore struct {
	Store
	state domain.UniverseState
	notes []domain.Note
	loads int
	// gone is a universe removed while this command waited for its lock: it is
	// there when the command starts and not there when the lock is in hand.
	gone bool
	// stale hides the journal from the next read, as a writer that added a
	// note in the moment between another's read and its write would.
	stale bool
}

func (j *journalStore) Load(id string) (domain.UniverseState, error) {
	j.loads++
	if id != j.state.ID || (j.gone && j.loads > 1) {
		return domain.UniverseState{}, domain.ErrNotFound
	}
	return j.state, nil
}
func (j *journalStore) List() ([]domain.UniverseState, error) {
	if j.gone && j.loads > 1 {
		return nil, nil
	}
	return []domain.UniverseState{j.state}, nil
}
func (j *journalStore) Save(st domain.UniverseState) error { j.state = st; return nil }
func (j *journalStore) Notes(string) ([]domain.Note, error) {
	if j.stale {
		j.stale = false
		return nil, nil
	}
	return j.notes, nil
}
func (j *journalStore) CreateNote(_ string, n domain.Note) error {
	for _, had := range j.notes {
		if had.ID == n.ID {
			return domain.ErrExists
		}
	}
	j.notes = append(j.notes, n)
	return nil
}
func (j *journalStore) SaveNote(_ string, n domain.Note) error {
	for i, had := range j.notes {
		if had.ID == n.ID {
			j.notes[i] = n
			return nil
		}
	}
	return domain.ErrNotFound
}

func journal(t *testing.T) (*Service, *journalStore) {
	t.Helper()
	store := &journalStore{state: domain.UniverseState{Name: "3", ID: "3", Slug: "u3"}}
	return &Service{Deps: Deps{Store: store}}, store
}

// The tool fills in the id and the time: nothing writing a note has to invent
// either, and an id is never reused even after the note before it went.
func TestANoteGetsItsIDAndTimeFromTheTool(t *testing.T) {
	s, _ := journal(t)
	ctx := context.Background()
	first, err := s.AddNote(ctx, "3", "  made it, started it  ", " claude ")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != "n1" || first.Text != "made it, started it" || first.By != "claude" {
		t.Fatalf("got %+v", first)
	}
	if first.At.IsZero() || first.At.Location() != time.UTC {
		t.Errorf("the time should be filled in, in UTC: %v", first.At)
	}
	second, err := s.AddNote(ctx, "3", "login redirect fixed", "")
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != "n2" || second.By != "" {
		t.Fatalf("got %+v", second)
	}
	if _, err := s.RemoveNote(ctx, "3", "n2"); err != nil {
		t.Fatal(err)
	}
	third, err := s.AddNote(ctx, "3", "and in web", "")
	if err != nil {
		t.Fatal(err)
	}
	if third.ID != "n3" {
		t.Errorf("an id is not reused after a removal: %s", third.ID)
	}
	if notes, _ := s.Journal("3"); len(notes) != 2 || notes[0].ID != "n1" || notes[1].ID != "n3" {
		t.Errorf("the journal reads in the order it was written: %+v", notes)
	}
}

// Editing keeps the account honest: the words change, the id and the time it
// happened do not.
func TestEditingKeepsTheIDAndTheTime(t *testing.T) {
	s, _ := journal(t)
	ctx := context.Background()
	made, err := s.AddNote(ctx, "3", "fixed in api", "")
	if err != nil {
		t.Fatal(err)
	}
	edited, err := s.EditNote(ctx, "3", "n1", "fixed in api and in web")
	if err != nil {
		t.Fatal(err)
	}
	if edited.ID != made.ID || !edited.At.Equal(made.At) || edited.Text != "fixed in api and in web" {
		t.Fatalf("got %+v, was %+v", edited, made)
	}
}

func TestAJournalSaysWhatItCannotDo(t *testing.T) {
	s, _ := journal(t)
	ctx := context.Background()
	if _, err := s.AddNote(ctx, "3", "   ", ""); err == nil || !strings.Contains(err.Error(), "something to say") || !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("an empty note, refused as the caller's mistake: %v", err)
	}
	if _, err := s.EditNote(ctx, "3", "n9", "nothing"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("an id that is not there: %v", err)
	}
	if _, err := s.RemoveNote(ctx, "3", "n9"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("an id that is not there: %v", err)
	}
	if _, err := s.Journal("nope"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("a universe that is not there: %v", err)
	}
}

// An id is taken with or without its prefix: `rm 7` is `rm n7`.
func TestANoteIsFoundWithOrWithoutItsPrefix(t *testing.T) {
	s, _ := journal(t)
	ctx := context.Background()
	if _, err := s.AddNote(ctx, "3", "first", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EditNote(ctx, "3", "1", "still first"); err != nil {
		t.Fatalf("a bare number: %v", err)
	}
	if _, err := s.RemoveNote(ctx, "3", " n1 "); err != nil {
		t.Fatalf("a padded id: %v", err)
	}
}

// A universe can be removed while a change to one of its notes waits for the
// lock, and a journal must not be written for one that is gone.
func TestANoteIsNotChangedForAUniverseThatWentAway(t *testing.T) {
	s, store := journal(t)
	if _, err := s.AddNote(context.Background(), "3", "mine", ""); err != nil {
		t.Fatal(err)
	}
	store.gone = true
	if _, err := s.EditNote(context.Background(), "3", "n1", "theirs"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("editing in a universe that went away: %v", err)
	}
	if store.notes[0].Text != "mine" {
		t.Errorf("it wrote anyway: %+v", store.notes)
	}
}

// Adding takes no lock, so two writers can pick the same id. The journal
// refuses the second, which then takes the next id rather than writing over
// the note that got there first.
func TestAnIDTakenSinceTheJournalWasReadIsNotWrittenOver(t *testing.T) {
	s, store := journal(t)
	ctx := context.Background()
	first, err := s.AddNote(ctx, "3", "theirs", "them")
	if err != nil {
		t.Fatal(err)
	}
	store.stale = true // as if "theirs" had landed a moment ago
	second, err := s.AddNote(ctx, "3", "mine", "me")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != "n1" || second.ID != "n2" {
		t.Fatalf("ids %s and %s", first.ID, second.ID)
	}
	if len(store.notes) != 2 || store.notes[0].Text != "theirs" {
		t.Errorf("both notes should be there, the first one as it was: %+v", store.notes)
	}
}

// A removed note keeps its place and its id, and the words go.
func TestARemovedNoteLeavesItsIDSpent(t *testing.T) {
	s, store := journal(t)
	ctx := context.Background()
	for _, text := range []string{"first", "second"} {
		if _, err := s.AddNote(ctx, "3", text, "claude"); err != nil {
			t.Fatal(err)
		}
	}
	removed, err := s.RemoveNote(ctx, "3", "n2")
	if err != nil {
		t.Fatal(err)
	}
	if removed.Text != "second" {
		t.Errorf("the removed note should be the one it says: %+v", removed)
	}
	if len(store.notes) != 2 || !store.notes[1].Removed || store.notes[1].Text != "" || store.notes[1].ID != "n2" {
		t.Fatalf("on disk: %+v", store.notes)
	}
	if notes, _ := s.Journal("3"); len(notes) != 1 || notes[0].ID != "n1" {
		t.Errorf("a removed note is not in the journal: %+v", notes)
	}
	// Its id is spent: the next note is n3, and n2 can be neither set nor removed again.
	next, err := s.AddNote(ctx, "3", "third", "")
	if err != nil || next.ID != "n3" {
		t.Errorf("next id: %s %v", next.ID, err)
	}
	if _, err := s.EditNote(ctx, "3", "n2", "back again"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("editing a removed note: %v", err)
	}
}

// Nothing may be written that cannot be read back: a journal with a note too
// long in it would be one nobody could repair, since repairing means reading.
func TestANoteTooLongIsRefused(t *testing.T) {
	s, _ := journal(t)
	ctx := context.Background()
	long := strings.Repeat("x", domain.NoteLimit+1)
	if _, err := s.AddNote(ctx, "3", long, ""); err == nil || !strings.Contains(err.Error(), "at most") || !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("adding: %v", err)
	}
	if _, err := s.AddNote(ctx, "3", strings.Repeat("x", domain.NoteLimit), ""); err != nil {
		t.Errorf("the limit itself is allowed: %v", err)
	}
	if _, err := s.EditNote(ctx, "3", "n1", long); err == nil || !strings.Contains(err.Error(), "at most") {
		t.Errorf("editing: %v", err)
	}
}
