package app

import (
	"context"
	"fmt"
	"time"
)

// ManifestDrift is how the manifest an installed multiverse came from compares with
// the remote it was installed from: whether it was edited here, and how far the
// two have moved apart. It reads what was last fetched, so it is offline.
type ManifestDrift struct {
	Dir    string
	Remote string
	// Dirty is an edit in the working tree that is not committed.
	Dirty bool
	// Ahead is commits made here that the remote lacks; Behind the reverse.
	Ahead, Behind int
}

// Diverged is when neither side contains the other, so a fast-forward is out.
func (d ManifestDrift) Diverged() bool { return d.Ahead > 0 && d.Behind > 0 }

// Updatable is when `catalog update` would move the manifest and not refuse.
func (d ManifestDrift) Updatable() bool { return d.Behind > 0 && !d.Dirty && d.Ahead == 0 }

// Needs says whether the person should hear about it while working: something
// upstream they have not got. A manifest edited here and nothing else is a
// choice, not news.
func (d ManifestDrift) Needs() bool { return d.Behind > 0 }

// Notes are the short facts, for the state column of a table.
func (d ManifestDrift) Notes(asOfLastFetch bool) []string {
	var out []string
	if d.Dirty {
		out = append(out, "manifest edited here")
	}
	switch {
	case d.Diverged():
		out = append(out, fmt.Sprintf("manifest diverged (%d local, %d upstream)", d.Ahead, d.Behind))
	case d.Behind > 0:
		when := ""
		if asOfLastFetch {
			when = " (as of the last fetch)"
		}
		out = append(out, fmt.Sprintf("manifest behind %d%s", d.Behind, when))
	case d.Ahead > 0:
		out = append(out, fmt.Sprintf("manifest %d ahead of %s", d.Ahead, d.Remote))
	}
	return out
}

// Advice says what to do about it, in the person's terms, or "" when nothing is
// pending. name is the multiverse, for the command to run.
func (d ManifestDrift) Advice(name string) string {
	update := "`multiversectl catalog update " + name + "`"
	switch {
	case d.Behind == 0:
		return ""
	case d.Diverged():
		return fmt.Sprintf("its manifest has %d commit%s of yours and %d new upstream, so update cannot move it: in `%s`, `git pull --rebase`", d.Ahead, plural1(d.Ahead), d.Behind, d.Dir)
	case d.Dirty:
		return fmt.Sprintf("%d upstream change%s waiting, and its manifest is edited here, which update never overwrites: keep working, or stash your edit in `%s`, run %s, then `git stash pop`", d.Behind, plural1(d.Behind), d.Dir, update)
	case d.Ahead > 0:
		return fmt.Sprintf("%d upstream change%s waiting: in `%s`, `git pull --rebase`", d.Behind, plural1(d.Behind), d.Dir)
	}
	return fmt.Sprintf("%d upstream change%s to its manifest: %s", d.Behind, plural1(d.Behind), update)
}

func plural1(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// manifestDrift is false for a multiverse installed from a file, which has nothing
// to drift from. A manifest pinned to a tag or commit never has anything behind.
func manifestDrift(ctx context.Context, g Git, files Files, e MultiverseEntry) (ManifestDrift, bool) {
	dir := manifestClone(e.Root)
	if e.Source.Repo == "" || !files.Exists(dir) {
		return ManifestDrift{}, false
	}
	d := ManifestDrift{Dir: dir, Remote: shortRepo(e.Source.Repo)}
	if clean, err := g.IsClean(ctx, dir); err == nil && !clean {
		d.Dirty = true
	}
	if n, err := g.Behind(ctx, dir, e.Source.Ref); err == nil {
		d.Behind = n
	}
	if n, err := g.Ahead(ctx, dir, e.Source.Ref); err == nil {
		d.Ahead = n
	}
	return d, true
}

const (
	// checkEvery is how stale the last fetch may be before a command looks again.
	checkEvery = 6 * time.Hour
	// checkTimeout keeps a slow or unreachable remote from slowing a command.
	checkTimeout = 4 * time.Second
	// NoFetchEnv turns the look-again off, for scripts and offline work.
	NoFetchEnv = "MULTIVERSECTL_NO_FETCH"
)

// ManifestNotice is what to tell the person who is about to work in the multiverse
// at root: that its manifest has moved upstream, or has diverged. It looks at
// what was last fetched, and when that is older than checkEvery it fetches
// once more, briefly and quietly, so the news arrives without anyone asking.
// It never changes anything, and a failure to fetch is not worth a word.
func (s *Service) ManifestNotice(ctx context.Context) string {
	cfg, err := s.Config.Load()
	if err != nil {
		return ""
	}
	for name, e := range cfg.Multiverses {
		if e.Root != s.Multiverse {
			continue
		}
		dir := manifestClone(e.Root)
		if e.Source.Repo != "" && s.System.Getenv(NoFetchEnv) == "" && s.Files.Exists(dir) &&
			time.Since(s.Git.LastFetch(ctx, dir)) > checkEvery {
			fetchCtx, cancel := context.WithTimeout(ctx, checkTimeout)
			_ = s.Git.Fetch(fetchCtx, dir)
			cancel()
		}
		d, ok := manifestDrift(ctx, s.Git, s.Files, e)
		if !ok || !d.Needs() {
			return ""
		}
		return fmt.Sprintf("`%s`: %s", name, d.Advice(name))
	}
	return ""
}
