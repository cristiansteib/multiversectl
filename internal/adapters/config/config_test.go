package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A config written before a multiverse was called a project is read, and saved
// back under the new name, so nobody has to migrate by hand.
func TestAnOldConfigIsReadAndSavedUnderTheNewName(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := filepath.Join(dir, "multiversectl", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	old := "catalog: https://github.com/me/index\nmultiverses:\n    acme:\n        root: /w/acme\n        manifest: /w/acme/.multiverse/source/multiverse.yaml\n        from: personal/acme\n"
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
	if e := cfg.Multiverses["acme"]; e.Root != "/w/acme" || e.From != "personal/acme" {
		t.Fatalf("not read: %+v", cfg)
	}
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	saved, _ := os.ReadFile(path)
	if !strings.Contains(string(saved), "multiverses:") || strings.Contains(string(saved), "projects:") {
		t.Errorf("saved under the wrong key:\n%s", saved)
	}
	again, err := store.Load()
	if err != nil || again.Multiverses["acme"].Root != "/w/acme" {
		t.Errorf("round trip: %+v, %v", again, err)
	}
}
