package statestore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/cristiansteib/multiversectl/internal/app"
	"github.com/cristiansteib/multiversectl/internal/domain"
)

const (
	stateFile  = "universe.yaml"
	configFile = "started-with.multiverse.yaml"
)

type Store struct{ root string }

var _ app.Store = (*Store)(nil)

func New(root string) *Store { return &Store{root: root} }

func (s *Store) universes() string { return filepath.Join(s.root, "universes") }

func (s *Store) path(id string) string {
	return filepath.Join(s.universes(), id, stateFile)
}

func (s *Store) Save(st domain.UniverseState) error {
	data, err := yaml.Marshal(st)
	if err != nil {
		return fmt.Errorf("encode universe %s: %w", st.Name, err)
	}
	if err := writeAtomic(s.path(st.ID), data); err != nil {
		return fmt.Errorf("save universe %s: %w", st.Name, err)
	}
	return nil
}

func (s *Store) Load(id string) (domain.UniverseState, error) {
	var st domain.UniverseState
	data, err := os.ReadFile(s.path(id))
	if errors.Is(err, fs.ErrNotExist) {
		return st, fmt.Errorf("universe %s: %w", id, domain.ErrNotFound)
	}
	if err != nil {
		return st, fmt.Errorf("read universe %s: %w", id, err)
	}
	if err := yaml.Unmarshal(data, &st); err != nil {
		return st, fmt.Errorf("parse %s: %w", s.path(id), err)
	}
	return fill(st, id), nil
}

// fill supplies the identity a state file written before universes could be
// named does not carry, so an existing numbered universe keeps working
// without being rewritten or moved.
func fill(st domain.UniverseState, id string) domain.UniverseState {
	if st.Name == "" {
		if st.Number > 0 {
			st.Name = strconv.Itoa(st.Number)
		} else {
			st.Name = id
		}
	}
	u := domain.Universe{Name: st.Name}
	if st.ID == "" {
		st.ID = u.ID()
	}
	if st.Slug == "" {
		st.Slug = u.Slug()
	}
	return st
}

// List returns every universe, numbered ones in order and then named ones
// alphabetically: a listing people read should not jump from 2 to 10.
func (s *Store) List() ([]domain.UniverseState, error) {
	entries, err := os.ReadDir(s.universes())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list universes in %s: %w", s.universes(), err)
	}
	var out []domain.UniverseState
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(s.path(e.Name())); err != nil {
			continue
		}
		st, err := s.Load(e.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := domain.UniverseNumber(out[i].Name), domain.UniverseNumber(out[j].Name)
		switch {
		case a > 0 && b > 0:
			return a < b
		case a > 0 || b > 0:
			return a > 0
		default:
			return out[i].Name < out[j].Name
		}
	})
	return out, nil
}

// configPath is where the manifest a universe was started with is kept: beside
// its state, in a file of its own so that universe.yaml stays short to read.
func (s *Store) configPath(id string) string {
	return filepath.Join(s.universes(), id, configFile)
}

func (s *Store) SaveConfig(id string, data []byte) error {
	if err := writeAtomic(s.configPath(id), data); err != nil {
		return fmt.Errorf("keep the manifest of universe %s: %w", id, err)
	}
	return nil
}

func (s *Store) LoadConfig(id string) ([]byte, error) {
	data, err := os.ReadFile(s.configPath(id))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("universe %s has no kept manifest: %w", id, domain.ErrNotFound)
	}
	return data, err
}

// Delete removes what the store wrote — the state, the kept manifest and the
// journal — and not the universe's directory itself: the worktrees in it are
// the caller's to take down first.
func (s *Store) Delete(id string) error {
	_ = os.Remove(s.configPath(id))
	_ = os.RemoveAll(s.ownPath(id))
	err := os.Remove(s.path(id))
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("universe %s: %w", id, domain.ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("delete universe %s: %w", id, err)
	}
	return nil
}

