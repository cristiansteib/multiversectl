package app_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/adapters/fsx"
	"github.com/cristiansteib/multiversectl/internal/adapters/gitcli"
	"github.com/cristiansteib/multiversectl/internal/adapters/statestore"
	"github.com/cristiansteib/multiversectl/internal/app"
	"github.com/cristiansteib/multiversectl/internal/domain"
)

// These tests run against real git: whether a branch really exists, and
// whether it really is checked out somewhere, is exactly what is being decided.

func sh(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=T", "-c", "user.email=t@example.test", "-c", "commit.gpgsign=false"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func commitIn(t *testing.T, dir, file string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(file), 0o644); err != nil {
		t.Fatal(err)
	}
	sh(t, dir, "add", file)
	sh(t, dir, "commit", "-qm", file)
}

type lines struct{ all []string }

func (l *lines) add(f string, a ...any)   { l.all = append(l.all, fmt.Sprintf(f, a...)) }
func (l *lines) Info(f string, a ...any)  { l.add(f, a...) }
func (l *lines) OK(f string, a ...any)    { l.add(f, a...) }
func (l *lines) Warn(f string, a ...any)  { l.add(f, a...) }
func (l *lines) Plain(f string, a ...any) { l.add(f, a...) }
func (*lines) Table([]string, [][]string) {}
func (l *lines) said(sub string) bool     { return strings.Contains(strings.Join(l.all, "\n"), sub) }

type noRuntime struct{ app.Runtime }

func (noRuntime) Inspect(context.Context, app.RuntimeRequest) (app.ProjectConfig, error) {
	return app.ProjectConfig{}, errors.New("no runtime in a test")
}

// bench is a project of two repositories (api and web), each a clone of its
// own origin, with a Service over them.
type bench struct {
	t       *testing.T
	ws      string
	origins map[string]string
	out     *lines
	svc     *app.Service
}

func newBench(t *testing.T) *bench {
	t.Helper()
	ws := t.TempDir()
	b := &bench{t: t, ws: ws, origins: map[string]string{}, out: &lines{}}
	m := &domain.Manifest{Name: "acme", Domain: "example.test", Repos: map[string]domain.Repo{}}
	for _, key := range []string{"api", "web"} {
		origin := filepath.Join(ws, "_origins", key+".git")
		seed := filepath.Join(ws, "_seeds", key)
		for _, args := range [][]string{{"init", "-q", "--bare", "-b", "main", origin}, {"init", "-q", "-b", "main", seed}} {
			if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
				t.Fatalf("%v: %s", err, out)
			}
		}
		commitIn(t, seed, "a")
		sh(t, seed, "remote", "add", "origin", origin)
		sh(t, seed, "push", "-q", "origin", "main")
		if out, err := exec.Command("git", "clone", "-q", origin, filepath.Join(ws, key)).CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		b.origins[key] = origin
		m.Repos[key] = domain.Repo{URL: origin, Runtime: domain.RuntimeNone}
	}
	b.svc = app.New(app.Deps{
		Git: gitcli.New(), Files: fsx.New(), Store: statestore.New(ws), Runtime: noRuntime{}, Out: b.out,
	}, ws, m, "test")
	return b
}

func (b *bench) clone(key string) string { return filepath.Join(b.ws, key) }

func (b *bench) create(in app.CreateInput) (domain.UniverseState, error) {
	b.t.Helper()
	return b.svc.Create(context.Background(), in)
}

// branchesOf lists a clone's branches, local and on origin.
func (b *bench) branchesOf(key string) string {
	b.t.Helper()
	names, err := gitcli.New().Branches(context.Background(), b.clone(key))
	if err != nil {
		b.t.Fatal(err)
	}
	return strings.Join(names, ",")
}

func TestANewUniverseWithNoNameTakesTheNextFreeNumber(t *testing.T) {
	b := newBench(t)
	st, err := b.create(app.CreateInput{})
	if err != nil || st.Name != "1" {
		t.Fatalf("%+v, %v", st, err)
	}
	if st.Branches["api"] != "u1" || st.Branches["web"] != "u1" {
		t.Errorf("branches = %v", st.Branches)
	}
	if len(st.CreatedBranches) != 2 {
		t.Errorf("the universe should remember it started both branches: %v", st.CreatedBranches)
	}
	if st2, err := b.create(app.CreateInput{}); err != nil || st2.Name != "2" {
		t.Fatalf("the second: %+v, %v", st2, err)
	}
}

func TestAutomaticNumbersStepOverLeftoverBranches(t *testing.T) {
	b := newBench(t)
	sh(t, b.clone("api"), "branch", "u1") // left by a universe that was removed
	sh(t, b.clone("web"), "branch", "u2")
	n, skipped, err := b.svc.NextFree(context.Background())
	if err != nil || n != 3 {
		t.Fatalf("next = %d, %v", n, err)
	}
	joined := strings.Join(skipped, "|")
	if !strings.Contains(joined, "1 (branch u1 exists in api)") || !strings.Contains(joined, "2 (branch u2 exists in web)") {
		t.Errorf("it should say why it skipped: %v", skipped)
	}
	st, err := b.create(app.CreateInput{})
	if err != nil || st.Name != "3" {
		t.Fatalf("%+v, %v", st, err)
	}
	if !b.out.said("skipped 1") {
		t.Errorf("the skip should be reported: %v", b.out.all)
	}
}

