package app_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cristiansteib/multiversectl/internal/adapters/fsx"
	"github.com/cristiansteib/multiversectl/internal/app"
)

func statusOf(t *testing.T, r *rig) app.WorkspaceStatus {
	t.Helper()
	rep, err := r.Status(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	return rep.Workspaces[0]
}

func TestAnEditedManifestIsSaidAndUpdateIsNotSuggestedToRefuse(t *testing.T) {
	r, root := installedRig(t)
	mkdirAll(t, root, "billing-api")
	r.git.behind, r.git.dirty = 2, true
	w := statusOf(t, r)
	notes := strings.Join(w.Notes, " | ")
	if !w.Dirty || w.Stale || !strings.Contains(notes, "manifest edited here") || !strings.Contains(notes, "manifest behind 2") {
		t.Errorf("%+v", w)
	}
	if !strings.Contains(w.Advice, "edited here, which update never overwrites") || !strings.Contains(w.Advice, "stash") {
		t.Errorf("advice = %q", w.Advice)
	}
	rep, _ := r.Status(context.Background(), false)
	if rep.Next == "multiversectl project update acme" || !strings.Contains(rep.Next, "stash") {
		t.Errorf("next = %q: an update that would refuse is not what to run", rep.Next)
	}
}

func TestAnEditWithNothingUpstreamIsNotNews(t *testing.T) {
	r, root := installedRig(t)
	mkdirAll(t, root, "billing-api")
	r.git.dirty = true
	w := statusOf(t, r)
	if !w.Dirty || w.Advice != "" || w.Stale || strings.Join(w.Notes, "|") != "manifest edited here" {
		t.Errorf("%+v", w)
	}
}

func TestADivergedManifestSaysSoAndHowToMerge(t *testing.T) {
	r, root := installedRig(t)
	mkdirAll(t, root, "billing-api")
	r.git.ahead, r.git.behind = 1, 3
	w := statusOf(t, r)
	if !w.Diverged || w.Stale || !strings.Contains(strings.Join(w.Notes, "|"), "manifest diverged (1 local, 3 upstream)") || !strings.Contains(w.Advice, "git pull --rebase") {
		t.Errorf("%+v", w)
	}
}

func noticeService(r *rig, env map[string]string) *app.Service {
	e := r.cfg.c.Workspaces["acme"]
	return app.New(app.Deps{Git: r.git, Config: r.cfg, Files: fsx.New(), System: envSystem{env: env}}, e.Root, nil, "test")
}

func TestWorkingInAProjectSaysWhatIsWaitingUpstream(t *testing.T) {
	r, _ := installedRig(t)
	r.git.behind, r.git.lastFetch = 2, time.Now()
	msg := noticeService(r, nil).ManifestNotice(context.Background())
	if !strings.Contains(msg, "`acme`: 2 upstream changes to its manifest: `multiversectl project update acme`") {
		t.Errorf("notice = %q", msg)
	}
	if len(r.git.fetched) != 0 {
		t.Errorf("a recent fetch is not repeated: %v", r.git.fetched)
	}

	r.git.dirty = true
	if msg := noticeService(r, nil).ManifestNotice(context.Background()); !strings.Contains(msg, "keep working") {
		t.Errorf("an edited manifest is told it can keep working: %q", msg)
	}

	r.git.behind, r.git.dirty = 0, false
	if msg := noticeService(r, nil).ManifestNotice(context.Background()); msg != "" {
		t.Errorf("nothing upstream, nothing to say: %q", msg)
	}
}

func TestTheNoticeLooksAgainOnlyWhenTheLastLookIsOld(t *testing.T) {
	r, _ := installedRig(t)
	r.git.behind = 1

	r.git.lastFetch = time.Time{}
	noticeService(r, nil).ManifestNotice(context.Background())
	if len(r.git.fetched) != 1 {
		t.Errorf("never fetched, so it fetches once: %v", r.git.fetched)
	}

	r.git.fetched, r.git.lastFetch = nil, time.Now().Add(-7*time.Hour)
	noticeService(r, nil).ManifestNotice(context.Background())
	if len(r.git.fetched) != 1 {
		t.Errorf("a stale fetch is repeated: %v", r.git.fetched)
	}

	r.git.fetched = nil
	noticeService(r, map[string]string{app.NoFetchEnv: "1"}).ManifestNotice(context.Background())
	if len(r.git.fetched) != 0 {
		t.Errorf("%s keeps it offline: %v", app.NoFetchEnv, r.git.fetched)
	}
}

func mkdirAll(t *testing.T, root, name string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
		t.Fatal(err)
	}
}
