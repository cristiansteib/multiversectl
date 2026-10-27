package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

type codeGit struct {
	Git
	diffs     map[string]DiffStat
	asked     []string
	pushed    bool
	noDefault bool
	fetched   time.Time
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
func (g *codeGit) Fetch(_ context.Context, dir string) error {
	g.fetches = append(g.fetches, dir)
	return nil
}
func (g *codeGit) DiffStat(_ context.Context, dir, ref string) (DiffStat, error) {
	g.asked = append(g.asked, dir+"@"+ref)
	return g.diffs[ref], nil
}

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

func TestAServiceNobodyIsSignedInToIsNotAFailure(t *testing.T) {
	host := &codeHost{owns: "https://example.test/acme/api.git", available: errors.New("not signed in")}
	rows, err := codeService(&codeGit{}, host).UniverseCode(context.Background(), "3", false)
	if err != nil {
		t.Fatalf("UniverseCode: %v", err)
	}
	if len(host.asked) != 0 {
		t.Errorf("a service that cannot be used should not be asked, got %v", host.asked)
	}
	if rows[0].ReviewError != "" {
		t.Errorf("want no error to show, got %q", rows[0].ReviewError)
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
