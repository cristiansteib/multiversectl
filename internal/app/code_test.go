package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

type codeGit struct {
	Git
	// The repositories of a universe are read at once, so what the fake records
	// of them is behind a lock.
	mu        sync.Mutex
	diffs     map[string]DiffStat
	asked     []string
	pushed    bool
	noDefault bool
	fetched   time.Time
	diffErr   map[string]error
	fetches   []string
}

// A universe's branch that has never been pushed has no counterpart on
// origin, and asking the distance to it fails: that is what makes a listing
// fall back to the branch the universe was started from.
func (g *codeGit) Behind(_ context.Context, _, ref string) (int, error) {
	if ref == "u3" && !g.pushed {
		return 0, errors.New("unknown revision origin/u3")
	}
	return 2, nil
}
func (g *codeGit) Ahead(context.Context, string, string) (int, error) { return 3, nil }
func (g *codeGit) IsClean(context.Context, string) (bool, error)      { return false, nil }
func (g *codeGit) DefaultBranch(context.Context, string) (string, error) {
	if g.noDefault {
		return "", errors.New("no origin/HEAD")
	}
	return "main", nil
}
func (g *codeGit) LastFetch(context.Context, string) time.Time { return g.fetched }
func (g *codeGit) FetchQuietly(_ context.Context, dir string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.fetches = append(g.fetches, dir)
	return nil
}
func (g *codeGit) DiffStat(_ context.Context, dir, ref string) (DiffStat, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.asked = append(g.asked, dir+"@"+ref)
	return g.diffs[ref], g.diffErr[ref]
}
func (g *codeGit) DiffFiles(context.Context, string, string) ([]FileChange, error) { return nil, nil }
func (g *codeGit) Commits(context.Context, string, string, int) ([]Commit, error)  { return nil, nil }

type codeHost struct {
	RepoHost
	owns      string
	review    *Review
	err       error
	available error
	asked     []string
}

func (h *codeHost) Name() string                    { return "Example" }
func (h *codeHost) Owns(repoURL string) bool        { return h.owns != "" && repoURL == h.owns }
func (h *codeHost) Available(context.Context) error { return h.available }
func (h *codeHost) FindReview(_ context.Context, repoURL, branch string) (*Review, error) {
	h.asked = append(h.asked, repoURL+"#"+branch)
	return h.review, h.err
}

func codeService(git *codeGit, hosts ...RepoHost) *Service {
	st := domain.UniverseState{Name: "3", ID: "3", Slug: "3", Project: "acme-u3", Branches: map[string]string{"api": "u3", "ui": "u3"}}
	m := &domain.Manifest{Name: "acme",
		Repos: map[string]domain.Repo{"api": {URL: "https://example.test/acme/api.git"}, "ui": {URL: "https://elsewhere.test/acme/ui.git"}},
		Apps:  map[string]domain.App{"api": {Repo: "api"}}}
	return New(Deps{Store: &pruneStore{states: []domain.UniverseState{st}}, Git: git, Files: everywhere{},
		Out: discardOut{}, Hosts: hosts}, "/ws", m, "test")
}

