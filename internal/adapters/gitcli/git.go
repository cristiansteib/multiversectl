package gitcli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cristiansteib/multiversectl/internal/app"
)

type Git struct{}

func New() *Git { return &Git{} }

func (Git) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return "", fmt.Errorf("git %s: %w", args[0], err)
		}
		return "", fmt.Errorf("git %s: %s", args[0], msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func (g Git) Clone(ctx context.Context, url, dest string) error {
	_, err := g.run(ctx, "clone", url, dest)
	return err
}

func (g Git) Fetch(ctx context.Context, repoDir string) error {
	_, err := g.run(ctx, "-C", repoDir, "fetch", "--quiet", "origin")
	return err
}

func (g Git) AddWorktree(ctx context.Context, repoDir, dest, branch, base string) error {
	args := []string{"-C", repoDir, "worktree", "add"}
	switch {
	case base == "":
		args = append(args, dest, branch)
	case strings.HasPrefix(base, "origin/"):
		args = append(args, "--track", "-b", branch, dest, base)
	default:
		args = append(args, "-b", branch, dest, base)
	}
	_, err := g.run(ctx, args...)
	return err
}

func (g Git) AddWorktreeDetached(ctx context.Context, repoDir, dest, ref string) error {
	_, err := g.run(ctx, "-C", repoDir, "worktree", "add", "--detach", dest, ref)
	return err
}

func (g Git) Branches(ctx context.Context, repoDir string) ([]string, error) {
	out, err := g.run(ctx, "-C", repoDir, "for-each-ref", "--format=%(refname)", "refs/heads", "refs/remotes/origin")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var names []string
	for _, ref := range strings.Fields(out) {
		name := strings.TrimPrefix(strings.TrimPrefix(ref, "refs/heads/"), "refs/remotes/origin/")
		if name == "HEAD" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// CheckedOutAt reads `git worktree list --porcelain`, whose blocks are a
// worktree line, a HEAD line and a branch line.
func (g Git) CheckedOutAt(ctx context.Context, repoDir, branch string) (string, error) {
	out, err := g.run(ctx, "-C", repoDir, "worktree", "list", "--porcelain")
	if err != nil {
		return "", err
	}
	path := ""
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			path = strings.TrimPrefix(line, "worktree ")
		case line == "branch refs/heads/"+branch:
			return path, nil
		}
	}
	return "", nil
}

// ancestor is `git merge-base --is-ancestor a b`: exit 1 is the answer "no".
func (g Git) ancestor(ctx context.Context, repoDir, a, b string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoDir, "merge-base", "--is-ancestor", a, b)
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 && ctx.Err() == nil {
		return false, nil
	}
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	return false, fmt.Errorf("git merge-base: %w", err)
}

func (g Git) DeleteBranchIfSafe(ctx context.Context, repoDir, branch, base string) (bool, string, error) {
	local, err := g.refPresent(ctx, repoDir, "refs/heads/"+branch)
	if err != nil || !local {
		return false, "", err
	}
	if at, err := g.CheckedOutAt(ctx, repoDir, branch); err != nil {
		return false, "", err
	} else if at != "" {
		return false, "it is checked out at " + at, nil
	}
	// Merged into the base branch, or already on the branch's own remote copy:
	// either way the commits are somewhere else, and the name is all that goes.
	for _, into := range []string{"refs/remotes/origin/" + base, "refs/remotes/origin/" + branch} {
		if ok, err := g.refPresent(ctx, repoDir, into); err != nil {
			return false, "", err
		} else if !ok {
			continue
		}
		if safe, err := g.ancestor(ctx, repoDir, "refs/heads/"+branch, into); err != nil {
			return false, "", err
		} else if safe {
			if _, err := g.run(ctx, "-C", repoDir, "branch", "-D", branch); err != nil {
				return false, "", err
			}
			return true, "", nil
		}
	}
	n, err := g.run(ctx, "-C", repoDir, "rev-list", "--count", "refs/heads/"+branch, "--not", "--remotes")
	if err != nil {
		return false, "", err
	}
	return false, n + " commits that are on no remote", nil
}

