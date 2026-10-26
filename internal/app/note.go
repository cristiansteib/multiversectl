package app

import (
	"strings"

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
