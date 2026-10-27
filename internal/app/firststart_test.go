package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

type setupRuntime struct {
	Runtime
	failures int // tries that fail before one succeeds; -1 is every one
	ran      []string
}

func (r *setupRuntime) ExecQuiet(_ context.Context, _ RuntimeRequest, service string, args []string) (string, error) {
	r.ran = append(r.ran, fmt.Sprintf("%s %v", service, args))
	if r.failures != 0 {
		if r.failures > 0 {
			r.failures--
		}
		return "relation does not exist", errors.New("exit status 1")
	}
	return "", nil
}

type savingStore struct {
	Store
	saved []domain.UniverseState
}

func (s *savingStore) Save(st domain.UniverseState) error { s.saved = append(s.saved, st); return nil }

type warnings struct {
	discardOut
	warned []string
}

func (w *warnings) Warn(format string, a ...any) {
	w.warned = append(w.warned, fmt.Sprintf(format, a...))
}

func setupService(rt *setupRuntime, steps ...domain.SetupStep) (*Service, *savingStore, *warnings) {
	store, out := &savingStore{}, &warnings{}
	m := &domain.Manifest{Apps: map[string]domain.App{"api": {Repo: "api", Setup: steps}}}
	return New(Deps{Runtime: rt, Store: store, Out: out}, "/ws", m, "test"), store, out
}

func quickly(t *testing.T) {
	t.Helper()
	every, giveUp := setupEvery, setupGiveUp
	setupEvery, setupGiveUp = time.Millisecond, 200*time.Millisecond
	t.Cleanup(func() { setupEvery, setupGiveUp = every, giveUp })
}

var loadFixture = domain.SetupStep{Command: []string{"python", "manage.py", "createfixture"}}

func TestSetupRunsOnceInTheOnlyServiceAndIsKept(t *testing.T) {
	quickly(t)
	rt := &setupRuntime{}
	svc, store, _ := setupService(rt, loadFixture)
	st := svc.firstStart(context.Background(), domain.UniverseState{Name: "1"}, "api", RuntimeRequest{}, []string{"web"})
	if len(rt.ran) != 1 || rt.ran[0] != "web [python manage.py createfixture]" {
		t.Fatalf("ran %v", rt.ran)
	}
	if len(st.SetupDone) != 1 || len(store.saved) != 1 {
		t.Fatalf("not kept: %v, saved %d", st.SetupDone, len(store.saved))
	}
	// A later start finds it done: loading the data again would put it back over
	// whatever was changed since.
	svc.firstStart(context.Background(), st, "api", RuntimeRequest{}, []string{"web"})
	if len(rt.ran) != 1 {
		t.Errorf("ran again: %v", rt.ran)
	}
}

// The service is up while a database is still migrating under it, so the first
// tries fail; that is waited out, not reported.
func TestSetupIsTriedAgainWhileTheServiceIsStillStarting(t *testing.T) {
	quickly(t)
	rt := &setupRuntime{failures: 3}
	svc, _, out := setupService(rt, loadFixture)
	st := svc.firstStart(context.Background(), domain.UniverseState{Name: "1"}, "api", RuntimeRequest{}, []string{"web"})
	if len(rt.ran) != 4 || len(st.SetupDone) != 1 || len(out.warned) != 0 {
		t.Errorf("ran %d times, done %v, warned %v", len(rt.ran), st.SetupDone, out.warned)
	}
}

// A loadFixture that never loads is said, with how to run it by hand, and does not
// stop the universe; it is not marked done, so the next start tries again.
func TestSetupThatNeverSucceedsIsSaidAndNotKept(t *testing.T) {
	quickly(t)
	rt := &setupRuntime{failures: -1}
	svc, store, out := setupService(rt, loadFixture)
	st := svc.firstStart(context.Background(), domain.UniverseState{Name: "1"}, "api", RuntimeRequest{}, []string{"web"})
	if len(st.SetupDone) != 0 || len(store.saved) != 0 {
		t.Errorf("a failed setup was kept: %v", st.SetupDone)
	}
	if len(out.warned) != 1 {
		t.Fatalf("want one warning, got %v", out.warned)
	}
	for _, want := range []string{"universe exec 1 api --service web -- python manage.py createfixture", "next start"} {
		if !contains(out.warned[0], want) {
			t.Errorf("the warning lacks %q: %s", want, out.warned[0])
		}
	}
}

func TestSetupNeedsAServiceWhenTheAppHasSeveral(t *testing.T) {
	quickly(t)
	rt := &setupRuntime{}
	svc, _, out := setupService(rt, loadFixture)
	svc.firstStart(context.Background(), domain.UniverseState{Name: "1"}, "api", RuntimeRequest{}, []string{"web", "db"})
	if len(rt.ran) != 0 || len(out.warned) != 1 {
		t.Errorf("ran %v, warned %v", rt.ran, out.warned)
	}
	named := domain.SetupStep{Service: "db", Command: []string{"psql"}}
	svc, _, _ = setupService(rt, named)
	svc.firstStart(context.Background(), domain.UniverseState{Name: "1"}, "api", RuntimeRequest{}, []string{"web", "db"})
	if len(rt.ran) != 1 || rt.ran[0] != "db [psql]" {
		t.Errorf("a named service was not used: %v", rt.ran)
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