func TestANamedNewUniverseNeverReusesABranch(t *testing.T) {
	b := newBench(t)
	sh(t, b.clone("api"), "branch", "u7") // in the clone only

	// And one that only exists on origin, pushed by someone else.
	other := filepath.Join(t.TempDir(), "other")
	if out, err := exec.Command("git", "clone", "-q", b.origins["web"], other).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	sh(t, other, "switch", "-q", "-c", "u7")
	commitIn(t, other, "theirs")
	sh(t, other, "push", "-q", "origin", "u7")

	_, err := b.create(app.CreateInput{Name: "7"})
	var clash *app.ClashError
	if !errors.As(err, &clash) {
		t.Fatalf("err = %v, want a clash", err)
	}
	msg := err.Error()
	for _, want := range []string{`api: branch "u7" already exists (in the clone)`, `web: branch "u7" already exists (on origin)`, "universe checkout", "nothing was created"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal lacks %q:\n%s", want, msg)
		}
	}
	if _, statErr := os.Stat(filepath.Join(b.ws, "universes", "7")); statErr == nil {
		t.Error("a refused universe must leave nothing behind")
	}
}

func TestANewUniverseStartsFromTheBaseYouNameAndFromOriginNotAStaleCopy(t *testing.T) {
	b := newBench(t)
	// origin moves ahead of the clone's own main.
	other := filepath.Join(t.TempDir(), "other")
	if out, err := exec.Command("git", "clone", "-q", b.origins["api"], other).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	sh(t, other, "switch", "-q", "-c", "release")
	commitIn(t, other, "rel")
	sh(t, other, "push", "-q", "origin", "release")
	commitIn(t, other, "rel2")
	sh(t, other, "push", "-q", "origin", "release")

	st, err := b.create(app.CreateInput{Name: "4", Bases: map[string]string{"api": "release"}})
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(b.ws, "universes", "4", "api")
	if sh(t, dest, "rev-parse", "HEAD") != sh(t, b.clone("api"), "rev-parse", "origin/release") {
		t.Error("the branch should start at origin/release as fetched, not at a stale copy")
	}
	if st.Branches["api"] != "u4" {
		t.Errorf("branches = %v", st.Branches)
	}
	if _, err := b.create(app.CreateInput{Name: "5", Base: "nope"}); err == nil || !strings.Contains(err.Error(), `the base "nope" does not exist`) {
		t.Errorf("a base that is not there: %v", err)
	}
}

func TestACheckoutUniverseWorksOnAnExistingBranch(t *testing.T) {
	b := newBench(t)
	other := filepath.Join(t.TempDir(), "other")
	if out, err := exec.Command("git", "clone", "-q", b.origins["api"], other).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	sh(t, other, "switch", "-q", "-c", "fix/login")
	commitIn(t, other, "fix")
	sh(t, other, "push", "-q", "origin", "fix/login")

	st, err := b.create(app.CreateInput{Checkout: true, Branch: "fix/login"})
	if err != nil {
		t.Fatal(err)
	}
	if st.Name != "fix/login" {
		t.Errorf("a checkout universe is named after its branch: %q", st.Name)
	}
	apiDest := filepath.Join(b.ws, "universes", "fix-login", "api")
	if _, err := os.Stat(filepath.Join(apiDest, "fix")); err != nil {
		t.Error("the api worktree should hold the branch's own commit")
	}
	if sh(t, apiDest, "rev-parse", "--abbrev-ref", "HEAD") != "fix/login" {
		t.Error("api is not on fix/login")
	}
	if len(st.CreatedBranches) != 1 || st.CreatedBranches[0] != "web" {
		t.Errorf("only web's branch was started here; api's was someone's: %v", st.CreatedBranches)
	}
	if sh(t, filepath.Join(b.ws, "universes", "fix-login", "web"), "rev-parse", "--abbrev-ref", "HEAD") != "fix/login" {
		t.Error("web, which lacks the branch, should get one of that name")
	}
}

func TestACheckoutCanLeaveTheReposWithoutTheBranchOnTheirBase(t *testing.T) {
	b := newBench(t)
	sh(t, b.clone("api"), "branch", "feat/only-api")
	st, err := b.create(app.CreateInput{Checkout: true, Branch: "feat/only-api", OnlyExisting: true})
	if err != nil {
		t.Fatal(err)
	}
	web := filepath.Join(b.ws, "universes", st.ID, "web")
	if sh(t, web, "rev-parse", "--abbrev-ref", "HEAD") != "HEAD" {
		t.Error("web should be detached")
	}
	if len(st.CreatedBranches) != 0 {
		t.Errorf("nothing was started: %v", st.CreatedBranches)
	}
	if got := b.branchesOf("web"); got != "main" {
		t.Errorf("web gained branches: %s", got)
	}
}

