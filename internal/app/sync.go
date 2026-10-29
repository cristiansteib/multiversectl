package app

import (
	"context"
	"fmt"
	"github.com/cristiansteib/multiversectl/internal/domain"
	"sort"
	"strings"
)

// Freshness is how a worktree or a clone stands against the branch it follows,
// as of the last fetch. It is what tells someone that the code they run is not
// the code that is on main now.
type Freshness struct {
	// Follows is the branch of origin it is compared with.
	Follows string `json:"follows,omitempty"`
	Behind  int    `json:"behind,omitempty"`
	Ahead   int    `json:"ahead,omitempty"`
	// Dirty means uncommitted changes, which no update touches.
	Dirty bool `json:"dirty,omitempty"`
}

// Stale says the checkout lacks commits it could have.
func (f Freshness) Stale() bool { return f.Behind > 0 }

// followed is the branch of origin a universe's checkout is compared with: its
// own counterpart once it has been pushed, otherwise the branch it started from.
func (s *Service) followed(ctx context.Context, repoKey, clone, branch string) string {
	if branch != "" && !strings.HasPrefix(branch, "origin/") {
		// Asking for the distance to origin/<branch> fails when it has none.
		if _, err := s.Git.Behind(ctx, clone, branch); err == nil {
			return branch
		}
	}
	if strings.HasPrefix(branch, "origin/") { // a detached checkout of a base
		return strings.TrimPrefix(branch, "origin/")
	}
	def, err := s.baseBranch(ctx, repoKey, clone)
	if err != nil {
		return ""
	}
	return def
}

// freshnessOf measures one directory against what it follows.
func (s *Service) freshnessOf(ctx context.Context, dir, follows string) Freshness {
	f := Freshness{Follows: follows}
	if follows == "" {
		return f
	}
	if n, err := s.Git.Behind(ctx, dir, follows); err == nil {
		f.Behind = n
	}
	if n, err := s.Git.Ahead(ctx, dir, follows); err == nil {
		f.Ahead = n
	}
	if clean, err := s.Git.IsClean(ctx, dir); err == nil {
		f.Dirty = !clean
	}
	return f
}

func (s *Service) withFreshness(ctx context.Context, st domain.UniverseState, list []Checkout, opts ListOptions) []Checkout {
	if !opts.Freshness || s.Manifest == nil {
		return list
	}
	for i, c := range list {
		if !s.Files.Exists(c.Path) {
			continue
		}
		clone := s.Manifest.CloneDir(s.Multiverse, c.Repo)
		f := s.freshnessOf(ctx, c.Path, s.followed(ctx, c.Repo, clone, c.Branch))
		list[i].Freshness = &f
	}
	return list
}

// SyncResult is what happened to one repository of a universe.
type SyncResult struct {
	Repo   string
	Moved  int    // commits brought in
	Detail string // why nothing moved, or how
}

// SyncOptions say what to do with a checkout that has commits of its own.
type SyncOptions struct {
	// Merge brings the base in with a merge commit when the checkout has
	// commits the base lacks; without it such a checkout is left alone.
	Merge bool
}

// fetchClones fetches several clones at once, one line each, so a multiverse with
// many repositories costs the slowest fetch and not the sum. A failure is
// reported and the work goes on with what is already here. The dirs fetched
// are marked in done.
func (s *Service) fetchClones(ctx context.Context, label string, keys []string, done map[string]bool) {
	var progress Progress = noProgress{}
	if s.Progress != nil {
		progress = s.Progress
	}
	var jobs []Job
	for _, key := range keys {
		dir := s.Manifest.CloneDir(s.Multiverse, key)
		if done[dir] || !s.Files.Exists(dir) {
			continue
		}
		done[dir] = true
		jobs = append(jobs, Job{Name: key, Run: func(ctx context.Context) error { return s.Git.Fetch(ctx, dir) }})
	}
	if len(jobs) == 0 {
		return
	}
	errs := progress.Parallel(label, cloneLimit, jobs)
	for i, j := range jobs {
		if errs[i] != nil {
			s.Out.Warn("%s: fetch: %v (continuing with what is already here)", j.Name, errs[i])
		}
	}
}

