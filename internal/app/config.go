package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"time"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// ConfigSHA is the fingerprint of manifest text: what says two are the same.
func ConfigSHA(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// recordConfig keeps the manifest a universe is being started with and says
// which it was, so a later edit of the file does not hide what the universe is
// running on.
func (s *Service) recordConfig(id string) (domain.ConfigRecord, error) {
	data, err := s.Files.ReadFile(s.Manifest.Path)
	if err != nil {
		return domain.ConfigRecord{}, err
	}
	if err := s.Store.SaveConfig(id, data); err != nil {
		return domain.ConfigRecord{}, err
	}
	return domain.ConfigRecord{SHA256: ConfigSHA(data), At: time.Now().UTC(), Version: s.Version}, nil
}

// ConfigReport is the manifest a universe uses, with what it was last started
// with beside it, and what is wrong with either, by the same checks that load it.
type ConfigReport struct {
	Path     string
	Current  []byte
	SHA256   string
	Problems []string
	// StartedWith is nil for a universe that has not been started since this was kept.
	StartedWith *domain.ConfigRecord
	// Snapshot is the manifest text as it was then, nil when it was not kept.
	Snapshot         []byte
	SnapshotProblems []string
	// Changed is whether the manifest is not the one it was started with.
	Changed bool
}

// UniverseConfig reports the manifest of a universe against the one it was started with.
// The file is read as it is now, edited or not.
func (s *Service) UniverseConfig(universe string) (ConfigReport, error) {
	st, err := s.Resolve(universe)
	if err != nil {
		return ConfigReport{}, err
	}
	data, err := s.Files.ReadFile(s.Manifest.Path)
	if err != nil {
		return ConfigReport{}, err
	}
	rep := ConfigReport{Path: s.Manifest.Path, Current: data, SHA256: ConfigSHA(data), Problems: s.checkManifest(data)}
	if st.StartedWith.SHA256 != "" {
		rec := st.StartedWith
		rep.StartedWith = &rec
		rep.Changed = rec.SHA256 != rep.SHA256
		if snap, err := s.Store.LoadConfig(st.ID); err == nil {
			rep.Snapshot, rep.SnapshotProblems = snap, s.checkManifest(snap)
		} else if !errors.Is(err, domain.ErrNotFound) {
			return rep, err
		}
	}
	return rep, nil
}

// checkManifest runs the checks that loading a manifest runs, on text, and says
// every problem it has, in one list.
func (s *Service) checkManifest(data []byte) []string {
	_, err := s.Manifests.Decode(data, filepath.Dir(s.Manifest.Path))
	if err == nil {
		return nil
	}
	var invalid *domain.ValidationError
	if errors.As(err, &invalid) {
		return invalid.Problems
	}
	return []string{err.Error()}
}
