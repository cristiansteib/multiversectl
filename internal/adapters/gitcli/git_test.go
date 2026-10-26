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