func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// The journal: a directory of notes, one file to a note
//
// A note is its own file, named for its id — .multiverse/notes/n7.json — so
// nothing that writes one has to coordinate with anything that writes
// another. Two processes adding a note at the same moment write two different
// files; changing or removing one touches that file alone, never a file
// somebody else has open; and no note is ever part of a document that has to
// be rewritten whole to change one line of it.
const (
	// ownDir is what belongs to multiversectl inside a universe, beside the
	// repository checkouts rather than among them: the journal, and whatever
	// else is worth keeping per universe later. It is the name the multiverse
	// root already uses for the same purpose, and being dotted it cannot be
	// taken for, or collide with, a repository called `multiverse`.
	ownDir   = ".multiverse"
	notesDir = "notes"
	noteExt  = ".json"
)

func (s *Store) ownPath(id string) string { return filepath.Join(s.universes(), id, ownDir) }

func (s *Store) notesPath(id string) string { return filepath.Join(s.ownPath(id), notesDir) }

func (s *Store) notePath(id, noteID string) string {
	return filepath.Join(s.notesPath(id), noteID+noteExt)
}

// Notes reads the journal, oldest first: ids are handed out in order, so their
// order is the order things happened in. Anything in the directory that is not
// a note — the temporary file of a write in flight, something somebody left
// there — is passed over rather than read.
func (s *Store) Notes(id string) ([]domain.Note, error) {
	dir := s.notesPath(id)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil // a universe nobody wrote about
	}
	if err != nil {
		return nil, fmt.Errorf("read the journal of universe %s: %w", id, err)
	}
	out := make([]domain.Note, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), noteExt) {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read note %s of universe %s: %w", e.Name(), id, err)
		}
		// An id taken by a write that never landed: nothing to read, and the
		// id stays spent. One note must not make a whole journal unreadable.
		if len(bytes.TrimSpace(data)) == 0 {
			continue
		}
		var n domain.Note
		if err := json.Unmarshal(data, &n); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := domain.NoteNumber(out[i].ID), domain.NoteNumber(out[j].ID)
		if a != b {
			return a < b
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// CreateNote writes a note that is not there yet. Two processes adding one at
// the same moment can pick the same id; the directory settles it, and the one
// that loses is told the id is taken rather than writing over the other's
// note.
func (s *Store) CreateNote(id string, n domain.Note) error {
	data, err := json.Marshal(n)
	if err != nil {
		return fmt.Errorf("encode the note: %w", err)
	}
	if err := s.makeNotesDir(id); err != nil {
		return err
	}
	f, err := os.OpenFile(s.notePath(id, n.ID), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("universe %s already has a note %s: %w", id, n.ID, domain.ErrExists)
	}
	if err != nil {
		return fmt.Errorf("write note %s of universe %s: %w", n.ID, id, err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("write note %s of universe %s: %w", n.ID, id, err)
	}
	return f.Close()
}

// makeNotesDir makes the journal's directory inside a universe that is there,
// one level at a time: making the whole path would make the universe's own
// directory too, and so bring back a universe that was removed while a note
// was being written for it.
func (s *Store) makeNotesDir(id string) error {
	for _, dir := range []string{s.ownPath(id), s.notesPath(id)} {
		switch err := os.Mkdir(dir, 0o755); {
		case err == nil, errors.Is(err, fs.ErrExist):
		case errors.Is(err, fs.ErrNotExist):
			return fmt.Errorf("universe %s: %w", id, domain.ErrNotFound)
		default:
			return err
		}
	}
	return nil
}

// SaveNote writes over one note: what it says has changed, or its words are
// gone and a tombstone is left in its place. Only that note's file is
// touched, so a change cannot undo a note written beside it meanwhile.
func (s *Store) SaveNote(id string, n domain.Note) error {
	data, err := json.Marshal(n)
	if err != nil {
		return fmt.Errorf("encode the note: %w", err)
	}
	if err := writeAtomic(s.notePath(id, n.ID), data); err != nil {
		return fmt.Errorf("write note %s of universe %s: %w", n.ID, id, err)
	}
	return nil
}
