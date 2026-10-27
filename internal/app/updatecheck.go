package app

import (
	"context"
	"errors"
	"strings"
	"time"
)

// UpdateOptOutEnv names the environment variable that turns the look and the
// notice off, for machines that must not talk to GitHub on their own. Both the
// command line and the web view honour it.
const UpdateOptOutEnv = "MULTIVERSECTL_NO_UPDATE_CHECK"

// UpdateEvery is how often a look for a newer release is worth making. The
// web view repeats it for as long as it runs; the command line only looks when
// the last look is older than this, so a busy shell costs GitHub two requests
// an hour, not one per command.
const UpdateEvery = 30 * time.Minute

// maxHighlights is how many fixes a notice names; the count says the rest.
const maxHighlights = 5

// UpdateRecord is what the last look found: the newest releases and what each
// changed, whoever asked. It is not tied to a version of this tool, because the
// command line and the web view that share it may be on different ones.
type UpdateRecord struct {
	CheckedAt time.Time      `json:"checkedAt"`
	Releases  []ReleaseNotes `json:"releases,omitempty"` // newest first
}

// ReleaseNotes is one release boiled down to what a person deciding whether to
// update wants to know.
type ReleaseNotes struct {
	Version string   `json:"version"`
	Fixes   []string `json:"fixes,omitempty"`
	// Changes counts everything that is not a fix: features, breaking changes.
	Changes int `json:"changes,omitempty"`
}

// UpdateNotice is what a given build has missed.
type UpdateNotice struct {
	Current string `json:"current"`
	Latest  string `json:"latest"`
	// Releases is how many releases there are between the two, Latest included.
	Releases int `json:"releases"`
	Fixes    int `json:"fixes"`
	// More says the counts are a floor: every release known is newer than
	// Current, so there are older ones that were not listed.
	More       bool     `json:"more,omitempty"`
	Changes    int      `json:"changes,omitempty"`
	Highlights []string `json:"highlights,omitempty"` // newest fixes first
	URL        string   `json:"url"`
}

// Released is true for a version that came from a release, not a build from
// source, which is neither older nor newer than one and is never told to update.
func Released(v string) bool {
	v = strings.TrimPrefix(v, "v")
	return v != "" && v[0] >= '0' && v[0] <= '9'
}

// Stale is true when it is time to look again.
func (r UpdateRecord) Stale(now time.Time) bool {
	return r.CheckedAt.IsZero() || now.Sub(r.CheckedAt) >= UpdateEvery || r.CheckedAt.After(now)
}

// Notice says what current has missed; false when it is up to date, is a build
// from source, or nothing is known.
func (r UpdateRecord) Notice(current string) (UpdateNotice, bool) {
	if !Released(current) {
		return UpdateNotice{}, false
	}
	n := UpdateNotice{Current: current, URL: releasesPage}
	reached := false // a release that is not newer: the counts start from it
	for _, rel := range r.Releases {
		if !isNewer(rel.Version, current) {
			reached = true
			continue
		}
		if n.Latest == "" || isNewer(rel.Version, n.Latest) {
			n.Latest = rel.Version
		}
		n.Fixes += len(rel.Fixes)
		n.Changes += rel.Changes
		n.Highlights = append(n.Highlights, rel.Fixes...)
	}
	if n.Latest == "" {
		return UpdateNotice{}, false
	}
	n.More = !reached
	if len(n.Highlights) > maxHighlights {
		n.Highlights = n.Highlights[:maxHighlights]
	}
	return n, true
}

// CachedUpdate is what the last look found, without asking anyone: it is a
// file read, which is what lets the command line use it on its way out.
func (s *Service) CachedUpdate() (UpdateRecord, bool) {
	if s.Updates == nil {
		return UpdateRecord{}, false
	}
	return s.Updates.Load()
}

// CheckForUpdate asks for the newest releases and keeps what they say.
//
// A look that fails is kept too, as a look: without that an offline machine
// would try again on every command. What the earlier look found stays.
func (s *Service) CheckForUpdate(ctx context.Context, now time.Time) (UpdateRecord, error) {
	prev, _ := s.CachedUpdate()
	rels, err := s.Releases.Recent(ctx)
	if err != nil {
		prev.CheckedAt = now
		_ = s.saveUpdate(prev)
		return prev, err
	}
	rec := UpdateRecord{CheckedAt: now}
	for _, rel := range rels {
		rec.Releases = append(rec.Releases, ReleaseNotes{Version: rel.Version, Fixes: rel.Fixes, Changes: rel.Changes})
	}
	_ = s.saveUpdate(rec)
	return rec, nil
}

// ClaimUpdateLook marks now as the time of a look that is about to be made,
// keeping what earlier ones found. A command that ends before its look does,
// and a burst of commands that all find the record old, would otherwise each
// ask GitHub; with the claim, the first one asks and the rest see it fresh.
//
// It says whether the claim was kept. Where nothing can be kept — no cache
// directory, or one that cannot be written — no look is worth starting: every
// command would find the record old and ask again.
func (s *Service) ClaimUpdateLook(now time.Time) bool {
	prev, _ := s.CachedUpdate()
	prev.CheckedAt = now
	return s.saveUpdate(prev) == nil
}

func (s *Service) saveUpdate(r UpdateRecord) error {
	if s.Updates == nil {
		return errors.New("no place to keep it")
	}
	return s.Updates.Save(r)
}
