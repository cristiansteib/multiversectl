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

	"github.com/cristiansteib/multiversectl/internal/adapters/trace"
	"github.com/cristiansteib/multiversectl/internal/app"
	"github.com/cristiansteib/multiversectl/internal/domain"
)

// Git runs the git program. A nil trace says nothing of what it runs.
type Git struct{ trace *trace.Tracer }

func New(t *trace.Tracer) *Git { return &Git{trace: t} }

func (g Git) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	done := g.trace.Begin(cmd)
	err := cmd.Run()
	done(err)
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return "", fmt.Errorf("git %s: %w", named(args), err)
		}
		return "", fmt.Errorf("git %s: %s", named(args), msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// named is the subcommand, for the message. Most calls start with "-C <dir>",
// and naming the error after that said only "git -C:".
func named(args []string) string {
	for i := 0; i < len(args); i++ {
		if args[i] == "-C" {
			i++ // and the directory after it
			continue
		}
		if strings.HasPrefix(args[i], "-") {
			continue
		}
		return args[i]
	}
	return "git"
}

func (g Git) Clone(ctx context.Context, url, dest string) error {
	_, err := g.run(ctx, "clone", url, dest)
	return err
}

func (g Git) Fetch(ctx context.Context, repoDir string) error {
	_, err := g.run(ctx, "-C", repoDir, "fetch", "--quiet", "origin")
	return err
}

// FetchQuietly is Fetch for when nobody is there to answer: a remote that wants
// a password fails instead of asking on a terminal, and ssh does not stop to ask
// either. A page reading again on its own is the case.
func (g Git) FetchQuietly(ctx context.Context, repoDir string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", repoDir, "fetch", "--quiet", "origin")
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
	done := g.trace.Begin(cmd)
	out, err := cmd.CombinedOutput()
	done(err)
	if err != nil {
		return fmt.Errorf("git fetch: %s", strings.TrimSpace(string(out)))
	}
	return nil
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
	done := g.trace.Begin(cmd)
	err := cmd.Run()
	done(err)
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

// PruneWorktrees makes git forget the worktrees whose directories are gone.
// Without it a directory removed any other way stays in the clone's
// bookkeeping, and the branch that was in it stays unavailable.
func (g Git) PruneWorktrees(ctx context.Context, repoDir string) error {
	_, err := g.run(ctx, "-C", repoDir, "worktree", "prune")
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
	done := g.trace.Begin(cmd)
	err := cmd.Run()
	done(err)
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

func (g Git) IsTracked(ctx context.Context, dir, path string) (bool, error) {
	// Literal, so a file name with a glob character is that file and no other.
	out, err := g.run(ctx, "--literal-pathspecs", "-C", dir, "ls-files", "-z", "--", path)
	return out != "", err
}

func (g Git) ExcludeFile(ctx context.Context, dir string) (string, error) {
	out, err := g.run(ctx, "-C", dir, "rev-parse", "--git-path", "info/exclude")
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(out) {
		out = filepath.Join(dir, out)
	}
	return out, nil
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
	d := parseNumstat(out)
	if ref == "" {
		// A file nobody has added yet is edited work as much as any other, and
		// `git diff` does not mention it.
		for _, f := range g.untracked(ctx, dir) {
			d.Files++
			d.Added += f.Added
		}
	}
	return d, nil
}

// maxUntracked bounds how much of a new file is read to count its lines.
const maxUntracked = 1 << 20

// untracked is the files git does not know of and has not been told to ignore,
// with their lines counted, as a diff would count them were they added.
func (g Git) untracked(ctx context.Context, dir string) []app.FileChange {
	out, err := g.run(ctx, "-C", dir, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil
	}
	var files []app.FileChange
	for _, p := range strings.Split(out, "\x00") {
		if p == "" {
			continue
		}
		f := app.FileChange{Path: p, Status: "untracked"}
		if text, ok := readNew(filepath.Join(dir, p)); ok {
			f.Added = lineCount(text)
		} else {
			f.Binary = true
		}
		files = append(files, f)
	}
	return files
}

// readNew reads a new file for counting and showing, and says no for what is
// not a plain text file, or is too large to be worth reading. A link is not
// followed: an untracked link in a checkout would otherwise show whatever it
// points at, wherever that is, and a pipe would block the read for ever.
func readNew(path string) (string, bool) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxUntracked {
		return "", false
	}
	b, err := os.ReadFile(path)
	if err != nil || bytes.IndexByte(b, 0) >= 0 {
		return "", false
	}
	return string(b), true
}

func lineCount(text string) int {
	if text == "" {
		return 0
	}
	n := strings.Count(text, "\n")
	if !strings.HasSuffix(text, "\n") {
		n++
	}
	return n
}

// diffRange is what DiffStat measures: the work on HEAD since it left origin's
// branch, or with no branch everything edited since HEAD.
func diffRange(ref string) string {
	if ref == "" {
		return "HEAD"
	}
	return "origin/" + ref + "...HEAD"
}

var statusNames = map[byte]string{'A': "added", 'M': "modified", 'D': "deleted", 'R': "renamed", 'C': "added", 'T': "modified"}

// DiffFiles lists the files DiffStat counts. The status comes from --name-status
// and the lines from --numstat, which are read apart because neither says both.
func (g Git) DiffFiles(ctx context.Context, dir, ref string) ([]app.FileChange, error) {
	rng := diffRange(ref)
	names, err := g.run(ctx, "-C", dir, "diff", "--name-status", "-z", "-M", rng)
	if err != nil {
		return nil, err
	}
	nums, err := g.run(ctx, "-C", dir, "diff", "--numstat", "-z", "-M", rng)
	if err != nil {
		return nil, err
	}
	type count struct {
		added, removed int
		binary         bool
	}
	counts := map[string]count{}
	toks := strings.Split(nums, "\x00")
	for i := 0; i < len(toks); i++ {
		f := strings.SplitN(toks[i], "\t", 3)
		if len(f) < 3 {
			continue
		}
		path := f[2]
		if path == "" && i+2 < len(toks) { // a rename names both paths after the counts
			path = toks[i+2]
			i += 2
		}
		a, errA := strconv.Atoi(f[0])
		r, errR := strconv.Atoi(f[1])
		counts[path] = count{added: a, removed: r, binary: errA != nil || errR != nil}
	}
	var files []app.FileChange
	toks = strings.Split(names, "\x00")
	for i := 0; i < len(toks); i++ {
		code := toks[i]
		if code == "" || i+1 >= len(toks) {
			continue
		}
		fc := app.FileChange{Status: statusNames[code[0]]}
		fc.Path = toks[i+1]
		i++
		if code[0] == 'R' || code[0] == 'C' {
			fc.OldPath, fc.Path = toks[i], toks[i+1]
			i++
			if code[0] == 'C' {
				fc.OldPath = ""
			}
		}
		c := counts[fc.Path]
		fc.Added, fc.Removed, fc.Binary = c.added, c.removed, c.binary
		files = append(files, fc)
	}
	if ref == "" {
		files = append(files, g.untracked(ctx, dir)...)
	}
	sort.SliceStable(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

// DiffPatch is one file's unified diff. A file git does not know yet has none
// to ask for, so it is written as what it is: all of it added.
func (g Git) DiffPatch(ctx context.Context, dir, ref, path string, limit int) (string, bool, error) {
	files, err := g.DiffFiles(ctx, dir, ref)
	if err != nil {
		return "", false, err
	}
	var file *app.FileChange
	for i := range files {
		if files[i].Path == path {
			file = &files[i]
		}
	}
	if file == nil {
		return "", false, fmt.Errorf("%s is not part of the change: %w", path, domain.ErrNotFound)
	}
	var patch string
	switch {
	case file.Status == "untracked":
		text, ok := readNew(filepath.Join(dir, path))
		if !ok {
			patch = "Binary or very large file: not shown.\n"
			break
		}
		var b strings.Builder
		fmt.Fprintf(&b, "--- /dev/null\n+++ b/%s\n@@ -0,0 +1,%d @@\n", path, lineCount(text))
		for _, l := range strings.SplitAfter(text, "\n") {
			if l == "" {
				continue
			}
			b.WriteString("+" + l)
			if !strings.HasSuffix(l, "\n") {
				b.WriteString("\n\\ No newline at end of file\n")
			}
		}
		patch = b.String()
	default:
		paths := []string{path}
		if file.OldPath != "" {
			paths = append(paths, file.OldPath)
		}
		args := append([]string{"-C", dir, "diff", "--no-color", "-M", diffRange(ref), "--"}, paths...)
		if patch, err = g.run(ctx, args...); err != nil {
			return "", false, err
		}
	}
	if limit > 0 && len(patch) > limit {
		// Whole lines where there are any, never more than the limit.
		if cut := strings.LastIndex(patch[:limit], "\n"); cut >= 0 {
			return patch[:cut+1], true, nil
		}
		return patch[:limit], true, nil
	}
	return patch, false, nil
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

// LsRemote asks the remote for its refs, never prompting: a remote that wants a
// password or an unknown ssh host fails with git's own words, which is what a
// preflight classifies.
func (g Git) LsRemote(ctx context.Context, url string) error {
	cmd := exec.CommandContext(ctx, "git", "ls-remote", "--quiet", "--", url, "HEAD")
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if os.Getenv("GIT_SSH_COMMAND") == "" {
		cmd.Env = append(cmd.Env, "GIT_SSH_COMMAND=ssh -o BatchMode=yes -o ConnectTimeout=10")
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	done := g.trace.Begin(cmd)
	err := cmd.Run()
	done(err)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("git ls-remote: %w", ctx.Err())
		}
		return fmt.Errorf("git ls-remote: %s", strings.TrimSpace(stderr.String()))
	}
	return nil
}
