package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A config written before a workspace was called a project is read, and saved
// back under the new name, so nobody has to migrate by hand.
func TestAnOldConfigIsReadAndSavedUnderTheNewName(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := filepath.Join(dir, "multiversectl", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	old := "current: fichero\ncatalog: https://github.com/me/index\nworkspaces:\n    fichero:\n        root: /w/fichero\n        manifest: /w/fichero/.multiverse/workspace/multiverse.yaml\n        from: personal/fichero\n"
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := New()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if e := cfg.Workspaces["fichero"]; e.Root != "/w/fichero" || e.From != "personal/fichero" || cfg.Current != "fichero" {
		t.Fatalf("not read: %+v", cfg)
	}
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	saved, _ := os.ReadFile(path)
	if !strings.Contains(string(saved), "projects:") || strings.Contains(string(saved), "workspaces:") {
		t.Errorf("saved with the old name:\n%s", saved)
	}
	again, err := store.Load()
	if err != nil || again.Workspaces["fichero"].Root != "/w/fichero" {
		t.Errorf("round trip: %+v, %v", again, err)
	}
}
