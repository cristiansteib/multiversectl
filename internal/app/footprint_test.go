package app_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/app"
)

func kinds(items []app.Item, kind string) []app.Item {
	var out []app.Item
	for _, it := range items {
		if it.Kind == kind {
			out = append(out, it)
		}
	}
	return out
}

func TestTheFootprintListsWhatAProjectMadeAndWhatWouldLoseWork(t *testing.T) {
	b := newBench(t)
	for _, n := range []string{"1", "2"} {
		if _, err := b.create(app.CreateInput{Name: n}); err != nil {
			t.Fatal(err)
		}
	}
	// Uncommitted work in universe 1; a leftover branch; commits on no remote.
	if err := os.WriteFile(filepath.Join(b.ws, "universes", "1", "api", "wip.txt"), []byte("wip"), 0o644); err != nil {
		t.Fatal(err)
	}
	sh(t, b.clone("api"), "branch", "u7")
	sh(t, b.clone("web"), "switch", "-q", "-c", "scratch")
	commitIn(t, b.clone("web"), "unpushed")
	sh(t, b.clone("web"), "switch", "-q", "main")

	items, err := b.svc.ProjectFootprint(context.Background(), "acme")
	if err != nil {
		t.Fatal(err)
	}

	unis := kinds(items, "universe")
	if len(unis) != 2 {
		t.Fatalf("universes: %+v", unis)
	}
	one, two := unis[0], unis[1]
	if !one.Risk || !strings.Contains(one.Note, "uncommitted work in api") ||
		!strings.Contains(one.Note, "2 containers (1 up)") || !strings.Contains(one.Note, "1 volume") {
		t.Errorf("universe 1: %+v", one)
	}
	if two.Risk || two.Note != "" {
		t.Errorf("an untouched universe with nothing running is not a risk: %+v", two)
	}
	if one.Remove != "multiversectl -p acme universe rm 1" || one.Size < 0 {
		t.Errorf("universe 1 removal %q, size %d", one.Remove, one.Size)
	}

	clones := kinds(items, "clone")
	if len(clones) != 2 {
		t.Fatalf("clones: %+v", clones)
	}
	var web app.Item
	for _, c := range clones {
		if strings.Contains(c.What, "web") {
			web = c
		}
	}
	if !web.Risk || !strings.Contains(web.Note, "1 commit on no remote") || !strings.HasPrefix(web.Remove, "rm -rf '") {
		t.Errorf("a clone with an unpushed commit: %+v", web)
	}

	var left []string
	for _, br := range kinds(items, "branch") {
		left = append(left, br.What)
	}
	joined := strings.Join(left, "|")
	if !strings.Contains(joined, "branch u7 in api") {
		t.Errorf("the leftover branch is missing: %v", left)
	}
	if strings.Contains(joined, "u1") || strings.Contains(joined, "u2") {
		t.Errorf("a branch a universe is on is not left over: %v", left)
	}
}

func TestACleanProjectHasNothingToWarnAbout(t *testing.T) {
	b := newBench(t)
	items, err := b.svc.ProjectFootprint(context.Background(), "acme")
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.Risk {
			t.Errorf("nothing was done here: %+v", it)
		}
	}
	if len(kinds(items, "universe")) != 0 || len(kinds(items, "branch")) != 0 {
		t.Errorf("%+v", items)
	}
}
