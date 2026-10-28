package statestore

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"

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

// Delete removes only the state file; the caller owns the directory.
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

func (s *Store) Delete(id string) error {
	_ = os.Remove(s.configPath(id))
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
