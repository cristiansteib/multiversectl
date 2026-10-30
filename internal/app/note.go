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
		return "", errors.New("a note needs something to say")
	}
	if len(text) > domain.NoteLimit {
		return "", fmt.Errorf("a note is at most %d characters, and this one is %d: keep the account short, and the detail where the work is", domain.NoteLimit, len(text))
	}
	return text, nil
}

// AddNote appends one line, with the id and the time filled in here so that
// nothing writing a note has to invent either. It holds the universe's lock,
// so two agents adding a note at the same moment cannot choose the same id.
func (s *Service) AddNote(ctx context.Context, name, text, by string) (domain.Note, error) {
	text, err := noteWords(text)
	if err != nil {
		return domain.Note{}, err
	}
	var made domain.Note
	if err := s.withUniverse(ctx, name, func(st domain.UniverseState) error {
		notes, err := s.Store.Notes(st.ID)
		if err != nil {
			return err
		}
		made = domain.Note{
			ID:   domain.NoteID(domain.HighestNoteNumber(notes) + 1),
			At:   time.Now().UTC(),
			By:   strings.TrimSpace(by),
			Text: text,
		}
		return s.Store.AppendNote(st.ID, made)
	}); err != nil {
		return domain.Note{}, err
	}
	return made, nil
}

// EditNote replaces what one note says, keeping its id and the time it was
// made: the journal is an account of when things happened, not of when they
// were written down.
func (s *Service) EditNote(ctx context.Context, name, id, text string) (domain.Note, error) {
	text, err := noteWords(text)
	if err != nil {
		return domain.Note{}, err
	}
	return s.changeNotes(ctx, name, id, func(notes []domain.Note, at int) []domain.Note {
		notes[at].Text = text
		return notes
	})
}

// RemoveNote takes the words of one note out. Its id stays behind, spent: a
// journal that forgot an id would hand it out again, and an id somebody quoted
// in a pull request would come to mean another note.
func (s *Service) RemoveNote(ctx context.Context, name, id string) (domain.Note, error) {
	var removed domain.Note
	if _, err := s.changeNotes(ctx, name, id, func(notes []domain.Note, at int) []domain.Note {
		removed = notes[at]
		notes[at] = domain.Note{ID: removed.ID, At: removed.At, Removed: true}
		return notes
	}); err != nil {
		return domain.Note{}, err
	}
	return removed, nil
}

// changeNotes is what editing and removing have in common: find the note under
// the universe's lock, let the caller change the journal, write it back whole.
func (s *Service) changeNotes(ctx context.Context, name, id string, change func([]domain.Note, int) []domain.Note) (domain.Note, error) {
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
		saved := change(notes, at)
		changed = notes[at]
		return s.Store.SaveNotes(st.ID, saved)
	})
	return changed, err
}
