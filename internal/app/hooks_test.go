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

type hookRuntime struct {
	Runtime
	failures int // tries that fail before one succeeds; -1 is every one
	ran      []string
}

func (r *hookRuntime) ExecQuiet(_ context.Context, _ RuntimeRequest, service string, args []string) (string, error) {
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

func hookService(rt *hookRuntime, hooks ...domain.Hook) (*Service, *savingStore, *warnings) {
	store, out := &savingStore{}, &warnings{}
	m := &domain.Manifest{Apps: map[string]domain.App{"api": {Repo: "api", After: hooks}}}
	return New(Deps{Runtime: rt, Store: store, Out: out}, "/ws", m, "test"), store, out
}

func quickly(t *testing.T) {
	t.Helper()
	every, giveUp := hookEvery, hookGiveUp
	hookEvery, hookGiveUp = time.Millisecond, 200*time.Millisecond
	t.Cleanup(func() { hookEvery, hookGiveUp = every, giveUp })
}

var loadFixture = domain.Hook{Command: []string{"python", "manage.py", "createfixture"}, Once: true}

func TestAOnceHookRunsInTheOnlyServiceAndIsKept(t *testing.T) {
	quickly(t)
	rt := &hookRuntime{}
	svc, store, _ := hookService(rt, loadFixture)
	st := svc.runHooks(context.Background(), domain.UniverseState{Name: "1"}, "api", RuntimeRequest{}, []string{"web"})
	if len(rt.ran) != 1 || rt.ran[0] != "web [python manage.py createfixture]" {
		t.Fatalf("ran %v", rt.ran)
	}
	if len(st.HooksDone) != 1 || len(store.saved) != 1 {
		t.Fatalf("not kept: %v, saved %d", st.HooksDone, len(store.saved))
	}
	// A later start finds it done: loading the data again would put it back over
	// whatever was changed since.
	svc.runHooks(context.Background(), st, "api", RuntimeRequest{}, []string{"web"})
	if len(rt.ran) != 1 {
		t.Errorf("ran again: %v", rt.ran)
	}
}

// The service is up while a database is still migrating under it, so the first
// tries fail; that is waited out, not reported.
func TestAHookIsTriedAgainWhileTheServiceIsStillStarting(t *testing.T) {
	quickly(t)
	rt := &hookRuntime{failures: 3}
	svc, _, out := hookService(rt, loadFixture)
	st := svc.runHooks(context.Background(), domain.UniverseState{Name: "1"}, "api", RuntimeRequest{}, []string{"web"})
	if len(rt.ran) != 4 || len(st.HooksDone) != 1 || len(out.warned) != 0 {
		t.Errorf("ran %d times, done %v, warned %v", len(rt.ran), st.HooksDone, out.warned)
	}
}

// A loadFixture that never loads is said, with how to run it by hand, and does not
// stop the universe; it is not marked done, so the next start tries again.
func TestAHookThatNeverSucceedsIsSaidAndNotKept(t *testing.T) {
	quickly(t)
	rt := &hookRuntime{failures: -1}
	svc, store, out := hookService(rt, loadFixture)
	st := svc.runHooks(context.Background(), domain.UniverseState{Name: "1"}, "api", RuntimeRequest{}, []string{"web"})
	if len(st.HooksDone) != 0 || len(store.saved) != 0 {
		t.Errorf("a failed setup was kept: %v", st.HooksDone)
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

func TestAHookNeedsAServiceWhenTheAppHasSeveral(t *testing.T) {
	quickly(t)
	rt := &hookRuntime{}
	svc, _, out := hookService(rt, loadFixture)
	svc.runHooks(context.Background(), domain.UniverseState{Name: "1"}, "api", RuntimeRequest{}, []string{"web", "db"})
	if len(rt.ran) != 0 || len(out.warned) != 1 {
		t.Errorf("ran %v, warned %v", rt.ran, out.warned)
	}
	named := domain.Hook{Service: "db", Command: []string{"psql"}}
	svc, _, _ = hookService(rt, named)
	svc.runHooks(context.Background(), domain.UniverseState{Name: "1"}, "api", RuntimeRequest{}, []string{"web", "db"})
	if len(rt.ran) != 1 || rt.ran[0] != "db [psql]" {
		t.Errorf("a named service was not used: %v", rt.ran)
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }

// Without Once a hook runs after every start, and nothing is remembered; and the
// ones after a hook that failed wait, since they may depend on it.
func TestAHookWithoutOnceRunsAfterEveryStartAndAFailureStopsTheRest(t *testing.T) {
	quickly(t)
	rt := &hookRuntime{}
	every := domain.Hook{Command: []string{"warm"}}
	svc, store, _ := hookService(rt, every)
	st := domain.UniverseState{Name: "1"}
	st = svc.runHooks(context.Background(), st, "api", RuntimeRequest{}, []string{"web"})
	svc.runHooks(context.Background(), st, "api", RuntimeRequest{}, []string{"web"})
	if len(rt.ran) != 2 || len(st.HooksDone) != 0 || len(store.saved) != 0 {
		t.Errorf("ran %v, remembered %v", rt.ran, st.HooksDone)
	}

	rt = &hookRuntime{failures: -1}
	svc, _, _ = hookService(rt, domain.Hook{Command: []string{"first"}}, domain.Hook{Command: []string{"second"}})
	svc.runHooks(context.Background(), domain.UniverseState{Name: "1"}, "api", RuntimeRequest{}, []string{"web"})
	for _, ran := range rt.ran {
		if strings.Contains(ran, "second") {
			t.Fatalf("a hook ran after the one before it failed: %v", rt.ran)
		}
	}
}
