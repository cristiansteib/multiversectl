package gitcli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cristiansteib/multiversectl/internal/app"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=T", "-c", "user.email=t@example.test", "-c", "commit.gpgsign=false"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func commit(t *testing.T, dir, file string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(file), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", file)
	git(t, dir, "commit", "-qm", file)
}

// repo is a clone of a bare origin that has main with one commit.
func repo(t *testing.T) (clone, origin string) {
	t.Helper()
	root := t.TempDir()
	origin = filepath.Join(root, "origin.git")
	seed := filepath.Join(root, "seed")
	clone = filepath.Join(root, "clone")
	for _, args := range [][]string{{"init", "-q", "--bare", "-b", "main", origin}, {"init", "-q", "-b", "main", seed}} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
	}
	commit(t, seed, "a")
	git(t, seed, "remote", "add", "origin", origin)
	git(t, seed, "push", "-q", "origin", "main")
	if out, err := exec.Command("git", "clone", "-q", origin, clone).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	return clone, origin
}

func TestBranchesListsLocalAndRemoteOnce(t *testing.T) {
	clone, _ := repo(t)
	git(t, clone, "branch", "only-local")
	git(t, clone, "branch", "feat/x")
	git(t, clone, "push", "-q", "origin", "feat/x")
	got, err := Git{}.Branches(context.Background(), clone)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "feat/x,main,only-local" {
		t.Errorf("branches = %v: want each name once, origin/HEAD left out", got)
	}
}

func TestACheckedOutBranchIsFoundWhereItIs(t *testing.T) {
	clone, _ := repo(t)
	g := Git{}
	dest := filepath.Join(t.TempDir(), "wt")
	if err := g.AddWorktree(context.Background(), clone, dest, "u3", "origin/main"); err != nil {
		t.Fatal(err)
	}
	at, err := g.CheckedOutAt(context.Background(), clone, "u3")
	if err != nil || !samePath(at, dest) {
		t.Errorf("u3 is at %q (%v), want %q", at, err, dest)
	}
	if at, _ := g.CheckedOutAt(context.Background(), clone, "nowhere"); at != "" {
		t.Errorf("a branch that is not checked out is at %q", at)
	}
	// The branch of the shared clone itself counts too.
	if at, _ := g.CheckedOutAt(context.Background(), clone, "main"); !samePath(at, clone) {
		t.Errorf("main is at %q, want the clone %q", at, clone)
	}
}

func TestADetachedWorktreeTakesNoBranch(t *testing.T) {
	clone, _ := repo(t)
	dest := filepath.Join(t.TempDir(), "wt")
	if err := (Git{}).AddWorktreeDetached(context.Background(), clone, dest, "origin/main"); err != nil {
		t.Fatal(err)
	}
	if head := git(t, dest, "rev-parse", "--abbrev-ref", "HEAD"); head != "HEAD" {
		t.Errorf("HEAD = %q, want detached", head)
	}
	if at, _ := (Git{}).CheckedOutAt(context.Background(), clone, "main"); !samePath(at, clone) {
		t.Errorf("a detached worktree must not take main from the clone: %q", at)
	}
}

