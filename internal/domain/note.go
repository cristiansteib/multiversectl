package domain

import (
	"strconv"
	"strings"
	"time"
)

// A Note is one line of a universe's journal: what was done in it, when, and
// who says so. Nothing has to write one — the journal is a place, not a duty —
// but when several agents and a person share a multiverse for a week, the
// order the lines were added in is the only account of what happened.
type Note struct {
	ID   string    `json:"id"`
	At   time.Time `json:"at"`
	By   string    `json:"by,omitempty"`
	Text string    `json:"text"`
	// Removed marks a note taken out: the words go, the id stays. It is what
	// keeps an id from being handed out twice without counting the journal's
	// notes anywhere outside the journal — an id quoted in a pull request
	// should never come to mean a different note.
	Removed bool `json:"removed,omitempty"`
}

// NoteLimit is as long as one note may be. A journal is an account of the
// work, not a place to paste a log into: what is kept short stays readable,
// and nothing can be written that is too long to be read back and removed.
const NoteLimit = 8 << 10

// notePrefix keeps an id short enough to type at a terminal and unmistakable
// for a date or a count: n1, n7, n12.
const notePrefix = "n"

// NoteID is the id of the nth note a universe has been given.
func NoteID(number int) string { return notePrefix + strconv.Itoa(number) }

// LiveNotes are the notes that are still there, in order: what a journal says.
// A removed one keeps its place on disk and its id, and is not one of these.
func LiveNotes(notes []Note) []Note {
	out := make([]Note, 0, len(notes))
	for _, n := range notes {
		if !n.Removed {
			out = append(out, n)
		}
	}
	return out
}

// HighestNoteNumber is the largest id a journal has given out, as a number,
// counting the notes that were removed: their ids are spent. An id in another
// shape, put there by hand or by a later version, is counted out.
func HighestNoteNumber(notes []Note) int {
	highest := 0
	for _, n := range notes {
		if number, err := strconv.Atoi(strings.TrimPrefix(n.ID, notePrefix)); err == nil && number > highest {
			highest = number
		}
	}
	return highest
}

// FindNote is where the note with that id is, and whether it is there to be
// changed. An id is taken with or without its prefix, so `rm 7` works as well
// as `rm n7`; one that was removed is not found, its id being spent.
func FindNote(notes []Note, id string) (int, bool) {
	id = strings.TrimPrefix(strings.TrimSpace(id), notePrefix)
	for i, n := range notes {
		if !n.Removed && strings.TrimPrefix(n.ID, notePrefix) == id {
			return i, true
		}
	}
	return 0, false
}
