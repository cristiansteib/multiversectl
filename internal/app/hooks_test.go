package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
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
	m := &domain.Manifest{Apps: map[string]domain.App{"api": {Repo: "api", Hooks: map[domain.HookEvent][]domain.Hook{domain.PostUp: hooks}}}}
	return New(Deps{Runtime: rt, Store: store, Out: out, System: fakeSystem{}, Files: &tempFiles{}}, "/ws", m, "test"), store, out
}

// afterUp fires the hooks of an app that has just come up, as Start does.
func afterUp(svc *Service, st domain.UniverseState, services []string) domain.UniverseState {
	st, _ = svc.fire(context.Background(), domain.PostUp, st, "api", &RuntimeRequest{}, services)
	return st
}

// tempFiles keeps the files a hook's context is written to in memory.
type tempFiles struct {
	Files
	written map[string]string
	removed []string
}

func (f *tempFiles) Exists(string) bool { return false }

func (f *tempFiles) TempFile(string) (string, error) {
	name := fmt.Sprintf("/tmp/context-%d.json", len(f.written))
	if f.written == nil {
		f.written = map[string]string{}
	}
	f.written[name] = ""
	return name, nil
}

func (f *tempFiles) WriteFile(path string, data []byte, _ fs.FileMode) error {
	f.written[path] = string(data)
	return nil
}

func (f *tempFiles) RemoveAll(path string) error {
	f.removed = append(f.removed, path)
	return nil
}

// machine records what is run on the machine, and fails the first failures tries.
type machine struct {
	failures int
	ran      []HostCommand
	done     chan struct{}
}

func (m *machine) RunHost(ctx context.Context, c HostCommand) (string, error) {
	m.ran = append(m.ran, c)
	if m.done != nil {
		<-ctx.Done()
		return "", ctx.Err()
	}
	if m.failures != 0 {
		if m.failures > 0 {
			m.failures--
		}
		return "connection refused", errors.New("exit status 7")
	}
	return "", nil
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
	st := afterUp(svc, domain.UniverseState{Name: "1"}, []string{"web"})
	if len(rt.ran) != 1 || rt.ran[0] != "web [python manage.py createfixture]" {
		t.Fatalf("ran %v", rt.ran)
	}
	if len(st.HooksDone) != 1 || len(store.saved) != 1 {
		t.Fatalf("not kept: %v, saved %d", st.HooksDone, len(store.saved))
	}
	// A later start finds it done: loading the data again would put it back over
	// whatever was changed since.
	afterUp(svc, st, []string{"web"})
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
	st := afterUp(svc, domain.UniverseState{Name: "1"}, []string{"web"})
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
	st := afterUp(svc, domain.UniverseState{Name: "1"}, []string{"web"})
	if len(st.HooksDone) != 0 || len(store.saved) != 0 {
		t.Errorf("a failed setup was kept: %v", st.HooksDone)
	}
	if len(out.warned) != 1 {
		t.Fatalf("want one warning, got %v", out.warned)
	}
	for _, want := range []string{"universe exec 1 api --service web -- python manage.py createfixture", "tried again the next time"} {
		if !contains(out.warned[0], want) {
			t.Errorf("the warning lacks %q: %s", want, out.warned[0])
		}
	}
}

func TestAHookNeedsAServiceWhenTheAppHasSeveral(t *testing.T) {
	quickly(t)
	rt := &hookRuntime{}
	svc, _, out := hookService(rt, loadFixture)
	afterUp(svc, domain.UniverseState{Name: "1"}, []string{"web", "db"})
	if len(rt.ran) != 0 || len(out.warned) != 1 {
		t.Errorf("ran %v, warned %v", rt.ran, out.warned)
	}
	named := domain.Hook{Service: "db", Command: []string{"psql"}}
	svc, _, _ = hookService(rt, named)
	afterUp(svc, domain.UniverseState{Name: "1"}, []string{"web", "db"})
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
	st = afterUp(svc, st, []string{"web"})
	afterUp(svc, st, []string{"web"})
	if len(rt.ran) != 2 || len(st.HooksDone) != 0 || len(store.saved) != 0 {
		t.Errorf("ran %v, remembered %v", rt.ran, st.HooksDone)
	}

	rt = &hookRuntime{failures: -1}
	svc, _, _ = hookService(rt, domain.Hook{Command: []string{"first"}}, domain.Hook{Command: []string{"second"}})
	afterUp(svc, domain.UniverseState{Name: "1"}, []string{"web"})
	for _, ran := range rt.ran {
		if strings.Contains(ran, "second") {
			t.Fatalf("a hook ran after the one before it failed: %v", rt.ran)
		}
	}
}

// on is a service whose hooks run for ev on the machine, with the universe's
// addresses to hand.
func onTheMachine(m *machine, ev domain.HookEvent, hooks ...domain.Hook) (*Service, *tempFiles, *warnings) {
	rt := &hookRuntime{}
	svc, _, out := hookService(rt)
	files := svc.Files.(*tempFiles)
	svc.Commands = m
	svc.Manifest.Apps["api"] = domain.App{Repo: "api", Hooks: map[domain.HookEvent][]domain.Hook{ev: hooks}}
	svc.Manifest.Repos = map[string]domain.Repo{"api": {URL: "git@example.test:acme/api.git"}}
	return svc, files, out
}

