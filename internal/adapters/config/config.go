package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/cristiansteib/multiversectl/internal/app"
)

type Store struct{ path string }

var _ app.ConfigStore = (*Store)(nil)

// New resolves $XDG_CONFIG_HOME/multiversectl/config.yaml, falling back to
// ~/.config.
func New() (*Store, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("locate config directory: %w", err)
		}
		base = filepath.Join(home, ".config")
	}
	return &Store{path: filepath.Join(base, "multiversectl", "config.yaml")}, nil
}

func (s *Store) Path() string { return s.path }

// Load returns a zero Config when the file does not exist yet, and upgrades
// the single-multiverse layout older releases wrote.
func (s *Store) Load() (app.Config, error) {
	var c app.Config
	data, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, fmt.Errorf("read config %s: %w", s.path, err)
	}
	if err := yaml.Unmarshal(data, &c); err != nil {
		return c, fmt.Errorf("parse config %s: %w", s.path, err)
	}
	return c, nil
}

func (s *Store) Save(c app.Config) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if err := writeAtomic(s.path, data, 0o600); err != nil {
		return fmt.Errorf("save config %s: %w", s.path, err)
	}
	return nil
}

func writeAtomic(path string, data []byte, perm fs.FileMode) error {
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
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
