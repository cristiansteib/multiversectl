package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cristiansteib/multiversectl/internal/adapters/updatecache"
	"github.com/cristiansteib/multiversectl/internal/app"
)

type recentReleases struct {
	fakeReleases
	rels []app.Release
	err  error
}

func (r recentReleases) Recent(context.Context) ([]app.Release, error) { return r.rels, r.err }

func TestWhatABuildHasMissedIsCountedFromTheReleasesAfterIt(t *testing.T) {
	rec := app.UpdateRecord{Releases: []app.ReleaseNotes{
		{Version: "v0.0.43", Fixes: []string{"c", "d"}},
		{Version: "v0.0.42", Fixes: []string{"a", "b"}, Changes: 1},
		{Version: "v0.0.41", Fixes: []string{"old"}},
	}}
	n, ok := rec.Notice("v0.0.41")
	if !ok || n.Latest != "v0.0.43" || n.Fixes != 4 || n.Changes != 1 || len(n.Highlights) != 4 || n.Highlights[0] != "c" {
		t.Fatalf("got %+v %v", n, ok)
	}
	if n.More {
		t.Error("v0.0.41 is in the list, so the count is whole")
	}
	if n, _ := rec.Notice("v0.0.30"); !n.More || n.Fixes != 5 {
		t.Errorf("older than anything listed: %+v", n)
	}
	if _, ok := rec.Notice("v0.0.43"); ok {
		t.Error("the newest build was told to update")
	}
	if _, ok := rec.Notice("dev"); ok {
		t.Error("a build from source was told to update")
	}
}

func TestOnlyAReleaseIsToldToUpdate(t *testing.T) {
	for v, want := range map[string]bool{"v1.2.3": true, "1.2.3": true, "dev": false, "": false} {
		if got := app.Released(v); got != want {
			t.Errorf("Released(%q) = %v", v, got)
		}
	}
}

func TestALookIsKeptForTheNextCommand(t *testing.T) {
	store := updatecache.At(t.TempDir() + "/update.json")
	rels := recentReleases{rels: []app.Release{{Version: "v0.0.42", Fixes: []string{"a repository with only a compose file runs as itself", "the page parses"}, Changes: 1}}}
	svc := app.New(app.Deps{Releases: rels, Updates: store}, t.TempDir(), nil, "v0.0.41")

	now := time.Now()
	if _, ok := svc.CachedUpdate(); ok {
		t.Fatal("nothing was looked at yet")
	}
	if _, err := svc.CheckForUpdate(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	rec, ok := svc.CachedUpdate()
	if !ok || rec.Stale(now.Add(time.Minute)) || !rec.Stale(now.Add(app.UpdateEvery)) {
		t.Fatalf("got %+v %v", rec, ok)
	}
	n, _ := rec.Notice("v0.0.41")
	want := []string{"a repository with only a compose file runs as itself", "the page parses"}
	if n.Fixes != 2 || n.Changes != 1 || len(n.Highlights) != 2 || n.Highlights[0] != want[0] || n.Highlights[1] != want[1] {
		t.Errorf("got %+v", n)
	}
}

func TestAFailedLookIsStillALookAndKeepsWhatWasFound(t *testing.T) {
	store := updatecache.At(t.TempDir() + "/update.json")
	svc := app.New(app.Deps{Releases: recentReleases{rels: []app.Release{{Version: "v0.0.42", Fixes: []string{"a fix"}}}}, Updates: store}, t.TempDir(), nil, "v0.0.41")
	first := time.Now()
	if _, err := svc.CheckForUpdate(context.Background(), first); err != nil {
		t.Fatal(err)
	}

	svc.Releases = recentReleases{err: errors.New("offline")}
	later := first.Add(time.Hour)
	if _, err := svc.CheckForUpdate(context.Background(), later); err == nil {
		t.Fatal("want the error")
	}
	rec, _ := svc.CachedUpdate()
	if rec.Stale(later.Add(time.Minute)) {
		t.Error("an offline machine would look again on every command")
	}
	if _, ok := rec.Notice("v0.0.41"); !ok {
		t.Error("the earlier finding was lost")
	}
}

// Where nothing can be kept the throttle cannot hold, and a look that no
// command can remember is a look made on every command.
func TestNoLookIsClaimedWhereNothingCanBeKept(t *testing.T) {
	svc := app.New(app.Deps{Releases: recentReleases{}}, t.TempDir(), nil, "v0.0.41")
	if svc.ClaimUpdateLook(time.Now()) {
		t.Error("claimed with nowhere to keep the claim")
	}
	kept := app.New(app.Deps{Releases: recentReleases{}, Updates: updatecache.At(t.TempDir() + "/u.json")}, t.TempDir(), nil, "v0.0.41")
	if !kept.ClaimUpdateLook(time.Now()) {
		t.Error("the claim was not kept")
	}
}