func (g Git) Unpushed(ctx context.Context, repoDir string) (int, error) {
	out, err := g.run(ctx, "-C", repoDir, "rev-list", "--count", "--branches", "--not", "--remotes")
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("git rev-list: %w", err)
	}
	return n, nil
}

func (g Git) RemoveWorktree(ctx context.Context, repoDir, dest string, discard bool) error {
	args := []string{"-C", repoDir, "worktree", "remove"}
	if discard {
		// Without it git refuses a worktree holding work that is nowhere
		// else, which is the refusal the caller asked to go past.
		args = append(args, "--force")
	}
	_, err := g.run(ctx, append(args, dest)...)
	return err
}

func (g Git) CurrentBranch(ctx context.Context, dir string) (string, error) {
	return g.run(ctx, "-C", dir, "rev-parse", "--abbrev-ref", "HEAD")
}

// show-ref exits 1 when the ref is missing; that is an answer. Anything else
// (128 for a malformed name or a repository it cannot read) is a failure, and
// must not be reported as "the ref is not there": DeleteBranchIfSafe returns
// early on a missing branch, so an error read as absence skips its guards.
func (g Git) refPresent(ctx context.Context, repoDir, full string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoDir, "show-ref", "--verify", "--quiet", full)
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 && ctx.Err() == nil {
		return false, nil
	}
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	return false, fmt.Errorf("git show-ref: %w", err)
}

func (g Git) RefExists(ctx context.Context, repoDir, ref string) (app.RefKind, error) {
	if ok, err := g.refPresent(ctx, repoDir, "refs/heads/"+ref); err != nil {
		return app.RefNone, err
	} else if ok {
		return app.RefLocal, nil
	}
	if ok, err := g.refPresent(ctx, repoDir, "refs/remotes/origin/"+ref); err != nil {
		return app.RefNone, err
	} else if ok {
		return app.RefRemote, nil
	}
	return app.RefNone, nil
}

