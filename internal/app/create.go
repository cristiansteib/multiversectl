package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// CreateInput asks for a universe, in one of two ways that never mix.
//
// A new universe starts branches of its own: in every repository a branch that
// exists nowhere yet, from the base branch, so it can never land on someone
// else's work. Name is a number or a branch-like name, and empty means the next
// free number; Branch names the new branch in every repository and Branches
// overrides it per repository key. Base is where the new branches start, the
// repository's default branch when empty, and Bases overrides it per key.
//
// A checkout universe (Checkout) works on branches that already exist: Branch
// is the one to check out, Branches overrides it per key, and Name defaults to
// the branch. A repository that lacks the branch gets a new one of that name
// from its base, or with OnlyExisting is left at its base, detached.
type CreateInput struct {
	Name     string
	Branch   string
	Branches map[string]string
	Base     string
	Bases    map[string]string
	Note     string

	Checkout     bool
	OnlyExisting bool
}

// How one repository's worktree comes about.
type how int

const (
	keep     how = iota // the universe already has this checkout
	fresh               // a new branch from the base
	existing            // an existing branch, as it is
	detached            // the base itself, with no branch
)

// rollbackGrace is how long taking back a half-made universe may take. It is
// generous: the work is a handful of local git commands, and the only reason
// to stop is a git that has wedged.
const rollbackGrace = 30 * time.Second

type repoPlan struct {
	key, clone, dest string
	branch, base     string
	how              how
	// kind is what the branch is now: only meaningful for checkouts.
	kind RefKind
	// baseKind is where the base was found, which decides the ref a new
	// branch starts from.
	baseKind RefKind
}

// created says the plan starts a branch of its own.
func (p repoPlan) created() bool { return p.how == fresh }

