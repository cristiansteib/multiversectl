package system

import (
	"strings"
	"testing"
)

// Anything the tool writes for the user (shell completion, the agent skill, a
// path written ~/) goes by HOME and the XDG variables, so they follow the state
// directory too; every other variable is the real one.
func TestGetenvPutsTheUsersFilesInTheStateDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MULTIVERSECTL_STATE_DIR", dir)
	t.Setenv("HOME", "/home/real")
	t.Setenv("DOCKER_HOST", "unix:///run/docker.sock")
	for _, key := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		if got := (System{}).Getenv(key); !strings.HasPrefix(got, dir) {
			t.Errorf("%s = %q, want it under %s", key, got, dir)
		}
	}
	if got := (System{}).Getenv("DOCKER_HOST"); got != "unix:///run/docker.sock" {
		t.Errorf("DOCKER_HOST = %q", got)
	}
	t.Setenv("MULTIVERSECTL_STATE_DIR", "")
	if got := (System{}).Getenv("HOME"); got != "/home/real" {
		t.Errorf("HOME = %q without a state directory", got)
	}
}