func (g Git) DefaultBranch(ctx context.Context, repoDir string) (string, error) {
	if out, err := g.run(ctx, "-C", repoDir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil && out != "" {
		return strings.TrimPrefix(out, "origin/"), nil
	}
	for _, name := range []string{"main", "master"} {
		ok, err := g.refPresent(ctx, repoDir, "refs/remotes/origin/"+name)
		if err != nil {
			return "", err
		}
		if ok {
			return name, nil
		}
	}
	return "", errors.New("cannot determine the default branch of origin")
}

// Checkout prefers origin/<ref> so a branch name lands on what was last
// fetched and not on a stale local branch from the first clone.
func (g Git) Checkout(ctx context.Context, repoDir, ref string) error {
	target := ref
	if _, err := g.run(ctx, "-C", repoDir, "rev-parse", "--verify", "--quiet", "origin/"+ref); err == nil {
		target = "origin/" + ref
	}
	_, err := g.run(ctx, "-C", repoDir, "checkout", "--quiet", "--detach", target)
	return err
}

func (g Git) Init(ctx context.Context, dir string) error {
	_, err := g.run(ctx, "init", "--quiet", "--initial-branch=main", dir)
	return err
}

func (g Git) Toplevel(ctx context.Context, dir string) (string, error) {
	return g.run(ctx, "-C", dir, "rev-parse", "--show-toplevel")
}

func (g Git) IsClean(ctx context.Context, dir string) (bool, error) {
	out, err := g.run(ctx, "-C", dir, "status", "--porcelain")
	return out == "", err
}

func (g Git) HasRemote(ctx context.Context, dir string) bool {
	out, err := g.run(ctx, "-C", dir, "remote")
	return err == nil && out != ""
}

func (g Git) Commit(ctx context.Context, dir, message string, paths ...string) error {
	if _, err := g.run(ctx, append([]string{"-C", dir, "add", "--"}, paths...)...); err != nil {
		return err
	}
	_, err := g.run(ctx, append([]string{"-C", dir, "commit", "--quiet", "-m", message, "--"}, paths...)...)
	return err
}

func (g Git) AddRemote(ctx context.Context, dir, name, url string) error {
	_, err := g.run(ctx, "-C", dir, "remote", "add", name, url)
	return err
}

func (g Git) Push(ctx context.Context, dir, branch string) error {
	_, err := g.run(ctx, "-C", dir, "push", "--quiet", "origin", "HEAD:refs/heads/"+branch)
	return err
}

func (g Git) RemoteURL(ctx context.Context, dir string) (string, error) {
	return g.run(ctx, "-C", dir, "remote", "get-url", "origin")
}

func (g Git) Behind(ctx context.Context, dir, ref string) (int, error) {
	branch := ref
	if branch == "" {
		var err error
		if branch, err = g.DefaultBranch(ctx, dir); err != nil {
			return 0, err
		}
	}
	out, err := g.run(ctx, "-C", dir, "rev-list", "--count", "HEAD..origin/"+branch)
	if err != nil {
		return 0, err
	}
	var n int
	if _, err := fmt.Sscanf(out, "%d", &n); err != nil {
		return 0, fmt.Errorf("git rev-list: unexpected %q", out)
	}
	return n, nil
}

func (g Git) Ahead(ctx context.Context, dir, ref string) (int, error) {
	branch := ref
	if branch == "" {
		var err error
		if branch, err = g.DefaultBranch(ctx, dir); err != nil {
			return 0, err
		}
	}
	out, err := g.run(ctx, "-C", dir, "rev-list", "--count", "origin/"+branch+"..HEAD")
	if err != nil {
		return 0, err
	}
	var n int
	if _, err := fmt.Sscanf(out, "%d", &n); err != nil {
		return 0, fmt.Errorf("git rev-list: unexpected %q", out)
	}
	return n, nil
}

// LastFetch reads the modification time of FETCH_HEAD, which git rewrites on
// every fetch, so no file of our own is needed to know when one last happened.
func (g Git) LastFetch(ctx context.Context, dir string) time.Time {
	gitDir, err := g.run(ctx, "-C", dir, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return time.Time{}
	}
	info, err := os.Stat(filepath.Join(gitDir, "FETCH_HEAD"))
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

func (g Git) FastForward(ctx context.Context, dir, branch string) error {
	_, err := g.run(ctx, "-C", dir, "merge", "--ff-only", "--quiet", "origin/"+branch)
	return err
}

func (g Git) Merge(ctx context.Context, dir, branch string) error {
	args := []string{"-C", dir}
	// A merge commit needs an author. A machine that has set none still gets
	// its merge, signed as the tool, instead of a failure that reads like a conflict.
	if out, _ := g.run(ctx, "-C", dir, "config", "user.email"); out == "" {
		args = append(args, "-c", "user.name=multiversectl", "-c", "user.email=multiversectl@localhost")
	}
	if _, err := g.run(ctx, append(args, "merge", "--no-edit", "--quiet", "origin/"+branch)...); err != nil {
		_, _ = g.run(ctx, "-C", dir, "merge", "--abort")
		return err
	}
	return nil
}

// DiffStat counts what changed, from --numstat rather than --shortstat: the
// per-file lines are what a binary file is told apart by, and summing them
// here costs nothing.
func (g Git) DiffStat(ctx context.Context, dir, ref string) (app.DiffStat, error) {
	args := []string{"-C", dir, "diff", "--numstat"}
	if ref == "" {
		// Everything not committed, staged or not, against HEAD.
		args = append(args, "HEAD")
	} else {
		// Three dots: the work done on this branch since it left the other,
		// not the other's own commits, which Behind already counts.
		args = append(args, "origin/"+ref+"...HEAD")
	}
	out, err := g.run(ctx, args...)
	if err != nil {
		return app.DiffStat{}, err
	}
	return parseNumstat(out), nil
}

// parseNumstat reads "<added>\t<removed>\t<path>" lines. A binary file has a
// dash for both counts and still changed, so it is a file with no lines.
func parseNumstat(out string) app.DiffStat {
	var d app.DiffStat
	for _, line := range strings.Split(out, "\n") {
		fields := strings.SplitN(strings.TrimSpace(line), "\t", 3)
		if len(fields) < 3 {
			continue
		}
		d.Files++
		if n, err := strconv.Atoi(fields[0]); err == nil {
			d.Added += n
		}
		if n, err := strconv.Atoi(fields[1]); err == nil {
			d.Removed += n
		}
	}
	return d
}
