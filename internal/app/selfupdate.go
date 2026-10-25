package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// SelfUpdateInput describes the running binary: its version, the platform it
// was built for and where it lives on disk.
type SelfUpdateInput struct {
	CurrentVersion string
	OS             string
	Arch           string
	BinaryPath     string
	// ServerPath is where the web view's program goes, beside this one; empty
	// leaves it alone. A release without it (an older one) is not an error.
	ServerPath string
}

// SelfUpdateResult says whether the binary was replaced.
type SelfUpdateResult struct {
	Updated bool
	// Server is set when the web view's program was installed or replaced too.
	Server bool
	From   string
	To     string
}

// SelfUpdate replaces the binary with the latest release when it is newer.
func (s *Service) SelfUpdate(ctx context.Context, in SelfUpdateInput) (SelfUpdateResult, error) {
	var latest Release
	err := s.progress().Do("Looking for the latest release", func(ctx context.Context) (err error) {
		latest, err = s.Releases.Latest(ctx)
		return err
	})
	if err != nil {
		return SelfUpdateResult{}, fmt.Errorf("looking up the latest release: %w", err)
	}
	result := SelfUpdateResult{From: in.CurrentVersion, To: latest.Version}
	if _, ok := parseSemver(latest.Version); !ok {
		return result, fmt.Errorf("latest release %q: not a version", latest.Version)
	}
	if !isNewer(latest.Version, in.CurrentVersion) {
		return result, nil
	}

	name := fmt.Sprintf("multiversectl_%s_%s", in.OS, in.Arch)
	url, ok := latest.Assets[name]
	if !ok {
		return result, fmt.Errorf("release %s has no asset %s: %w", latest.Version, name, domain.ErrNotFound)
	}
	if err := s.install(ctx, "Downloading multiversectl "+latest.Version, url, in.BinaryPath); err != nil {
		return result, err
	}
	result.Updated = true

	// The web view is a separate program that `server start` looks for beside
	// this one, so it has to move with it or the page runs an older release.
	if server, ok := latest.Assets[fmt.Sprintf("multiversectl-server_%s_%s", in.OS, in.Arch)]; ok && in.ServerPath != "" {
		if err := s.install(ctx, "Downloading the web view", server, in.ServerPath); err != nil {
			return result, fmt.Errorf("multiversectl is updated, but the web view is not: %w", err)
		}
		result.Server = true
	}
	return result, nil
}

// install downloads to a temp file and only then swaps it in, so the binary
// is never left half-written.
func (s *Service) install(ctx context.Context, label, url, binaryPath string) error {
	tmp, err := s.Files.TempFile("multiversectl-*")
	if err != nil {
		return err
	}
	defer s.Files.RemoveAll(tmp)

	err = s.progress().Do(label, func(ctx context.Context) error { return s.Releases.Download(ctx, url, tmp) })
	if err != nil {
		return fmt.Errorf("downloading %s: %w", url, err)
	}
	if err := s.Files.Chmod(tmp, 0o755); err != nil {
		return err
	}
	if err := s.Files.Rename(tmp, binaryPath); err == nil {
		return nil
	}

	// Rename fails across filesystems. Copy beside the target and rename that:
	// the last step is then atomic and works while the old binary is running.
	staged := binaryPath + ".new"
	defer s.Files.RemoveAll(staged)
	if err := s.Files.CopyFile(tmp, staged); err != nil {
		return fmt.Errorf("staging next to %s: %w", binaryPath, err)
	}
	if err := s.Files.Chmod(staged, 0o755); err != nil {
		return err
	}
	if err := s.Files.Rename(staged, binaryPath); err != nil {
		return fmt.Errorf("replacing %s: %w", binaryPath, err)
	}
	return nil
}

// isNewer reports whether latest is a higher version than current. A current
// version that does not parse, such as "dev", is older than everything.
// NewerVersion reports whether latest is a later release than current.
func NewerVersion(latest, current string) bool { return isNewer(latest, current) }

func isNewer(latest, current string) bool {
	l, ok := parseSemver(latest)
	if !ok {
		return false
	}
	c, ok := parseSemver(current)
	if !ok {
		return true
	}
	for i := range l {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

// parseSemver reads major.minor.patch, tolerating a leading "v" and ignoring
// any pre-release or build suffix.
func parseSemver(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// progress is the spinner, or a no-op where there is no terminal to draw on.
func (s *Service) progress() Progress {
	if s.Progress == nil {
		return noProgress{}
	}
	return s.Progress
}