func TestEachRepositoryIsMeasuredAgainstWhatItFollows(t *testing.T) {
	git := &codeGit{diffs: map[string]DiffStat{
		"main": {Files: 4, Added: 120, Removed: 30},
		"":     {Files: 1, Added: 7},
	}}
	rows, err := codeService(git).UniverseCode(context.Background(), "3", false)
	if err != nil {
		t.Fatalf("UniverseCode: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("want a row per repository, got %d", len(rows))
	}
	api := rows[0]
	if api.Repo != "api" || api.App != "api" || api.Branch != "u3" {
		t.Errorf("the row is filed wrong: %+v", api)
	}
	if api.Follows != "main" || api.Behind != 2 || api.Ahead != 3 {
		t.Errorf("want it measured against main, got %+v", api.Freshness)
	}
	if api.Work != (DiffStat{Files: 4, Added: 120, Removed: 30}) {
		t.Errorf("want the work against main, got %+v", api.Work)
	}
	// It is dirty, so what is not committed is counted too, and separately.
	if api.Uncommitted != (DiffStat{Files: 1, Added: 7}) {
		t.Errorf("want the uncommitted change apart, got %+v", api.Uncommitted)
	}
}

// Once the branch is on origin it is its own yardstick: the question stops
// being "how far from main" and becomes "how far from what was pushed".
func TestAPushedBranchIsMeasuredAgainstItsOwnCounterpart(t *testing.T) {
	git := &codeGit{pushed: true, diffs: map[string]DiffStat{"u3": {Files: 2, Added: 9}}}
	rows, err := codeService(git).UniverseCode(context.Background(), "3", false)
	if err != nil {
		t.Fatalf("UniverseCode: %v", err)
	}
	if rows[0].Follows != "u3" || rows[0].Work != (DiffStat{Files: 2, Added: 9}) {
		t.Errorf("want it measured against origin/u3, got %+v %+v", rows[0].Freshness, rows[0].Work)
	}
}

func TestOnlyTheServiceThatOwnsTheRepositoryIsAsked(t *testing.T) {
	host := &codeHost{owns: "https://example.test/acme/api.git", review: &Review{Number: 12, Title: "Keep the tax column"}}
	rows, err := codeService(&codeGit{}, host).UniverseCode(context.Background(), "3", false)
	if err != nil {
		t.Fatalf("UniverseCode: %v", err)
	}
	if len(host.asked) != 1 || host.asked[0] != "https://example.test/acme/api.git#u3" {
		t.Fatalf("want only the repository it owns asked about, got %v", host.asked)
	}
	if rows[0].Review == nil || rows[0].Review.Number != 12 {
		t.Errorf("want the review on the api row, got %+v", rows[0].Review)
	}
	if rows[1].Review != nil {
		t.Errorf("want no review where no service owns the repository, got %+v", rows[1].Review)
	}
}

// Not being signed in is not a failure of the universe, but a row with no review
// and no word of why looks the same as a branch that never had one.
func TestAServiceNobodyIsSignedInToIsSaid(t *testing.T) {
	host := &codeHost{owns: "https://example.test/acme/api.git", available: &HostUnavailable{Reason: "not signed in", CanSignIn: true, Fix: "tool auth login"}}
	rows, err := codeService(&codeGit{}, host).UniverseCode(context.Background(), "3", false)
	if err != nil {
		t.Fatalf("UniverseCode: %v", err)
	}
	if len(host.asked) != 0 {
		t.Errorf("a service that cannot be used should not be asked, got %v", host.asked)
	}
	for _, want := range []string{"not signed in", "tool auth login"} {
		if !strings.Contains(rows[0].ReviewError, want) {
			t.Errorf("the row does not say %q: %q", want, rows[0].ReviewError)
		}
	}
}

func TestAServiceThatFailsSaysSoRatherThanShowingNoReview(t *testing.T) {
	host := &codeHost{owns: "https://example.test/acme/api.git", err: errors.New("api rate limit exceeded")}
	rows, err := codeService(&codeGit{}, host).UniverseCode(context.Background(), "3", false)
	if err != nil {
		t.Fatalf("UniverseCode: %v", err)
	}
	if rows[0].ReviewError != "api rate limit exceeded" {
		t.Errorf("want the service's own words, got %q", rows[0].ReviewError)
	}
}

// A repository whose clone has no usable origin has nothing to measure
// against. Asking for a diff with no ref answers "what is edited and not
// committed", which the page would have drawn as committed work.
func TestARepositoryWithNoOriginIsNotCreditedWithWork(t *testing.T) {
	git := &codeGit{noDefault: true, diffs: map[string]DiffStat{"": {Files: 2, Added: 14}}}
	rows, err := codeService(git).UniverseCode(context.Background(), "3", false)
	if err != nil {
		t.Fatalf("UniverseCode: %v", err)
	}
	if rows[0].Follows != "" {
		t.Fatalf("this fake has no branch to follow, got %q", rows[0].Follows)
	}
	if !rows[0].Work.Empty() {
		t.Errorf("want no committed work, got %+v", rows[0].Work)
	}
	if !rows[0].Dirty || rows[0].Uncommitted != (DiffStat{Files: 2, Added: 14}) {
		t.Errorf("want the edit reported as uncommitted, got dirty=%v %+v", rows[0].Dirty, rows[0].Uncommitted)
	}
	for _, asked := range git.asked {
		if asked != "/ws/universes/3/api@" && asked != "/ws/universes/3/ui@" {
			t.Errorf("want only the uncommitted diff asked for, got %q", asked)
		}
	}
}

// A pushed branch is its own yardstick for what is left to push, so the work a
// review shows — the branch against what it merges into — is measured apart.
func TestWhatABranchAmountsToIsMeasuredAgainstItsBase(t *testing.T) {
	git := &codeGit{pushed: true, diffs: map[string]DiffStat{"u3": {}, "main": {Files: 1, Added: 1}, "release": {Files: 5, Added: 50}}}
	rows, err := codeService(git).UniverseCode(context.Background(), "3", false)
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].Base != "main" || rows[0].Change != (DiffStat{Files: 1, Added: 1}) || !rows[0].Work.Empty() {
		t.Errorf("want the change against main and nothing left to push, got base %q change %+v work %+v", rows[0].Base, rows[0].Change, rows[0].Work)
	}

	// A review says what it is to be merged into, and that is the yardstick.
	host := &codeHost{owns: "https://example.test/acme/api.git", review: &Review{Number: 7, State: "open", Base: "release"}}
	rows, _ = codeService(git, host).UniverseCode(context.Background(), "3", false)
	if rows[0].Base != "release" || rows[0].Change.Files != 5 {
		t.Errorf("want the change against the review's base, got base %q change %+v", rows[0].Base, rows[0].Change)
	}
}

