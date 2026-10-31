package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// NoteInput says what to change about a universe's note. A nil field is left as
// it is, and an empty one clears it.
type NoteInput struct {
	Text *string
	PR   *string
}

// SetNote records why a universe exists and where its work is going, so that
// whoever finds it later, a person or another agent, does not have to guess.
// It is state the tool already keeps; nothing else about the universe changes.
func (s *Service) SetNote(name string, in NoteInput) (domain.UniverseState, error) {
	st, err := s.Resolve(name)
	if err != nil {
		return st, err
	}
	if in.Text != nil {
		st.Note = strings.TrimSpace(*in.Text)
	}
	if in.PR != nil {
		st.PR = strings.TrimSpace(*in.PR)
	}
	return st, s.Store.Save(st)
}

// Note reads a universe's note and PR.
func (s *Service) Note(name string) (text, pr string, err error) {
	st, err := s.Resolve(name)
	return st.Note, st.PR, err
}

// The journal
//
// The note above is one line: what the universe is for. The journal is the
// rest — what has been done in it, in the order it happened. An agent may add
// to it and is never made to: a universe nobody wrote about is normal, and an
// empty journal is not a failing. What it buys, when it is used, is that the
// next person or agent to open the universe reads the account instead of
// reconstructing it from branches and containers.

// Journal is what a universe's journal says: its notes, oldest first. A note
// that was removed keeps its place on disk and is not one of them.
func (s *Service) Journal(name string) ([]domain.Note, error) {
	st, err := s.Resolve(name)
	if err != nil {
		return nil, err
	}
	notes, err := s.Store.Notes(st.ID)
	if err != nil {
		return nil, err
	}
	return domain.LiveNotes(notes), nil
}

// noteWords is what a note may say: trimmed, not empty, and short enough to be
// read back and taken out again.
func noteWords(text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", domain.Invalid("a note needs something to say")
	}
	if len(text) > domain.NoteLimit {
		return "", domain.Invalid("a note is at most %d characters, and this one is %d: keep the account short, and the detail where the work is", domain.NoteLimit, len(text))
	}
	return text, nil
}

// noteTries bounds a race that nobody should lose this often: each turn of it
// is another writer having taken the id in the moment between reading the
// journal and writing the note.
const noteTries = 50

// AddNote writes one line, with the id and the time filled in here so that
// nothing writing a note has to invent either.
//
// It takes no lock. A journal will not hold two notes under one id, so the
// writer that loses a race is told and takes the next id — and the id is the
// only thing two writers contend for. That matters because `up` holds the
// universe's lock for as long as it builds, and the moment one most wants to
// write down what is happening is while something long is happening.
func (s *Service) AddNote(_ context.Context, name, text, by string) (domain.Note, error) {
	text, err := noteWords(text)
	if err != nil {
		return domain.Note{}, err
	}
	st, err := s.Resolve(name)
	if err != nil {
		return domain.Note{}, err
	}
	notes, err := s.Store.Notes(st.ID)
	if err != nil {
		return domain.Note{}, err
	}
	next := domain.HighestNoteNumber(notes) + 1
	for try := 0; try < noteTries; try++ {
		made := domain.Note{
			ID:   domain.NoteID(next + try),
			At:   time.Now().UTC(),
			By:   strings.TrimSpace(by),
			Text: text,
		}
		switch err := s.Store.CreateNote(st.ID, made); {
		case err == nil:
			return made, nil
		case errors.Is(err, domain.ErrExists): // taken since the journal was read: try the next
		default:
			return domain.Note{}, err
		}
	}
	return domain.Note{}, fmt.Errorf("universe %s: no id free for a note after %d tries, from %s", st.Name, noteTries, domain.NoteID(next))
}

// EditNote replaces what one note says, keeping its id and the time it was
// made: the journal is an account of when things happened, not of when they
// were written down.
func (s *Service) EditNote(ctx context.Context, name, id, text string) (domain.Note, error) {
	text, err := noteWords(text)
	if err != nil {
		return domain.Note{}, err
	}
	return s.changeNote(ctx, name, id, func(n domain.Note) domain.Note {
		n.Text = text
		return n
	})
}

// RemoveNote takes the words of one note out. Its id stays behind, spent: a
// journal that forgot an id would hand it out again, and an id somebody quoted
// in a pull request would come to mean another note.
func (s *Service) RemoveNote(ctx context.Context, name, id string) (domain.Note, error) {
	var removed domain.Note
	if _, err := s.changeNote(ctx, name, id, func(n domain.Note) domain.Note {
		removed = n
		return domain.Note{ID: n.ID, At: n.At, Removed: true}
	}); err != nil {
		return domain.Note{}, err
	}
	return removed, nil
}

// changeNote is what editing and removing have in common: find the note under
// the universe's lock, let the caller say what it becomes, write that note.
// The lock is taken here and not for adding one, because these change a note
// somebody may be reading, and because the id is already settled.
func (s *Service) changeNote(ctx context.Context, name, id string, change func(domain.Note) domain.Note) (domain.Note, error) {
	var changed domain.Note
	err := s.withUniverse(ctx, name, func(st domain.UniverseState) error {
		notes, err := s.Store.Notes(st.ID)
		if err != nil {
			return err
		}
		at, ok := domain.FindNote(notes, id)
		if !ok {
			return fmt.Errorf("universe %s has no note %s: %w", st.Name, id, domain.ErrNotFound)
		}
		changed = change(notes[at])
		return s.Store.SaveNote(st.ID, changed)
	})
	return changed, err
}