func TestABranchIsDeletedOnlyWhenNothingIsLost(t *testing.T) {
	ctx := context.Background()
	g := Git{}

	t.Run("work that is on no remote is kept", func(t *testing.T) {
		clone, _ := repo(t)
		git(t, clone, "branch", "u3")
		git(t, clone, "switch", "-q", "u3")
		commit(t, clone, "wip")
		git(t, clone, "switch", "-q", "main")
		deleted, why, err := g.DeleteBranchIfSafe(ctx, clone, "u3", "main")
		if err != nil || deleted || !strings.Contains(why, "1 commits that are on no remote") {
			t.Errorf("deleted %v, why %q, err %v: unpushed commits must keep the branch", deleted, why, err)
		}
		if kind, _ := g.RefExists(ctx, clone, "u3"); kind != app.RefLocal {
			t.Error("the branch is gone")
		}
	})

	t.Run("a branch with nothing of its own is deleted", func(t *testing.T) {
		clone, _ := repo(t)
		git(t, clone, "branch", "u4")
		if deleted, why, err := g.DeleteBranchIfSafe(ctx, clone, "u4", "main"); err != nil || !deleted {
			t.Errorf("deleted %v, why %q, err %v", deleted, why, err)
		}
	})

	t.Run("pushed work is deleted: the commits are on the remote", func(t *testing.T) {
		clone, _ := repo(t)
		git(t, clone, "switch", "-q", "-c", "u5")
		commit(t, clone, "feature")
		git(t, clone, "push", "-q", "origin", "u5")
		git(t, clone, "switch", "-q", "main")
		if deleted, why, err := g.DeleteBranchIfSafe(ctx, clone, "u5", "main"); err != nil || !deleted {
			t.Errorf("deleted %v, why %q, err %v", deleted, why, err)
		}
		if kind, _ := g.RefExists(ctx, clone, "u5"); kind != app.RefRemote {
			t.Errorf("the remote copy must remain: %v", kind)
		}
	})

	t.Run("a branch in use is kept", func(t *testing.T) {
		clone, _ := repo(t)
		dest := filepath.Join(t.TempDir(), "wt")
		if err := g.AddWorktree(ctx, clone, dest, "u6", "origin/main"); err != nil {
			t.Fatal(err)
		}
		deleted, why, err := g.DeleteBranchIfSafe(ctx, clone, "u6", "main")
		if err != nil || deleted || !strings.Contains(why, "checked out at") {
			t.Errorf("deleted %v, why %q, err %v", deleted, why, err)
		}
	})

	t.Run("a branch that does not exist is nothing to do", func(t *testing.T) {
		clone, _ := repo(t)
		if deleted, why, err := g.DeleteBranchIfSafe(ctx, clone, "ghost", "main"); err != nil || deleted || why != "" {
			t.Errorf("deleted %v, why %q, err %v", deleted, why, err)
		}
	})
}

// samePath compares real locations: git reports the resolved path, and on macOS
// the temp directory is reached through a symlink (/var -> /private/var).
func samePath(a, b string) bool {
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return ra == rb
}

func TestAheadAndBehindCountEachSidesCommits(t *testing.T) {
	clone, origin := repo(t)
	g := Git{}
	ctx := context.Background()

	commit(t, clone, "mine")
	other := filepath.Join(t.TempDir(), "other")
	if out, err := exec.Command("git", "clone", "-q", origin, other).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	commit(t, other, "theirs1")
	commit(t, other, "theirs2")
	git(t, other, "push", "-q", "origin", "main")
	if err := g.Fetch(ctx, clone); err != nil {
		t.Fatal(err)
	}

	ahead, errA := g.Ahead(ctx, clone, "")
	behind, errB := g.Behind(ctx, clone, "main")
	if errA != nil || errB != nil || ahead != 1 || behind != 2 {
		t.Errorf("ahead=%d (%v) behind=%d (%v), want 1 and 2: diverged", ahead, errA, behind, errB)
	}
}

