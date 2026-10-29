// Package updatecache keeps what the last look for a newer release found, in
// the user's cache directory. The command line and the web view are separate
// processes; this file is how a look made by one helps the other.
package updatecache

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/cristiansteib/multiversectl/internal/adapters/userdirs"
	"github.com/cristiansteib/multiversectl/internal/app"
)

type Store struct{ path string }

var _ app.UpdateStore = (*Store)(nil)

// New resolves $XDG_CACHE_HOME/multiversectl/update.json, falling back to the
// platform's cache directory.
func New() (*Store, error) {
	base, err := userdirs.Cache()
	if err != nil {
		return nil, err
	}
	return &Store{path: filepath.Join(base, "multiversectl", "update.json")}, nil
}

// At keeps it at path; for tests.
func At(path string) *Store { return &Store{path: path} }

func (s *Store) Load() (app.UpdateRecord, bool) {
	var r app.UpdateRecord
	data, err := os.ReadFile(s.path)
	if err != nil || json.Unmarshal(data, &r) != nil {
		return app.UpdateRecord{}, false
	}
	return r, true
}

// Save writes beside the file and renames, so a command reading while another
// writes sees one whole record or the other, never half of one.
func (s *Store) Save(r app.UpdateRecord) error {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".update-*")
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
	return os.Rename(tmp.Name(), s.path)
}