func TestRereadingFetchesEachCloneOnceAndLeavesItAloneOtherwise(t *testing.T) {
	git := &codeGit{}
	if _, err := codeService(git).UniverseCode(context.Background(), "3", false); err != nil || len(git.fetches) != 0 {
		t.Fatalf("a plain read reached for a remote: %v %v", git.fetches, err)
	}
	if _, err := codeService(git).UniverseCode(context.Background(), "3", true); err != nil || len(git.fetches) != 2 {
		t.Fatalf("want a fetch per clone, got %v %v", git.fetches, err)
	}
}

// A base that is not in the clone cannot be measured against, and an empty
// change measured against nothing is not "no changes".
func TestABaseThatCannotBeMeasuredIsSaidSoNotShownAsNoChange(t *testing.T) {
	git := &codeGit{pushed: true, diffErr: map[string]error{"main": errors.New("unknown revision origin/main")}}
	rows, err := codeService(git).UniverseCode(context.Background(), "3", false)
	if err != nil {
		t.Fatal(err)
	}
	if !rows[0].BaseUnknown || rows[0].Base != "main" || !rows[0].Change.Empty() {
		t.Errorf("want the base said to be unknown, got %+v", rows[0])
	}
}

// The files are of the change the page showed the numbers for: it names the
// base, and the hosting service is not asked again, which could answer otherwise.
func TestADiffIsOfTheBaseThePageShowedAndAsksNoHost(t *testing.T) {
	host := &codeHost{owns: "https://example.test/acme/api.git", review: &Review{Number: 7, State: "open", Base: "main"}}
	svc := codeService(&codeGit{}, host)
	if _, _, err := svc.UniverseDiff(context.Background(), "3", "api", ScopeBranch, "release/2.x"); err != nil {
		t.Fatal(err)
	}
	if len(host.asked) != 0 {
		t.Errorf("the hosting service was asked: %v", host.asked)
	}
	for _, bad := range []string{"-p", "main..other", "a b", "/etc", "x\ny"} {
		if _, _, err := svc.UniverseDiff(context.Background(), "3", "api", ScopeBranch, bad); err == nil {
			t.Errorf("%q was taken for a branch", bad)
		}
	}
	if _, _, err := svc.UniverseDiff(context.Background(), "3", "api", "bogus", ""); err == nil {
		t.Error("a scope that is neither was accepted")
	}
	if _, _, err := svc.UniverseDiff(context.Background(), "3", "nope", ScopeBranch, "main"); err == nil {
		t.Error("a repository that is not in the universe was read")
	}
}

