package fsx

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSizeSumsAFolderAndDoesNotFollowLinks(t *testing.T) {
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "big"), make([]byte, 5000), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, n := range map[string]int{"a": 100, "sub/b": 250} {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, make([]byte, n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	f := New()
	if n, err := f.Size(dir); err != nil || n != 350 {
		t.Errorf("folder = %d, %v; a link out of the tree is not its target's size", n, err)
	}
	if n, err := f.Size(filepath.Join(dir, "a")); err != nil || n != 100 {
		t.Errorf("file = %d, %v", n, err)
	}
	if _, err := f.Size(filepath.Join(dir, "nope")); err == nil {
		t.Error("a missing path is an error")
	}
}