func TestLastFetchIsWhenGitLastFetched(t *testing.T) {
	clone, _ := repo(t)
	g := Git{}
	ctx := context.Background()
	if err := os.Remove(filepath.Join(clone, ".git", "FETCH_HEAD")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if at := g.LastFetch(ctx, clone); !at.IsZero() {
		t.Errorf("never fetched, got %v", at)
	}
	if err := g.Fetch(ctx, clone); err != nil {
		t.Fatal(err)
	}
	if at := g.LastFetch(ctx, clone); time.Since(at) > time.Minute || at.IsZero() {
		t.Errorf("just fetched, got %v", at)
	}
}

// A binary file has no lines to count and still changed, which --shortstat
// would have hidden inside its own prose.
func TestABinaryFileCountsAsAChangedFileWithNoLines(t *testing.T) {
	d := parseNumstat("12\t3\tinternal/app/code.go\n-\t-\tdocs/logo.png\n0\t40\tREADME.md\n")
	if d.Files != 3 {
		t.Errorf("want three files, got %d", d.Files)
	}
	if d.Added != 12 || d.Removed != 43 {
		t.Errorf("want +12 -43, got +%d -%d", d.Added, d.Removed)
	}
}

func TestNothingChangedIsNoFilesAtAll(t *testing.T) {
	if d := parseNumstat("\n"); !(d.Files == 0 && d.Added == 0 && d.Removed == 0) {
		t.Errorf("want an empty stat, got %+v", d)
	}
}

// The files of a change are what DiffStat counts, so the two are read off the
// same repository and must agree: renames as one file, a new untracked file as
// added lines, and a patch only for what is in the list.
func TestDiffFilesListsTheBranchAndWhatIsNotCommitted(t *testing.T) {
	clone, _ := repo(t)
	ctx := context.Background()
	// Big enough that git calls a moved and edited file a rename, and already on
	// main, so that moving it is a change of the branch.
	body := strings.Repeat("a line of the file\n", 12)
	if err := os.WriteFile(filepath.Join(clone, "big"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, clone, "add", "big")
	git(t, clone, "commit", "-qm", "big")
	git(t, clone, "push", "-q", "origin", "main")
	git(t, clone, "switch", "-q", "-c", "fix/x")
	if err := os.WriteFile(filepath.Join(clone, "big"), []byte(body+"more\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, clone, "mv", "big", "renamed")
	git(t, clone, "commit", "-qam", "rename and edit")
	commit(t, clone, "b")
	git(t, clone, "push", "-q", "origin", "fix/x")

	files, err := Git{}.DiffFiles(ctx, clone, "main")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]app.FileChange{}
	for _, f := range files {
		got[f.Path] = f
	}
	if r := got["renamed"]; r.Status != "renamed" || r.OldPath != "big" {
		t.Errorf("want a rename, got %+v", r)
	}
	if b := got["b"]; b.Status != "added" || b.Added != 1 {
		t.Errorf("want b added with a line, got %+v", b)
	}
	stat, _ := Git{}.DiffStat(ctx, clone, "main")
	if stat.Files != len(files) {
		t.Errorf("the stat counts %d files and the list has %d", stat.Files, len(files))
	}

	// Nothing is uncommitted until a file is edited and another is created.
	if files, _ := (Git{}).DiffFiles(ctx, clone, ""); len(files) != 0 {
		t.Errorf("a clean checkout has no uncommitted files: %+v", files)
	}
	if err := os.WriteFile(filepath.Join(clone, "b"), []byte("b\nedited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(clone, "new.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, _ = Git{}.DiffFiles(ctx, clone, "")
	if len(files) != 2 || files[1].Path != "new.txt" || files[1].Status != "untracked" || files[1].Added != 2 {
		t.Fatalf("want the edit and the new file, got %+v", files)
	}
	if st, _ := (Git{}).DiffStat(ctx, clone, ""); st.Files != 2 || st.Added != 4 {
		t.Errorf("the untracked file is edited work too: %+v", st)
	}

	patch, truncated, err := Git{}.DiffPatch(ctx, clone, "", "new.txt", 1<<10)
	if err != nil || truncated || !strings.Contains(patch, "+one\n+two") {
		t.Errorf("want the new file written as added, got %q %v %v", patch, truncated, err)
	}
	patch, truncated, _ = Git{}.DiffPatch(ctx, clone, "main", "renamed", 10)
	if !truncated || len(patch) > 10 {
		t.Errorf("want the patch cut at the limit, got %q %v", patch, truncated)
	}
	if _, _, err := (Git{}).DiffPatch(ctx, clone, "main", "../etc/passwd", 100); err == nil {
		t.Error("a path that is not in the change was read")
	}
}

// An untracked link is not followed: its target may be anywhere on the machine,
// and what the page would show of it is not part of the repository.
func TestAnUntrackedLinkIsNotReadThroughTo(t *testing.T) {
	clone, _ := repo(t)
	secret := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(secret, []byte("hunter2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(clone, "link")); err != nil {
		t.Skip("no symlinks here")
	}
	patch, _, err := Git{}.DiffPatch(context.Background(), clone, "", "link", 1<<10)
	if err != nil || strings.Contains(patch, "hunter2") {
		t.Errorf("read through the link: %q %v", patch, err)
	}
	files, _ := Git{}.DiffFiles(context.Background(), clone, "")
	if len(files) != 1 || files[0].Added != 0 {
		t.Errorf("a link has no lines of its own: %+v", files)
	}
}
