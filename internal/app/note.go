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

// Journal is a universe's notes, oldest first.
func (s *Service) Journal(name string) ([]domain.Note, error) {
	st, err := s.Resolve(name)
	if err != nil {
		return nil, err
	}
	return s.Store.Notes(st.ID)
}

// AddNote appends one line, with the id and the time filled in here so that
// nothing writing a note has to invent either. It holds the universe's lock,
// so two agents adding a note at the same moment cannot choose the same id.
func (s *Service) AddNote(ctx context.Context, name, text, by string) (domain.Note, error) {
	text, by = strings.TrimSpace(text), strings.TrimSpace(by)
	if text == "" {
		return domain.Note{}, errors.New("a note needs something to say")
	}
	st, err := s.Resolve(name)
	if err != nil {
		return domain.Note{}, err
	}
	unlock, err := s.lockUniverse(ctx, st.ID, st.Name)
	if err != nil {
		return domain.Note{}, err
	}
	defer unlock()
	notes, err := s.Store.Notes(st.ID)
	if err != nil {
		return domain.Note{}, err
	}
	issued := max(st.NotesIssued, domain.HighestNoteNumber(notes)) + 1
	n := domain.Note{ID: domain.NoteID(issued), At: time.Now().UTC(), By: by, Text: text}
	if err := s.Store.AppendNote(st.ID, n); err != nil {
		return domain.Note{}, err
	}
	// Counted in the universe's own state, which a rewritten journal cannot
	// lose: the alternative is the next note taking the id of a removed one.
	st.NotesIssued = issued
	return n, s.Store.Save(st)
}

// EditNote replaces what one note says, keeping its id and the time it was
// made: the journal is an account of when things happened, not of when they
// were written down.
func (s *Service) EditNote(ctx context.Context, name, id, text string) (domain.Note, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return domain.Note{}, errors.New("a note needs something to say")
	}
	return s.changeNotes(ctx, name, id, func(notes []domain.Note, at int) ([]domain.Note, domain.Note) {
		notes[at].Text = text
		return notes, notes[at]
	})
}

// RemoveNote takes one line out. The ids of the others do not move, and the
// next note still gets an id of its own.
func (s *Service) RemoveNote(ctx context.Context, name, id string) (domain.Note, error) {
	return s.changeNotes(ctx, name, id, func(notes []domain.Note, at int) ([]domain.Note, domain.Note) {
		return append(notes[:at:at], notes[at+1:]...), notes[at]
	})
}

// changeNotes is what editing and removing have in common: find the note under
// the universe's lock, let the caller change the journal, write it back whole.
func (s *Service) changeNotes(ctx context.Context, name, id string, change func([]domain.Note, int) ([]domain.Note, domain.Note)) (domain.Note, error) {
	st, err := s.Resolve(name)
	if err != nil {
		return domain.Note{}, err
	}
	unlock, err := s.lockUniverse(ctx, st.ID, st.Name)
	if err != nil {
		return domain.Note{}, err
	}
	defer unlock()
	notes, err := s.Store.Notes(st.ID)
	if err != nil {
		return domain.Note{}, err
	}
	at, ok := domain.FindNote(notes, id)
	if !ok {
		return domain.Note{}, fmt.Errorf("universe %s has no note %s: %w", st.Name, id, domain.ErrNotFound)
	}
	notes, changed := change(notes, at)
	return changed, s.Store.SaveNotes(st.ID, notes)
}
