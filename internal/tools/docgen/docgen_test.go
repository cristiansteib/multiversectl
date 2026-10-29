package main

import (
	"os"
	"path/filepath"
	"testing"
)

// The committed site must be what the sources produce, so nobody has to
// remember to run `make docs` — and so a page added to nav.yaml without a
// fragment, or a fragment no page renders, fails here rather than silently
// publishing a broken menu.
func TestCommittedSiteIsCurrent(t *testing.T) {
	src := filepath.Join("..", "..", "..", "docsrc")
	committed := filepath.Join("..", "..", "..", "docs")

	fresh := t.TempDir()
	if err := render(src, fresh, filepath.Join(committed, "assets")); err != nil {
		t.Fatal(err)
	}

	err := filepath.WalkDir(fresh, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(fresh, path)
		if err != nil {
			return err
		}
		want, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		got, err := os.ReadFile(filepath.Join(committed, rel))
		if err != nil {
			t.Errorf("docs/%s is missing — run: make docs", rel)
			return nil
		}
		if string(got) != string(want) {
			t.Errorf("docs/%s is stale — run: make docs", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
