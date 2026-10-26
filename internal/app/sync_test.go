package app_test

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/app"
)

// pushToOrigin lands a commit on origin's main from a clone of its own.
func (b *bench) pushToOrigin(key, file string) {
	b.t.Helper()
	other := filepath.Join(b.t.TempDir(), "other")
	if out, err := exec.Command("git", "clone", "-q", b.origins[key], other).CombinedOutput(); err != nil {
		b.t.Fatalf("%v %s", err, out)
	}
	commitIn(b.t, other, file)
	sh(b.t, other, "push", "-q", "origin", "main")
}

func (b *bench) sync(name string, opts app.SyncOptions) map[string]app.SyncResult {
	b.t.Helper()
	res, err := b.svc.Sync(context.Background(), name, opts)
	if err != nil {
		b.t.Fatal(err)
	}
	out := map[string]app.SyncResult{}
	for _, r := range res {
		out[r.Repo] = r
	}
	return out
}

func TestSyncBringsAUniverseForwardToWhatMainHasNow(t *testing.T) {
	b := newBench(t)
	if _, err := b.create(app.CreateInput{Name: "1"}); err != nil {
		t.Fatal(err)
	}
	b.pushToOrigin("api", "fix")
	dest := filepath.Join(b.ws, "universes", "1", "api")

	res := b.sync("1", app.SyncOptions{})
	if res["api"].Moved != 1 {
		t.Errorf("api: %+v", res["api"])
	}
	if res["web"].Moved != 0 || !strings.Contains(res["web"].Detail, "up to date") {
		t.Errorf("web has nothing new: %+v", res["web"])
	}
	if _, err := exec.Command("test", "-f", filepath.Join(dest, "fix")).CombinedOutput(); err != nil {
		t.Error("the new file should be in the universe's worktree")
	}
}

func TestSyncNeverTouchesUncommittedWorkOrOwnCommitsUnlessAsked(t *testing.T) {
	b := newBench(t)
	if _, err := b.create(app.CreateInput{Name: "1"}); err != nil {
		t.Fatal(err)
	}
	api := filepath.Join(b.ws, "universes", "1", "api")
	web := filepath.Join(b.ws, "universes", "1", "web")
	b.pushToOrigin("api", "fix")
	b.pushToOrigin("web", "fix")

	// api has uncommitted work; web has a commit of its own.
	writeFile(t, filepath.Join(api, "wip"), "x")
	commitIn(t, web, "mine")

	res := b.sync("1", app.SyncOptions{})
	if res["api"].Moved != 0 || !strings.Contains(res["api"].Detail, "uncommitted") {
		t.Errorf("api: %+v", res["api"])
	}
	if res["web"].Moved != 0 || !strings.Contains(res["web"].Detail, "--merge") {
		t.Errorf("web: %+v", res["web"])
	}

	res = b.sync("1", app.SyncOptions{Merge: true})
	if res["web"].Moved != 1 || !strings.Contains(res["web"].Detail, "merged") {
		t.Errorf("web with --merge: %+v", res["web"])
	}
	if sh(t, web, "log", "--format=%s", "-n", "5") == "" || !strings.Contains(sh(t, web, "log", "--format=%s"), "mine") {
		t.Error("its own commit must be kept")
	}
}

func TestUpdateClonesMovesACleanCloneAndLeavesAnEditedOne(t *testing.T) {
	b := newBench(t)
	b.pushToOrigin("api", "one")
	b.pushToOrigin("web", "one")
	writeFile(t, filepath.Join(b.clone("web"), "wip"), "x")
	sh(t, b.clone("web"), "add", "wip") // staged: not clean

	res, err := b.svc.UpdateClones(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]app.CloneResult{}
	for _, r := range res {
		got[r.Repo] = r
	}
	if got["api"].Moved != 1 {
		t.Errorf("api: %+v", got["api"])
	}
	if got["web"].Moved != 0 || !strings.Contains(got["web"].Detail, "uncommitted") {
		t.Errorf("web: %+v", got["web"])
	}
}

func TestAListingSaysHowFarBehindEachCheckoutIs(t *testing.T) {
	b := newBench(t)
	if _, err := b.create(app.CreateInput{Name: "1"}); err != nil {
		t.Fatal(err)
	}
	b.pushToOrigin("api", "fix")
	b.sync("1", app.SyncOptions{}) // fetches; moves api forward
	b.pushToOrigin("api", "fix2")
	if err := gitcliFetch(b.clone("api")); err != nil {
		t.Fatal(err)
	}
	list, err := b.svc.List(context.Background(), app.ListOptions{Freshness: true})
	if err != nil || len(list) != 1 {
		t.Fatalf("%v %v", list, err)
	}
	for _, c := range list[0].Checkouts {
		want := 0
		if c.Repo == "api" {
			want = 1
		}
		if c.Freshness == nil || c.Freshness.Behind != want || c.Freshness.Follows != "main" {
			t.Errorf("%s: %+v", c.Repo, c.Freshness)
		}
	}
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if out, err := exec.Command("sh", "-c", "printf %s \"$1\" > \"$2\"", "sh", text, path).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
}

func gitcliFetch(dir string) error {
	return exec.Command("git", "-C", dir, "fetch", "-q", "origin").Run()
}
