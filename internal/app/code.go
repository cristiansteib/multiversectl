package app

import (
	"context"
	"sort"
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
	checkouts := s.checkouts(st)
	out := make([]CodeStatus, len(checkouts))
	var wg sync.WaitGroup
	for i, c := range checkouts {
		wg.Add(1)
		go func(i int, c Checkout) {
			defer wg.Done()
			out[i] = s.codeOf(ctx, c)
		}(i, c)
	}
	wg.Wait()
	sort.Slice(out, func(i, j int) bool { return out[i].Repo < out[j].Repo })
	return out, nil
}

func (s *Service) codeOf(ctx context.Context, c Checkout) CodeStatus {
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
	if d, err := s.Git.DiffStat(ctx, c.Path, cs.Follows); err == nil {
		cs.Work = d
	}
	if cs.Dirty {
		if d, err := s.Git.DiffStat(ctx, c.Path, ""); err == nil {
			cs.Uncommitted = d
		}
	}
	cs.Review, cs.ReviewError = s.reviewOf(ctx, cs.URL, c.Branch)
	return cs
}

// reviewOf asks the service the repository is on, and only that one: a URL no
// host owns is a repository nobody can have opened a review on.
func (s *Service) reviewOf(ctx context.Context, repoURL, branch string) (*Review, string) {
	if repoURL == "" || branch == "" {
		return nil, ""
	}
	for _, h := range s.Hosts {
		if !h.Owns(repoURL) {
			continue
		}
		// A service that is not installed or not signed in is not an error
		// worth showing on every repository of every universe.
		if err := h.Available(ctx); err != nil {
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