func (s *Service) repoKeys() []string {
	keys := make([]string, 0, len(s.Manifest.Repos))
	for key := range s.Manifest.Repos {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// refresh brings a clone up to date with origin, once, so a branch someone
// pushed an hour ago counts as existing and the base is not stale.
func (s *Service) refresh(ctx context.Context, clone string, done map[string]bool) {
	if done[clone] {
		return
	}
	done[clone] = true
	if err := s.Git.Fetch(ctx, clone); err != nil {
		s.Out.Warn("fetch %s: %v (continuing with what is already here)", clone, err)
	}
}

// hasBranch says whether a clone knows a branch, its own or origin's copy.
// No name is no branch, and so not found.
func (s *Service) hasBranch(ctx context.Context, clone, branch string) (bool, error) {
	if branch == "" {
		return false, nil
	}
	kind, err := s.Git.RefExists(ctx, clone, branch)
	if err != nil {
		return false, err
	}
	return kind != RefNone, nil
}

// NextFree is the number a new universe would get: the smallest that no universe
// has and whose branch exists in no repository, with the numbers it passed over
// and why. A leftover branch from a universe that was removed is the usual reason.
func (s *Service) NextFree(ctx context.Context) (int, []string, error) {
	states, err := s.Store.List()
	if err != nil {
		return 0, nil, err
	}
	taken := map[int]bool{}
	for _, st := range states {
		taken[st.Number] = true
	}
	fetched := map[string]bool{}
	var clones []struct{ key, dir string }
	for _, key := range s.repoKeys() {
		if dir := s.Manifest.CloneDir(s.Workspace, key); s.Files.Exists(dir) {
			s.refresh(ctx, dir, fetched)
			clones = append(clones, struct{ key, dir string }{key, dir})
		}
	}
	var skipped []string
	for n := 1; n < 100000; n++ {
		if taken[n] || s.Files.Exists(s.Universe(strconv.Itoa(n)).Dir()) {
			continue
		}
		branch := s.Manifest.DefaultBranchFor(n)
		var where []string
		for _, c := range clones {
			if kind, err := s.Git.RefExists(ctx, c.dir, branch); err != nil {
				return 0, nil, err
			} else if kind != RefNone {
				where = append(where, c.key)
			}
		}
		if len(where) > 0 {
			skipped = append(skipped, fmt.Sprintf("%d (branch %s exists in %s)", n, branch, strings.Join(where, ", ")))
			continue
		}
		return n, skipped, nil
	}
	return 0, skipped, errors.New("no free universe number")
}

// BranchInfo is a branch name and the repositories that have it.
type BranchInfo struct {
	Name  string
	Repos []string
}

// Branches lists the branches across the project's clones, as last fetched,
// for choosing one to check out.
func (s *Service) Branches(ctx context.Context) ([]BranchInfo, error) {
	where := map[string][]string{}
	for _, key := range s.repoKeys() {
		dir := s.Manifest.CloneDir(s.Workspace, key)
		if !s.Files.Exists(dir) {
			continue
		}
		names, err := s.Git.Branches(ctx, dir)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		for _, n := range names {
			where[n] = append(where[n], key)
		}
	}
	out := make([]BranchInfo, 0, len(where))
	for n, repos := range where {
		out = append(out, BranchInfo{Name: n, Repos: repos})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// plan decides what each repository gets and finds every clash before anything
// is created, so a refusal leaves nothing behind and says everything at once.
func (s *Service) plan(ctx context.Context, u domain.Universe, in CreateInput) ([]repoPlan, error) {
	var plans []repoPlan
	var problems []string
	fetched := map[string]bool{}
	anyHas := false

	for _, key := range s.repoKeys() {
		clone := s.Manifest.CloneDir(s.Workspace, key)
		if !s.Files.Exists(clone) {
			return nil, fmt.Errorf("clone `%s`: %w — run `multiversectl project update` to clone it", clone, domain.ErrNotFound)
		}
		p := repoPlan{key: key, clone: clone, dest: u.RepoDir(key)}
		if s.Files.Exists(p.dest) {
			p.how = keep
			// The worktree is kept as it is, but it still answers the question
			// the guard below asks: a checkout whose worktrees are all here
			// already is a re-run, and asking none of them left it reporting
			// that the branch existed in no repository.
			if in.Checkout {
				has, err := s.hasBranch(ctx, clone, firstNonEmpty(in.Branches[key], in.Branch))
				if err != nil {
					return nil, err
				}
				anyHas = anyHas || has
			}
			plans = append(plans, p)
			continue
		}
		s.refresh(ctx, clone, fetched)

		base := firstNonEmpty(in.Bases[key], in.Base)
		if base == "" {
			def, err := s.Git.DefaultBranch(ctx, clone)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", key, err)
			}
			base = def
		}
		baseKind, err := s.Git.RefExists(ctx, clone, base)
		if err != nil {
			return nil, err
		}
		if baseKind == RefNone {
			problems = append(problems, fmt.Sprintf("%s: the base %q does not exist", key, base))
			continue
		}
		p.base, p.baseKind = base, baseKind

		if in.Checkout {
			p.branch = firstNonEmpty(in.Branches[key], in.Branch)
			if p.branch == "" {
				// Named for another repository only: this one stays on its base.
				p.how = detached
				plans = append(plans, p)
				continue
			}
			kind, err := s.Git.RefExists(ctx, clone, p.branch)
			if err != nil {
				return nil, err
			}
			p.kind = kind
			switch {
			case kind == RefNone && in.OnlyExisting:
				p.how = detached
			case kind == RefNone:
				p.how = fresh
			default:
				anyHas = true
				p.how = existing
				if at, err := s.Git.CheckedOutAt(ctx, clone, p.branch); err != nil {
					return nil, err
				} else if at != "" {
					problems = append(problems, fmt.Sprintf("%s: branch %q is already checked out at %s", key, p.branch, at))
				}
			}
		} else {
			p.how = fresh
			p.branch = firstNonEmpty(in.Branches[key], in.Branch, u.DefaultBranch())
			kind, err := s.Git.RefExists(ctx, clone, p.branch)
			if err != nil {
				return nil, err
			}
			if kind != RefNone {
				where := "in the clone"
				if kind == RefRemote {
					where = "on origin"
				}
				problems = append(problems, fmt.Sprintf("%s: branch %q already exists (%s)", key, p.branch, where))
			}
		}
		plans = append(plans, p)
	}

	if in.Checkout && !anyHas && len(problems) == 0 {
		problems = append(problems, fmt.Sprintf("the branch %q exists in no repository", firstNonEmpty(in.Branch)))
	}
	if len(problems) == 0 {
		return plans, nil
	}
	return nil, &ClashError{Checkout: in.Checkout, Problems: problems}
}

// ClashError is a universe that cannot be created without landing on work that
// is not its own, with every place that happens and what to do about it.
type ClashError struct {
	Checkout bool
	Problems []string
}

func (e *ClashError) Error() string {
	var b strings.Builder
	b.WriteString("nothing was created:")
	for _, p := range e.Problems {
		b.WriteString("\n  - " + p)
	}
	if e.Checkout {
		b.WriteString("\n  git allows a branch in one worktree only: remove the universe that has it, or check out another branch")
		b.WriteString("\n  to start a branch that does not exist yet, use `multiversectl universe new`")
	} else {
		b.WriteString("\n  a new universe never reuses a branch, so it cannot land on anyone's work:")
		b.WriteString("\n  - leave the name out and the next free number is used")
		b.WriteString("\n  - or work on the existing branch with `multiversectl universe checkout <branch>`")
		b.WriteString("\n  - or delete the leftover branch yourself if it is not needed: `git branch -d <branch>` in its clone")
	}
	return b.String()
}

// Create scaffolds a universe: one worktree per repository, its secrets and
// its state. It is safe to run again: existing checkouts are kept.
func (s *Service) Create(ctx context.Context, in CreateInput) (domain.UniverseState, error) {
	// Held before the number is picked: two processes asking for the next free
	// one at the same time would otherwise both be told the same number, and
	// the second would fail on branches the first had just made.
	unlock, err := s.lockProject(ctx)
	if err != nil {
		return domain.UniverseState{}, err
	}
	defer unlock()

	if in.Checkout && in.Branch == "" && len(in.Branches) == 0 {
		return domain.UniverseState{}, errors.New("a checkout universe needs the branch to check out")
	}
	if strings.TrimSpace(in.Name) == "" {
		if in.Checkout {
			if in.Branch == "" {
				return domain.UniverseState{}, errors.New("a checkout universe named only by per-repository branches needs a name of its own (--name)")
			}
			in.Name = in.Branch
		} else {
			n, skipped, err := s.NextFree(ctx)
			if err != nil {
				return domain.UniverseState{}, err
			}
			for _, sk := range skipped {
				s.Out.Info("skipped %s", sk)
			}
			in.Name = strconv.Itoa(n)
		}
	}
	if err := domain.ValidateUniverseName(in.Name); err != nil {
		return domain.UniverseState{}, err
	}
	u := s.Universe(strings.TrimSpace(in.Name))

	dom, err := s.ResolveDomain(ctx)
	if err != nil {
		return domain.UniverseState{}, err
	}
	plans, err := s.plan(ctx, u, in)
	if err != nil {
		return domain.UniverseState{}, err
	}

	dirExisted := s.Files.Exists(u.Dir())
	if err := s.Files.MkdirAll(u.Dir()); err != nil {
		return domain.UniverseState{}, err
	}
	branches := map[string]string{}
	var made []repoPlan // worktrees this call added, to take back if a later one fails
	var createdKeys []string
	for _, p := range plans {
		if p.how == keep {
			current, err := s.Git.CurrentBranch(ctx, p.dest)
			if err != nil {
				return domain.UniverseState{}, s.rollback(ctx, made, u, dirExisted, err)
			}
			branches[p.key] = current
			s.Out.OK("%s: checkout exists on %s", p.key, current)
			continue
		}
		if err := s.addWorktree(ctx, p); err != nil {
			return domain.UniverseState{}, s.rollback(ctx, made, u, dirExisted, fmt.Errorf("%s: %w", p.key, err))
		}
		made = append(made, p)
		switch p.how {
		case detached:
			branches[p.key] = "origin/" + p.base
			s.Out.OK("%s: has no %s; checked out origin/%s, detached", p.key, p.branch, p.base)
		case fresh:
			branches[p.key] = p.branch
			createdKeys = append(createdKeys, p.key)
			s.Out.OK("%s: new branch %s from origin/%s", p.key, p.branch, p.base)
		default:
			branches[p.key] = p.branch
			s.Out.OK("%s: worktree on %s", p.key, p.branch)
		}
	}

	if err := s.copySecrets(u); err != nil {
		return domain.UniverseState{}, s.rollback(ctx, made, u, dirExisted, err)
	}

	st := domain.UniverseState{
		Name:            u.Name,
		ID:              u.ID(),
		Slug:            u.Slug(),
		Number:          u.Number(),
		Domain:          dom,
		Project:         u.Project(),
		Branches:        branches,
		CreatedBranches: createdKeys,
		Created:         time.Now().UTC(),
		Note:            in.Note,
		Manifest:        s.Manifest.Path,
		Version:         s.Version,
	}
	// Running it again must not forget which branches the universe started:
	// removing it later depends on knowing.
	if old, err := s.Store.Load(u.ID()); err == nil {
		st.CreatedBranches = mergeKeys(old.CreatedBranches, createdKeys)
	}
	// The checkouts exist now, so their compose files can be read for the
	// labels that say which addresses this universe answers on.
	rules, err := s.resolveRules(ctx, u, dom)
	if err != nil {
		s.Out.Warn("addresses: %v (they will be resolved on the first start)", err)
	}
	st.Rules, st.Hosts = rules, hostnames(rules)
	if err := s.Store.Save(st); err != nil {
		return domain.UniverseState{}, s.rollback(ctx, made, u, dirExisted, err)
	}
	return st, nil
}

func mergeKeys(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, k := range append(append([]string{}, a...), b...) {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// rollback takes back the worktrees and branches a failed Create added, so a
// universe that could not be made does not leave half of one behind.
//
// It runs on a context of its own. The usual reason Create fails is that the
// caller pressed Ctrl-C, and a cleanup on that same cancelled context does
// nothing at all: every command it runs returns at once, silently, and the
// half-made universe stays on disk. Its own deadline keeps that from becoming
// a hang instead.
//
// What it cannot take back it says, in the error it returns: a worktree left
// behind is a directory somebody has to deal with, and a branch left behind
// takes its number out of the sequence. Silence there sends them looking for a
// universe the tool has already said it did not make.
func (s *Service) rollback(ctx context.Context, made []repoPlan, u domain.Universe, dirExisted bool, cause error) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackGrace)
	defer cancel()
	var left []string
	for i := len(made) - 1; i >= 0; i-- {
		p := made[i]
		// Discarding: these worktrees were made moments ago by this call, and
		// anything in them was put there by what failed.
		if err := s.Git.RemoveWorktree(ctx, p.clone, p.dest, true); err != nil {
			left = append(left, fmt.Sprintf("%s: worktree %s (%v)", p.key, p.dest, err))
			continue // the branch is still checked out in it, so it cannot go either
		}
		if p.created() {
			if _, _, err := s.Git.DeleteBranchIfSafe(ctx, p.clone, p.branch, p.base); err != nil {
				left = append(left, fmt.Sprintf("%s: branch %s (%v)", p.key, p.branch, err))
			}
		}
	}
	if !dirExisted {
		if err := s.Files.RemoveAll(u.Dir()); err != nil {
			left = append(left, fmt.Sprintf("the universe's folder %s (%v)", u.Dir(), err))
		}
	}
	if len(left) == 0 {
		return cause
	}
	return fmt.Errorf("%w\n  what was made could not all be taken back, and is still there:\n    %s",
		cause, strings.Join(left, "\n    "))
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// addWorktree makes one repository's checkout as planned. Starting a branch
// from the remote-tracking ref that the plan's fetch has just updated, not from
// the clone's own copy of the base, is deliberate: that copy is left wherever it
// last was and is routinely behind, which would start the universe on stale code
// without saying so.
func (s *Service) addWorktree(ctx context.Context, p repoPlan) error {
	switch p.how {
	case detached:
		return s.Git.AddWorktreeDetached(ctx, p.clone, p.dest, baseRef(p.base, p.baseKind))
	case existing:
		if p.kind == RefRemote {
			return s.Git.AddWorktree(ctx, p.clone, p.dest, p.branch, "origin/"+p.branch)
		}
		return s.Git.AddWorktree(ctx, p.clone, p.dest, p.branch, "")
	default:
		return s.Git.AddWorktree(ctx, p.clone, p.dest, p.branch, baseRef(p.base, p.baseKind))
	}
}

// baseRef is the ref a new branch starts from: origin's copy of the base when
// origin has it, and the clone's own branch when that is where the plan found
// it. Preferring origin's copy is deliberate — see addWorktree — but a base
// that exists in the clone alone has no copy there, and asking for one made
// git fail on a base the plan had just accepted. Only the two kinds RefExists
// resolves can get this far: a tag or a commit is refused as a base before it.
func baseRef(base string, kind RefKind) string {
	if kind == RefLocal {
		return base
	}
	return "origin/" + base
}

// copySecrets brings the files that are deliberately not in git — credentials
// and service accounts — into the universe's checkouts.
func (s *Service) copySecrets(u domain.Universe) error {
	for appKey := range s.Manifest.Apps {
		for _, pair := range s.SecretPaths(u, appKey) {
			if _, err := s.provideSecret(appKey, pair[0], pair[1]); err != nil {
				return err
			}
		}
	}
	return nil
}

// provideSecret puts one secret file in the universe (dst), taking it from the
// shared clone (src). When the clone has none, it does what the repositories
// themselves do on first run, `cp .env.example .env`: a tracked <name>.example
// beside it is copied to src, once, so the credentials are filled in one place
// and every universe copies them from there. found is false only when there is
// neither the file nor an example, which the caller reports.
func (s *Service) provideSecret(appKey, src, dst string) (found bool, err error) {
	if s.Files.Exists(dst) {
		return true, nil
	}
	if !s.Files.Exists(src) {
		example := src + ".example"
		if !s.Files.Exists(example) {
			return false, nil
		}
		if err := s.Files.CopyFile(example, src); err != nil {
			return false, err
		}
		s.Out.Warn("%s: there was no `%s`, so it was made from %s: put your own credentials in it, and every universe copies it from there", appKey, src, filepath.Base(example))
	}
	if err := s.Files.CopyFile(src, dst); err != nil {
		return false, err
	}
	s.Out.OK("%s: copied %s", appKey, filepath.Base(dst))
	return true, nil
}

// PrepareSecrets makes sure the secret files of the apps about to start are in
// the universe, so one put in the clone after the universe was created is
// picked up. A file that exists nowhere, and has no example to start from,
// stops the start and names each one and where it belongs. Without this the
// compose files are the first to notice (an env_file that does not exist makes
// them unreadable) and say nothing of where the file should come from.
func (s *Service) PrepareSecrets(u domain.Universe, apps []string) error {
	var missing []string
	for _, appKey := range apps {
		for _, pair := range s.SecretPaths(u, appKey) {
			found, err := s.provideSecret(appKey, pair[0], pair[1])
			if err != nil {
				return err
			}
			if !found {
				missing = append(missing, fmt.Sprintf("  %s needs %s\n      put your copy at `%s`", appKey, filepath.Base(pair[1]), pair[0]))
			}
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("a secret file is missing, and it is not in any repository because git ignores it:\n%s\nthen run this again: it copies the file into the universe", strings.Join(missing, "\n"))
	}
	return nil
}