var universeOne = domain.UniverseState{
	Name: "1", Slug: "1", Domain: "example.test",
	Hosts: map[string]string{"web": "web-1.example.test"},
	Rules: map[string]domain.RouteRule{"web": {App: "api", Service: "web", Port: 8000, Entrypoint: "web", Hostname: "web-1.example.test", ForwardURL: "http://192.0.2.7:41000"}},
}

func TestACommandOnTheMachineGetsTheUniversesAddressesInItsEnvironmentAndAFile(t *testing.T) {
	m := &machine{}
	svc, files, _ := onTheMachine(m, domain.PostUp, domain.Hook{Host: true, Dir: "api", Command: []string{"./register.sh"}})
	svc.Manifest.Proxy.Entrypoints = map[string]int{"web": 80}
	if _, err := svc.fire(context.Background(), domain.PostUp, universeOne, "api", nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(m.ran) != 1 {
		t.Fatalf("ran %v", m.ran)
	}
	got := m.ran[0]
	if !strings.HasSuffix(got.Dir, "/api") {
		t.Errorf("dir %q is not the api checkout", got.Dir)
	}
	for name, want := range map[string]string{
		"MULTIVERSE_EVENT": "postUp", "MULTIVERSE_APP": "api", "UNIVERSE": "1",
		"WEB_URL": "http://web-1.example.test", "WEB_FORWARD_URL": "http://192.0.2.7:41000",
	} {
		if got.Env[name] != want {
			t.Errorf("%s = %q, want %q", name, got.Env[name], want)
		}
	}
	file := got.Env["MULTIVERSE_CONTEXT_FILE"]
	if !strings.Contains(files.written[file], `"forwardUrl": "http://192.0.2.7:41000"`) || !strings.Contains(files.written[file], `"event": "postUp"`) {
		t.Errorf("the context file lacks the addresses: %s", files.written[file])
	}
	if !slices.Contains(files.removed, file) {
		t.Errorf("the context file was left behind: %v", files.removed)
	}
}

// The failure a hook means depends on when it runs: what a start needs stops it,
// and what comes after is said and left alone.
func TestWhatAFailureMeansDependsOnTheEventUnlessTheHookSays(t *testing.T) {
	boom := domain.Hook{Host: true, Command: []string{"make", "prepare"}}
	for _, tt := range []struct {
		name     string
		ev       domain.HookEvent
		hook     domain.Hook
		wantErr  bool
		wantWarn bool
	}{
		{"preUp stops the start", domain.PreUp, boom, true, false},
		{"postUp is said", domain.PostUp, boom, false, true},
		{"preRemove is said, so a broken hook cannot keep a universe", domain.PreRemove, boom, false, true},
		{"a preUp can ask to be said", domain.PreUp, withPolicy(boom, domain.FailWarn), false, true},
		{"a postUp can ask to stop", domain.PostUp, withPolicy(boom, domain.FailStop), true, false},
		{"a hook can be ignored", domain.PreUp, withPolicy(boom, domain.FailIgnore), false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := &machine{failures: -1}
			svc, _, out := onTheMachine(m, tt.ev, tt.hook)
			_, err := svc.fire(context.Background(), tt.ev, universeOne, "api", nil, nil)
			if (err != nil) != tt.wantErr || (len(out.warned) > 0) != tt.wantWarn {
				t.Errorf("err %v, warned %v", err, out.warned)
			}
			if err != nil && !strings.Contains(err.Error(), "make prepare") {
				t.Errorf("the error does not say what failed and how to run it: %v", err)
			}
		})
	}
}

func withPolicy(h domain.Hook, f domain.Failure) domain.Hook { h.OnFailure = f; return h }

func TestACommandOnTheMachineIsTriedAgainOnlyWhenItAsksToBe(t *testing.T) {
	quickly(t)
	m := &machine{failures: 2}
	svc, _, _ := onTheMachine(m, domain.PreUp, domain.Hook{Host: true, Command: []string{"wait-for-it"}, RetryFor: time.Second})
	if _, err := svc.fire(context.Background(), domain.PreUp, universeOne, "api", nil, nil); err != nil || len(m.ran) != 3 {
		t.Errorf("err %v after %d tries", err, len(m.ran))
	}
	m = &machine{failures: 2}
	svc, _, _ = onTheMachine(m, domain.PostUp, domain.Hook{Host: true, Command: []string{"once-only"}})
	svc.fire(context.Background(), domain.PostUp, universeOne, "api", nil, nil)
	if len(m.ran) != 1 {
		t.Errorf("a command that asked for nothing was tried %d times", len(m.ran))
	}
}

