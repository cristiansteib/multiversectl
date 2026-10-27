package app

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// CodeStatus is what has become of one repository inside a universe: the
// branch it is on, how it stands against the branch it follows, how much work
// that is, and the review opened for it. It is what someone looking at a
// universe wants to know before opening a terminal.
type CodeStatus struct {
	Repo   string
	App    string
	Branch string
	Path   string
	// URL is the repository on its hosting service, as the manifest names it.
	URL string
	Freshness
	// Work is the committed change against Follows, which once the branch is
	// pushed is its own counterpart: the commits not pushed yet. Change is what
	// the branch amounts to against Base, which is what a review shows.
	// Uncommitted is what is edited in the checkout and not committed yet.
	Work        DiffStat
	Change      DiffStat
	Base        string
	Uncommitted DiffStat
	// Fetched is when the clone last asked its remote, which is how old every
	// number here that is measured against a remote can be.
	Fetched time.Time
	// Review is the pull or merge request of the branch, nil when there is none
	// or when no hosting service was asked.
	Review *Review
	// ReviewError is why the service could not answer, which is not the same
	// as there being no review and must not be drawn as one.
	ReviewError string
}

// UniverseCode reads every repository of a universe at once. Each is a handful
// of git calls and at most one call over the network, and a multiverse with six
// repositories would otherwise cost their sum.
//
// Unless fetch is set nothing here fetches: it reports against what the last
// fetch brought in, as the rest of the listing does, so opening a page never
// reaches out to a remote on its own. Asking to reread is the decision to.
func (s *Service) UniverseCode(ctx context.Context, name string, fetch bool) ([]CodeStatus, error) {
	st, err := s.Resolve(name)
	if err != nil {
		return nil, err
	}
	if fetch {
		s.refetch(ctx, st)
	}
	// Asking a service whether it can be used is a process of its own and a
	// call over the network; one answer serves every repository of the
	// universe, so each host is asked once however many repositories it owns.
	ready := make([]func() error, len(s.Hosts))
	for i := range s.Hosts {
		host := s.Hosts[i]
		ready[i] = sync.OnceValue(func() error { return host.Available(ctx) })
	}
	// checkouts comes in the repositories' own order, which is the order the
	// rows keep.
	checkouts := s.checkouts(st)
	out := make([]CodeStatus, len(checkouts))
	var wg sync.WaitGroup
	for i, c := range checkouts {
		wg.Add(1)
		go func(i int, c Checkout) {
			defer wg.Done()
			out[i] = s.codeOf(ctx, c, ready)
		}(i, c)
	}
	wg.Wait()
	return out, nil
}

// refetch brings in what the remotes have, once per clone: the universe's
// worktrees share it, and a clone that cannot be reached keeps what it had,
// which the Fetched time then says.
func (s *Service) refetch(ctx context.Context, st domain.UniverseState) {
	if s.Manifest == nil {
		return
	}
	var wg sync.WaitGroup
	seen := map[string]bool{}
	for _, c := range s.checkouts(st) {
		clone := s.Manifest.CloneDir(s.Multiverse, c.Repo)
		if seen[clone] || !s.Files.Exists(clone) {
			continue
		}
		seen[clone] = true
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.Git.Fetch(ctx, clone)
		}()
	}
	wg.Wait()
}

func (s *Service) codeOf(ctx context.Context, c Checkout, ready []func() error) CodeStatus {
	cs := CodeStatus{Repo: c.Repo, App: c.App, Branch: c.Branch, Path: c.Path}
	if s.Manifest == nil {
		return cs
	}
	if r, ok := s.Manifest.Repos[c.Repo]; ok {
		cs.URL = r.URL
	}
	if !s.Files.Exists(c.Path) {
		return cs
	}
	clone := s.Manifest.CloneDir(s.Multiverse, c.Repo)
	cs.Freshness = s.freshnessOf(ctx, c.Path, s.followed(ctx, clone, c.Branch))
	cs.Fetched = s.Git.LastFetch(ctx, clone)
	// Only with a branch to measure against: asked with none, DiffStat answers
	// a different question — what is edited and not committed — and the page
	// would draw that as work that had been committed. A repository whose
	// clone has no usable origin has nothing to be measured against, so it is
	// only asked whether it has been edited.
	if cs.Follows != "" {
		if d, err := s.Git.DiffStat(ctx, c.Path, cs.Follows); err == nil {
			cs.Work = d
		}
	} else if clean, err := s.Git.IsClean(ctx, c.Path); err == nil {
		cs.Dirty = !clean
	}
	if cs.Dirty {
		if d, err := s.Git.DiffStat(ctx, c.Path, ""); err == nil {
			cs.Uncommitted = d
		}
	}
	cs.Review, cs.ReviewError = s.reviewOf(ctx, ready, cs.URL, c.Branch)
	// What the branch amounts to is measured against what it is to be merged
	// into: the review says which, and without one it is the default branch.
	cs.Base = s.baseOf(ctx, clone, cs.Review)
	if cs.Base != "" {
		if d, err := s.Git.DiffStat(ctx, c.Path, cs.Base); err == nil {
			cs.Change = d
		}
	}
	return cs
}

