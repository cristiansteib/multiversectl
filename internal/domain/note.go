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
}

// notePrefix keeps an id short enough to type at a terminal and unmistakable
// for a date or a count: n1, n7, n12.
const notePrefix = "n"

// NoteID is the id of the nth note a universe has been given.
func NoteID(number int) string { return notePrefix + strconv.Itoa(number) }

// HighestNoteNumber is the largest id in a journal, as a number. It is the
// fallback for a universe whose state does not count the notes it has issued
// yet — one written before there were journals. An id in another shape, put
// there by hand or by a later version, is counted out.
func HighestNoteNumber(notes []Note) int {
	highest := 0
	for _, n := range notes {
		if number, err := strconv.Atoi(strings.TrimPrefix(n.ID, notePrefix)); err == nil && number > highest {
			highest = number
		}
	}
	return highest
}

// FindNote is the note with that id, and whether it is there. An id is taken
// with or without its prefix, so `rm 7` works as well as `rm n7`.
func FindNote(notes []Note, id string) (int, bool) {
	id = strings.TrimPrefix(strings.TrimSpace(id), notePrefix)
	for i, n := range notes {
		if strings.TrimPrefix(n.ID, notePrefix) == id {
			return i, true
		}
	}
	return 0, false
}