func TestATryThatTakesTooLongIsEnded(t *testing.T) {
	m := &machine{done: make(chan struct{})}
	svc, _, _ := onTheMachine(m, domain.PreUp, domain.Hook{Host: true, Command: []string{"hang"}, Timeout: 20 * time.Millisecond})
	_, err := svc.fire(context.Background(), domain.PreUp, universeOne, "api", nil, nil)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err %v", err)
	}
}

func TestACommandInAServiceIsToldTheAddressesToo(t *testing.T) {
	var seen RuntimeRequest
	rt := &envRuntime{seen: &seen}
	svc, _, _ := hookService(&hookRuntime{}, domain.Hook{Command: []string{"register"}})
	svc.Runtime = rt
	if _, err := svc.fire(context.Background(), domain.PostUp, universeOne, "api", &RuntimeRequest{}, []string{"web"}); err != nil {
		t.Fatal(err)
	}
	if seen.ExecEnv["WEB_FORWARD_URL"] != "http://192.0.2.7:41000" || seen.ExecEnv["MULTIVERSE_EVENT"] != "postUp" {
		t.Errorf("exec env %v", seen.ExecEnv)
	}
}

type envRuntime struct {
	Runtime
	seen *RuntimeRequest
}

func (r *envRuntime) ExecQuiet(_ context.Context, req RuntimeRequest, _ string, _ []string) (string, error) {
	*r.seen = req
	return "", nil
}

// The hooks of the multiverse run on the machine around every app, and a Once
// hook of one event is not mistaken for the same command at another.
func TestTheMultiversesHooksAndTheKeysOfOnceHooks(t *testing.T) {
	m := &machine{}
	svc, _, _ := onTheMachine(m, domain.PostUp)
	svc.Manifest.Hooks = map[domain.HookEvent][]domain.Hook{domain.PostUp: {{Host: true, Once: true, Command: []string{"announce"}}}}
	st, err := svc.fire(context.Background(), domain.PostUp, universeOne, multiverseHooks, nil, nil)
	if err != nil || len(m.ran) != 1 || len(st.HooksDone) != 1 {
		t.Fatalf("err %v, ran %d, done %v", err, len(m.ran), st.HooksDone)
	}
	svc.fire(context.Background(), domain.PostUp, st, multiverseHooks, nil, nil)
	if len(m.ran) != 1 {
		t.Errorf("a once hook ran again")
	}
	a := domain.Hook{Host: true, Command: []string{"x"}}
	if a.Key("api", domain.PreUp) == a.Key("api", domain.PostUp) {
		t.Errorf("one command at two events shares a key")
	}
	// What used to be the only kind keeps its key, so a universe does not run
	// its seed again after an upgrade.
	old := domain.Hook{Service: "web", Command: []string{"python", "manage.py", "createfixture"}}
	if got := old.Key("api", domain.PostUp); got != "api|web|python manage.py createfixture" {
		t.Errorf("key %q", got)
	}
}

// Stopping and removing say it to the hooks, the multiverse's around the apps'.
func TestStoppingAndRemovingFireTheirEventsAroundTheApps(t *testing.T) {
	m := &machine{}
	s := lockedService(&fakeLocks{})
	s.System, s.Files, s.Commands = fakeSystem{}, &tempFiles{}, m
	at := func(ev domain.HookEvent) []domain.Hook {
		return []domain.Hook{{Host: true, Command: []string{string(ev)}}}
	}
	s.Manifest.Apps = map[string]domain.App{"api": {Repo: "api", Hooks: map[domain.HookEvent][]domain.Hook{
		domain.PreDown: at(domain.PreDown), domain.PostDown: at(domain.PostDown), domain.PreRemove: at(domain.PreRemove),
	}}}
	s.Manifest.Hooks = map[domain.HookEvent][]domain.Hook{
		domain.PreDown: at("multiverse-preDown"), domain.PostDown: at("multiverse-postDown"), domain.PreRemove: at("multiverse-preRemove"),
	}
	ran := func() string {
		var names []string
		for _, c := range m.ran {
			names = append(names, c.Args[0])
		}
		m.ran = nil
		return strings.Join(names, " ")
	}
	if err := s.Stop(context.Background(), StopInput{Universe: "3"}); err != nil {
		t.Fatal(err)
	}
	if got, want := ran(), "multiverse-preDown preDown postDown multiverse-postDown"; got != want {
		t.Errorf("stop ran %q, want %q", got, want)
	}
	if err := s.Remove(context.Background(), RemoveInput{Universe: "3", Force: true}); err != nil {
		t.Fatal(err)
	}
	if got, want := ran(), "preRemove multiverse-preRemove"; got != want {
		t.Errorf("remove ran %q, want %q", got, want)
	}
}

func TestTheCommandToRunByHandCanBePasted(t *testing.T) {
	got := shellLine([]string{"sh", "-c", `echo "it's $HOME"; exit 3`})
	if want := `sh -c 'echo "it'\''s $HOME"; exit 3'`; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
	if got := shellLine([]string{"python", "manage.py", "loaddata"}); got != "python manage.py loaddata" {
		t.Errorf("a plain command was quoted: %s", got)
	}
}
