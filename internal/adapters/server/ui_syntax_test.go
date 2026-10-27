package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// A script that does not parse leaves the page blank on "connecting…", and no
// Go test would notice. Node is on the CI runners; elsewhere this is skipped.
func TestPageScriptParses(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	src, err := uiFiles.ReadFile("ui/app.js")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "app.js")
	if err := os.WriteFile(path, src, 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, "--check", path).CombinedOutput(); err != nil {
		t.Fatalf("app.js does not parse:\n%s", out)
	}
}
