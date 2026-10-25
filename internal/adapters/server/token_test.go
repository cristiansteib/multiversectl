package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTheTokenSurvivesARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "multiversectl", "server.token")
	first, err := PersistentToken(path)
	if err != nil || first == "" {
		t.Fatalf("first = %q, %v", first, err)
	}
	second, err := PersistentToken(path)
	if err != nil || second != first {
		t.Errorf("a restart must find the same token: %q then %q (%v)", first, second, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("the token is for this user only: %v %v", info, err)
	}
	// Deleting the file is how it changes.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if third, _ := PersistentToken(path); third == first {
		t.Error("a deleted token must not come back")
	}
}
