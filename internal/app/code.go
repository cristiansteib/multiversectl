package app

import (
	"context"
	"sync"
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
	// Work is the committed change against Follows; Uncommitted is what is
	// edited in the checkout and not committed yet.
	Work        DiffStat
	Uncommitted DiffStat
	// Review is the open pull or merge request, nil when there is none or
	// when no hosting service was asked.
	Review *Review
	// ReviewError is why the service could not answer, which is not the same
	// as there being no review and must not be drawn as one.
	ReviewError string
}

// UniverseCode reads every repository of a universe at once. Each is a handful
// of git calls and at most one call over the network, and a project with six
// repositories would otherwise cost their sum.
//
// Nothing here fetches: it reports against what the last fetch brought in, as
// the rest of the listing does, so opening a page never reaches out to a
// remote on its own.
func (s *Service) UniverseCode(ctx context.Context, name string) ([]CodeStatus, error) {
	st, err := s.Resolve(name)
	if err != nil {
		return nil, err
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
	clone := s.Manifest.CloneDir(s.Workspace, c.Repo)
	cs.Freshness = s.freshnessOf(ctx, c.Path, s.followed(ctx, clone, c.Branch))
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
		r, err := h.OpenReview(ctx, repoURL, branch)
		if err != nil {
			return nil, err.Error()
		}
		return r, ""
	}
	return nil, ""
}