// reviewOf asks the service the repository is on, and only that one: a URL no
// host owns is a repository nobody can have opened a review on.
func (s *Service) reviewOf(ctx context.Context, ready []func() error, repoURL, branch string) (*Review, string) {
	if repoURL == "" || branch == "" {
		return nil, ""
	}
	for i, h := range s.Hosts {
		if !h.Owns(repoURL) {
			continue
		}
		// A service that is not installed or not signed in is not an error
		// worth showing on every repository of every universe.
		if err := ready[i](); err != nil {
			return nil, ""
		}
		r, err := h.FindReview(ctx, repoURL, branch)
		if err != nil {
			return nil, err.Error()
		}
		return r, ""
	}
	return nil, ""
}

func (s *Service) baseOf(ctx context.Context, clone string, r *Review) string {
	if r != nil && r.Base != "" {
		return r.Base
	}
	if def, err := s.Git.DefaultBranch(ctx, clone); err == nil {
		return def
	}
	return ""
}

// DiffScope says which change a diff is of.
const (
	// ScopeBranch is the branch against the one it is to be merged into.
	ScopeBranch = "branch"
	// ScopeUncommitted is what is edited and not committed.
	ScopeUncommitted = "uncommitted"
)

// UniverseDiff lists the files of one repository's change, and names what it
// is measured against.
func (s *Service) UniverseDiff(ctx context.Context, name, repo, scope string) (base string, files []FileChange, err error) {
	path, ref, err := s.diffTarget(ctx, name, repo, scope)
	if err != nil {
		return "", nil, err
	}
	files, err = s.Git.DiffFiles(ctx, path, ref)
	return ref, files, err
}

// UniverseDiffFile is the diff of one file of that list. The file has to be in
// it: what is read is what the listing showed, not any path a caller names.
func (s *Service) UniverseDiffFile(ctx context.Context, name, repo, scope, file string, limit int) (patch string, truncated bool, err error) {
	path, ref, err := s.diffTarget(ctx, name, repo, scope)
	if err != nil {
		return "", false, err
	}
	files, err := s.Git.DiffFiles(ctx, path, ref)
	if err != nil {
		return "", false, err
	}
	for _, f := range files {
		if f.Path == file {
			return s.Git.DiffPatch(ctx, path, ref, file, limit)
		}
	}
	return "", false, fmt.Errorf("%s is not in the change: %w", file, domain.ErrNotFound)
}

// diffTarget finds the checkout of a repository of the universe, and the ref
// its change is measured against: none for what is uncommitted.
func (s *Service) diffTarget(ctx context.Context, name, repo, scope string) (path, ref string, err error) {
	st, err := s.Resolve(name)
	if err != nil {
		return "", "", err
	}
	for _, c := range s.checkouts(st) {
		if c.Repo != repo {
			continue
		}
		if s.Manifest == nil || !s.Files.Exists(c.Path) {
			break
		}
		switch scope {
		case ScopeUncommitted:
			return c.Path, "", nil
		case ScopeBranch:
			clone := s.Manifest.CloneDir(s.Multiverse, c.Repo)
			var review *Review
			if r, ok := s.Manifest.Repos[c.Repo]; ok {
				ready := make([]func() error, len(s.Hosts))
				for i := range s.Hosts {
					host := s.Hosts[i]
					ready[i] = sync.OnceValue(func() error { return host.Available(ctx) })
				}
				review, _ = s.reviewOf(ctx, ready, r.URL, c.Branch)
			}
			if base := s.baseOf(ctx, clone, review); base != "" {
				return c.Path, base, nil
			}
			return "", "", fmt.Errorf("%s has no branch to measure against", repo)
		default:
			return "", "", fmt.Errorf("scope %q: want %s or %s", scope, ScopeBranch, ScopeUncommitted)
		}
	}
	return "", "", fmt.Errorf("repository %s is not checked out in universe %s: %w", repo, name, domain.ErrNotFound)
}