// instanceHost is a service with a public instance and company ones, each signed
// in separately: it answers for the instance it was pointed at.
type instanceHost struct {
	RepoHost
	instance string
	signedIn map[string]bool
	asked    *[]string
}

func (h instanceHost) Name() string { return "GitLab" }
func (h instanceHost) Owns(repoURL string) bool {
	return domain.Locate(repoURL, "").Platform == domain.PlatformGitLab
}
func (h instanceHost) At(hostname string) RepoHost {
	h.instance = hostname
	return h
}
func (h instanceHost) Available(context.Context) error {
	if !h.signedIn[h.instance] {
		return &HostUnavailable{Reason: "not signed in"}
	}
	return nil
}
func (h instanceHost) FindReview(_ context.Context, repoURL, branch string) (*Review, error) {
	*h.asked = append(*h.asked, h.instance+" "+repoURL+"#"+branch)
	return &Review{Number: 7, URL: "https://" + h.instance + "/muse/ui/-/merge_requests/7", State: "open"}, nil
}

// A repository on a company's own GitLab is asked there, and signing in to the
// public service neither helps nor hinders it: before, the question went to the
// tool's default instance and the merge request was never found.
func TestAReviewIsLookedForOnTheInstanceTheRepositoryIsOn(t *testing.T) {
	var asked []string
	host := instanceHost{signedIn: map[string]bool{"gitlab.example.test": true}, asked: &asked}
	svc := New(Deps{Hosts: []RepoHost{host}, Out: discardOut{}}, "/ws", &domain.Manifest{}, "test")
	ready := &readiness{}

	review, errText := svc.reviewOf(context.Background(), ready, "git@gitlab.example.test:muse/ui.git", "chore/x")
	if review == nil || errText != "" || len(asked) != 1 || asked[0] != "gitlab.example.test git@gitlab.example.test:muse/ui.git#chore/x" {
		t.Fatalf("review %v, error %q, asked %v", review, errText, asked)
	}
	// The public instance is a different sign-in: not being on it is said for that
	// repository, not hidden behind the company one being fine.
	review, errText = svc.reviewOf(context.Background(), ready, "git@gitlab.com:muse/ui.git", "chore/x")
	if review != nil || !strings.Contains(errText, "not signed in") {
		t.Errorf("a repository on gitlab.com: review %v, error %q", review, errText)
	}
}

// doctor says which services the repositories are on cannot be asked, and what
// to run, instead of leaving every branch looking as if it never had a review.
func TestDoctorSaysWhichHostingServicesCannotBeAsked(t *testing.T) {
	var asked []string
	host := instanceHost{signedIn: map[string]bool{"gitlab.example.test": true}, asked: &asked}
	m := &domain.Manifest{Repos: map[string]domain.Repo{
		"ui":  {URL: "git@gitlab.example.test:muse/ui.git"},
		"api": {URL: "git@gitlab.example.test:muse/api.git"},
		"lib": {URL: "git@gitlab.com:muse/lib.git"},
		"fs":  {URL: "/srv/repos/fs"},
	}}
	svc := New(Deps{Hosts: []RepoHost{host}, Out: discardOut{}}, "/ws", m, "test")
	got := map[string]Check{}
	svc.checkHosting(context.Background(), func(name string, status Status, format string, a ...any) {
		got[name] = Check{Name: name, Status: status, Detail: fmt.Sprintf(format, a...)}
	})
	if len(got) != 2 {
		t.Fatalf("want one check per instance a repository is on, got %v", got)
	}
	company := got["hosting gitlab gitlab.example.test"]
	if company.Status != StatusOK || !strings.Contains(company.Detail, "api, ui") {
		t.Errorf("the signed-in instance: %+v", company)
	}
	public := got["hosting gitlab"]
	if public.Status != StatusOK || !strings.Contains(public.Detail, "lib") {
		// instanceHost's error carries no sign-in fix, so it is only said.
		t.Errorf("the public instance: %+v", public)
	}
}
