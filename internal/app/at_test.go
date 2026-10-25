package app_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/app"
)

// A place that does not exist is the moment to show the ones that do.
func atRig(t *testing.T) (*rig, string) {
	t.Helper()
	proj := manifestRepo(t, "billing")
	git := &fakeRepos{repos: map[string]string{"https://github.com/acme/billing.git": proj}, inRepo: true}
	r := newRig(t, git)
	r.Yes = true
	if _, err := r.CreateCatalog(context.Background(), app.CreateCatalogInput{Name: "personal", Same: true, At: "/", AtGiven: true}); err != nil {
		t.Fatal(err)
	}
	r.Yes = false
	return r, "acme/billing"
}

func TestAWrongPlaceInAScriptNamesTheRightOnes(t *testing.T) {
	r, repo := atRig(t) // no terminal: the prompter has nothing to answer with
	_, err := r.Add(context.Background(), app.AddInput{Repo: repo, At: "persnal", AtGiven: true})
	if err == nil {
		t.Fatal("a place that is not there must not be guessed")
	}
	for _, want := range []string{`no catalog at "persnal"`, "the places are:", "/ (the root)", "personal"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error lacks %q: %v", want, err)
		}
	}
}

func TestAWrongPlaceWithYesNeverFallsBackToTheRoot(t *testing.T) {
	r, repo := atRig(t)
	r.Yes = true // --yes takes defaults, but writing somewhere nobody named is not one
	_, err := r.Add(context.Background(), app.AddInput{Repo: repo, At: "nowhere", AtGiven: true})
	if err == nil || !strings.Contains(err.Error(), "the places are:") {
		t.Fatalf("err = %v", err)
	}
	if _, ok := r.tree(t).Find("billing"); ok {
		t.Error("it was added to the root anyway")
	}
}

func TestAWrongPlaceAtATerminalOffersTheChoice(t *testing.T) {
	r, repo := atRig(t)
	r.prompt.selects = []int{1} // the picker: root is 0, personal is 1
	node, err := r.Add(context.Background(), app.AddInput{Repo: repo, At: "persnal", AtGiven: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.prompt.asked[0], `no catalog at "persnal"`) {
		t.Errorf("the question should say what was wrong: %q", r.prompt.asked[0])
	}
	if _, ok := r.tree(t).Find(node); !ok || !strings.HasPrefix(node, "personal/") {
		t.Errorf("it should have gone where it was told to choose, under personal: %q", node)
	}
}

func TestPlacesAreTheCatalogsByTheirPath(t *testing.T) {
	r, _ := atRig(t)
	if got := app.Places(r.tree(t)); got != "/ (the root), personal" {
		t.Errorf("places = %q", got)
	}
}