// Sync brings a universe's checkouts up to date with the branches they follow,
// so a universe made yesterday runs today's code. It fetches, then moves each
// worktree forward when that loses nothing, and says why it left any alone:
// uncommitted changes are never touched, and commits of the universe's own are
// merged over only when asked.
func (s *Service) Sync(ctx context.Context, name string, opts SyncOptions) ([]SyncResult, error) {
	st, err := s.Resolve(name)
	if err != nil {
		return nil, err
	}
	unlock, err := s.lockUniverse(ctx, st.ID, st.Name)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if st, err = s.Resolve(name); err != nil {
		return nil, err
	}
	if s.Manifest == nil {
		return nil, domain.ErrNoManifest
	}
	u := s.Universe(st.Name)
	fetched := map[string]bool{}
	var out []SyncResult
	repos := make([]string, 0, len(st.Branches))
	for repo := range st.Branches {
		repos = append(repos, repo)
	}
	sort.Strings(repos)
	moved := 0
	s.fetchClones(ctx, "Fetching "+plural(len(repos), "repository"), repos, fetched)
	for _, repo := range repos {
		dir := u.RepoDir(repo)
		clone := s.Manifest.CloneDir(s.Multiverse, repo)
		res := SyncResult{Repo: repo}
		if !s.Files.Exists(dir) || !s.Files.Exists(clone) {
			res.Detail = "not checked out"
			out = append(out, res)
			continue
		}
		s.refresh(ctx, clone, fetched)
		follows := s.followed(ctx, repo, clone, st.Branches[repo])
		f := s.freshnessOf(ctx, dir, follows)
		switch {
		case follows == "":
			res.Detail = "no branch of origin to follow"
		case f.Behind == 0:
			res.Detail = "up to date with origin/" + follows
		case f.Dirty:
			res.Detail = fmt.Sprintf("%d behind origin/%s, but it has uncommitted changes; commit or stash them first", f.Behind, follows)
		case f.Ahead == 0:
			if err := s.Git.FastForward(ctx, dir, follows); err != nil {
				res.Detail = fmt.Sprintf("%d behind origin/%s and could not move: %v", f.Behind, follows, err)
				break
			}
			res.Moved, res.Detail = f.Behind, "moved forward to origin/"+follows
		case opts.Merge:
			if err := s.Git.Merge(ctx, dir, follows); err != nil {
				res.Detail = fmt.Sprintf("merging origin/%s failed (%v); left as it was, merge it by hand in %s", follows, err, dir)
				break
			}
			res.Moved, res.Detail = f.Behind, fmt.Sprintf("merged origin/%s (%d commits of its own kept)", follows, f.Ahead)
		default:
			res.Detail = fmt.Sprintf("%d behind origin/%s, with %d commits of its own: --merge brings origin/%s in", f.Behind, follows, f.Ahead, follows)
		}
		moved += res.Moved
		out = append(out, res)
	}
	if moved > 0 {
		// The labels in the compose files may have changed with the code, and
		// hostnames are kept with the universe: take them again.
		if rules, err := s.resolveRules(ctx, u, st.Domain); err != nil {
			s.Out.Warn("addresses: %v", err)
		} else {
			st.Rules, st.Hosts = rules, hostnames(rules)
			if err := s.Store.Save(st); err != nil {
				return out, err
			}
		}
	}
	return out, nil
}

// CloneResult is what happened to one shared clone.
type CloneResult struct {
	Repo   string
	Moved  int
	Detail string
}

// UpdateClones fast-forwards the shared clones of the multiverse to origin. What
// a universe branches from is origin's branch, so this does not change new
// universes; it keeps what is read from the clone itself (the files secrets
// are made from, a repository's own checkout) current. A clone with changes,
// or one on a branch of its own, is left alone and reported.
func (s *Service) UpdateClones(ctx context.Context) ([]CloneResult, error) {
	if s.Manifest == nil {
		return nil, domain.ErrNoManifest
	}
	var out []CloneResult
	s.fetchClones(ctx, "Fetching "+plural(len(s.repoKeys()), "repository"), s.repoKeys(), map[string]bool{})
	for _, key := range s.repoKeys() {
		dir := s.Manifest.CloneDir(s.Multiverse, key)
		if !s.Files.Exists(dir) {
			continue
		}
		res := CloneResult{Repo: key}
		def, err := s.Git.DefaultBranch(ctx, dir)
		if err != nil {
			continue
		}
		cur, _ := s.Git.CurrentBranch(ctx, dir)
		f := s.freshnessOf(ctx, dir, def)
		switch {
		case f.Behind == 0:
		case cur != def:
			res.Detail = fmt.Sprintf("%d behind origin/%s, but the clone is on %q", f.Behind, def, cur)
		case f.Dirty:
			res.Detail = fmt.Sprintf("%d behind origin/%s, but the clone has uncommitted changes", f.Behind, def)
		case f.Ahead > 0:
			res.Detail = fmt.Sprintf("%d behind origin/%s and %d ahead of it; update it by hand", f.Behind, def, f.Ahead)
		default:
			if err := s.Git.FastForward(ctx, dir, def); err != nil {
				res.Detail = fmt.Sprintf("could not move forward: %v", err)
			} else {
				res.Moved, res.Detail = f.Behind, fmt.Sprintf("moved forward %d", f.Behind)
			}
		}
		if res.Detail != "" {
			out = append(out, res)
		}
	}
	return out, nil
}

// CloneFreshness measures every clone against origin's default branch, as of
// the last fetch, for a view that shows which ones are behind.
func (s *Service) CloneFreshness(ctx context.Context) map[string]Freshness {
	out := map[string]Freshness{}
	if s.Manifest == nil {
		return out
	}
	for _, key := range s.repoKeys() {
		dir := s.Manifest.CloneDir(s.Multiverse, key)
		if !s.Files.Exists(dir) {
			continue
		}
		if def, err := s.Git.DefaultBranch(ctx, dir); err == nil {
			out[key] = s.freshnessOf(ctx, dir, def)
		}
	}
	return out
}

// behindHint is what to add to an error that may come from old code: a
// universe's checkouts keep the commit they were made from, so a fix that
// reached main since is not in them until they are brought forward.
func (s *Service) behindHint(ctx context.Context, st domain.UniverseState) string {
	if s.Manifest == nil {
		return ""
	}
	u := s.Universe(st.Name)
	var behind []string
	for _, repo := range mapKeys(st.Branches) {
		dir := u.RepoDir(repo)
		if !s.Files.Exists(dir) {
			continue
		}
		clone := s.Manifest.CloneDir(s.Multiverse, repo)
		if f := s.freshnessOf(ctx, dir, s.followed(ctx, repo, clone, st.Branches[repo])); f.Behind > 0 {
			behind = append(behind, fmt.Sprintf("%s (%d commits behind origin/%s)", repo, f.Behind, f.Follows))
		}
	}
	if len(behind) == 0 {
		return ""
	}
	return fmt.Sprintf("\n  this universe's code is out of date: %s, as of the last fetch.\n  bring it forward and try again: multiversectl universe sync %s", strings.Join(behind, ", "), st.Name)
}
