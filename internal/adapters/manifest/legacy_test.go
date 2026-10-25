package manifest

import (
	"strings"
	"testing"
)

func TestAManifestThatSaysWorkspaceStillLoads(t *testing.T) {
	old := strings.Replace(minimal, "kind: Project", "kind: Workspace", 1)
	if old == minimal {
		t.Fatal("the fixture has no kind: Project to make old")
	}
	m, err := Decode([]byte(old), t.TempDir())
	if err != nil || m.Name == "" {
		t.Fatalf("%+v, %v", m, err)
	}
	if _, err := Decode([]byte(minimal), t.TempDir()); err != nil {
		t.Errorf("the new name: %v", err)
	}
}