func TestACheckoutRefusesABranchThatIsInUseOrThatDoesNotExist(t *testing.T) {
	b := newBench(t)
	if _, err := b.create(app.CreateInput{Name: "1"}); err != nil {
		t.Fatal(err)
	}
	_, err := b.create(app.CreateInput{Checkout: true, Branch: "u1", Name: "again"})
	if err == nil || !strings.Contains(err.Error(), `branch "u1" is already checked out at`) || !strings.Contains(err.Error(), "one worktree only") {
		t.Errorf("a branch in another universe: %v", err)
	}
	_, err = b.create(app.CreateInput{Checkout: true, Branch: "tpyo"})
	if err == nil || !strings.Contains(err.Error(), `"tpyo" exists in no repository`) {
		t.Errorf("a typo must not quietly create a branch: %v", err)
	}
	if got := b.branchesOf("api"); strings.Contains(got, "tpyo") {
		t.Errorf("the typo became a branch: %s", got)
	}
}

func TestAFailedCreateTakesBackWhatItAdded(t *testing.T) {
	b := newBench(t)
	// api succeeds, web's branch name is not a valid ref, so git refuses it.
	_, err := b.create(app.CreateInput{Name: "6", Branches: map[string]string{"web": "bad..name"}})
	if err == nil {
		t.Fatal("expected a failure")
	}
	if _, statErr := os.Stat(filepath.Join(b.ws, "universes", "6")); statErr == nil {
		t.Error("the universe directory should be gone")
	}
	if got := b.branchesOf("api"); strings.Contains(got, "u6") {
		t.Errorf("api kept the branch the failed universe made: %s", got)
	}
	if at, _ := gitcli.New().CheckedOutAt(context.Background(), b.clone("api"), "u6"); at != "" {
		t.Errorf("a worktree was left at %s", at)
	}
	// And the number is free again.
	if st, err := b.create(app.CreateInput{Name: "6"}); err != nil || st.Name != "6" {
		t.Errorf("retry: %+v, %v", st, err)
	}
}

func TestRemovingDeletesTheBranchesItStartedWhenNothingIsLost(t *testing.T) {
	b := newBench(t)
	st, err := b.create(app.CreateInput{Name: "3"})
	if err != nil {
		t.Fatal(err)
	}
	// api has a commit that is on no remote; web has nothing of its own.
	apiWt := filepath.Join(b.ws, "universes", "3", "api")
	commitIn(t, apiWt, "wip")
	sh(t, apiWt, "add", "-A")

	if err := b.svc.Remove(context.Background(), app.RemoveInput{Universe: st.Name}); err != nil {
		t.Fatal(err)
	}
	if got := b.branchesOf("web"); got != "main" {
		t.Errorf("web's untouched branch should be gone: %s", got)
	}
	if got := b.branchesOf("api"); !strings.Contains(got, "u3") {
		t.Errorf("api's branch holds unpushed work and must stay: %s", got)
	}
	if !b.out.said("kept branch u3") || !b.out.said("1 commits that are on no remote") {
		t.Errorf("it should say why it kept it: %v", b.out.all)
	}
	// The number is not free for a new universe while that branch is there,
	// and the planner says so instead of reusing it.
	if _, err := b.create(app.CreateInput{Name: "3"}); err == nil {
		t.Error("recreating 3 must not land on the kept branch")
	}
	if n, _, _ := b.svc.NextFree(context.Background()); n == 3 {
		t.Error("the next free number must step over 3")
	}
}

func TestRemovingNeverDeletesABranchItOnlyChecksOut(t *testing.T) {
	b := newBench(t)
	sh(t, b.clone("api"), "branch", "mine")
	st, err := b.create(app.CreateInput{Checkout: true, Branch: "mine", OnlyExisting: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.svc.Remove(context.Background(), app.RemoveInput{Universe: st.Name}); err != nil {
		t.Fatal(err)
	}
	if got := b.branchesOf("api"); !strings.Contains(got, "mine") {
		t.Errorf("a branch the universe did not start was deleted: %s", got)
	}
}

func TestBranchesAreListedAcrossTheRepositories(t *testing.T) {
	b := newBench(t)
	sh(t, b.clone("api"), "branch", "feat/x")
	sh(t, b.clone("web"), "branch", "feat/x")
	sh(t, b.clone("web"), "branch", "only-web")
	list, err := b.svc.Branches(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, bi := range list {
		got[bi.Name] = strings.Join(bi.Repos, "+")
	}
	if got["feat/x"] != "api+web" || got["only-web"] != "web" || got["main"] != "api+web" {
		t.Errorf("branches = %v", got)
	}
}
