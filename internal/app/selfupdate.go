package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

const (
	// checksumsAsset is what the release pipeline publishes the digests as.
	checksumsAsset = "checksums.txt"
	releasesPage   = "https://github.com/cristiansteib/multiversectl/releases"
	sha256Hex      = 64
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

	// What the release says its files should hash to, read before anything is
	// downloaded: a release that cannot say is a release nothing is taken from.
	sums, err := s.checksums(ctx, latest)
	if err != nil {
		return result, err
	}

	// A machine with only the server has no command line to replace.
	if in.BinaryPath != "" {
		name := fmt.Sprintf("multiversectl_%s_%s", in.OS, in.Arch)
		url, ok := latest.Assets[name]
		if !ok {
			return result, fmt.Errorf("release %s has no asset %s: %w", latest.Version, name, domain.ErrNotFound)
		}
		if err := s.install(ctx, "Downloading multiversectl "+latest.Version, url, name, sums, in.BinaryPath); err != nil {
			return result, err
		}
		result.Updated = true
	}

	// The web view is a separate program that `server start` looks for beside
	// this one, so it has to move with it or the page runs an older release.
	serverName := fmt.Sprintf("multiversectl-server_%s_%s", in.OS, in.Arch)
	if server, ok := latest.Assets[serverName]; ok && in.ServerPath != "" {
		if err := s.install(ctx, "Downloading the web view", server, serverName, sums, in.ServerPath); err != nil {
			if result.Updated {
				return result, fmt.Errorf("multiversectl is updated, but the web view is not: %w", err)
			}
			return result, err
		}
		result.Server, result.Updated = true, true
	}
	return result, nil
}

// checksums is the release's own list of what each of its files should hash
// to.
//
// It is not proof of who built them — anyone who could swap an asset could
// swap this file with it — but it is what makes a download verifiable at all:
// a file that arrives altered is caught here instead of being run, and the
// list is what a signature covers, since the release pipeline signs it.
//
// A release with no list stops the update. There is no reason for that to
// happen, and "the check was missing, so it was skipped" is the shape of the
// problem the check exists to prevent.
func (s *Service) checksums(ctx context.Context, rel Release) (map[string]string, error) {
	url, ok := rel.Assets[checksumsAsset]
	if !ok {
		return nil, fmt.Errorf("release %s has no %s, so what it offers cannot be checked; download it by hand from %s",
			rel.Version, checksumsAsset, releasesPage)
	}
	tmp, err := s.Files.TempFile("multiversectl-checksums-*")
	if err != nil {
		return nil, err
	}
	defer s.Files.RemoveAll(tmp)
	if _, err := s.Releases.Download(ctx, url, tmp); err != nil {
		return nil, fmt.Errorf("downloading %s: %w", checksumsAsset, err)
	}
	body, err := s.Files.ReadFile(tmp)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", checksumsAsset, err)
	}
	return parseChecksums(string(body)), nil
}

// parseChecksums reads the "<hex>  <name>" lines sha256sum writes. A line it
// cannot read is skipped rather than failing the lot: the file is a list, and
// the name being looked for is either in it or it is not.
func parseChecksums(body string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(body, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || len(fields[0]) != sha256Hex {
			continue
		}
		// sha256sum marks a binary read with a leading * on the name.
		out[strings.TrimPrefix(fields[1], "*")] = strings.ToLower(fields[0])
	}
	return out
}

// install downloads to a temp file, checks it against what the release says it
// should be, and only then swaps it in: the binary is never left half-written,
// and never written at all from bytes that are not the ones published.
func (s *Service) install(ctx context.Context, label, url, asset string, sums map[string]string, binaryPath string) error {
	want, ok := sums[asset]
	if !ok {
		return fmt.Errorf("%s is not listed in %s, so it cannot be checked; download it by hand from %s",
			asset, checksumsAsset, releasesPage)
	}
	tmp, err := s.Files.TempFile("multiversectl-*")
	if err != nil {
		return err
	}
	defer s.Files.RemoveAll(tmp)

	var got string
	err = s.progress().Do(label, func(ctx context.Context) (err error) {
		got, err = s.Releases.Download(ctx, url, tmp)
		return err
	})
	if err != nil {
		return fmt.Errorf("downloading %s: %w", url, err)
	}
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("%s does not match the checksum the release publishes for it "+
			"(expected %s, got %s): nothing was installed", asset, want, got)
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
