package userdirs

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNothingIsRelocatedWithoutAStateDirectory(t *testing.T) {
	t.Setenv(Env, "")
	t.Setenv("XDG_CONFIG_HOME", "/xdg/config")
	t.Setenv("XDG_STATE_HOME", "/xdg/state")
	if got, _ := Config(); got != "/xdg/config" {
		t.Errorf("Config = %q", got)
	}
	if got, _ := State(); got != "/xdg/state" {
		t.Errorf("State = %q", got)
	}
	if DefaultRoot() != "~/multiverse" || Suffix() != "" || Dir() != "" {
		t.Errorf("root %q, suffix %q, dir %q", DefaultRoot(), Suffix(), Dir())
	}
}

// With a state directory every location is inside it, whatever the XDG
// variables of the real user say, and the shared proxy gets a project name of
// its own.
func TestAStateDirectoryHoldsEveryUserLocation(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(Env, dir)
	t.Setenv("XDG_CONFIG_HOME", "/xdg/config")
	t.Setenv("XDG_STATE_HOME", "/xdg/state")
	locations := map[string]func() (string, error){"home": Home, "config": Config, "data": Data, "state": State, "cache": Cache}
	for name, f := range locations {
		got, err := f()
		if err != nil || !strings.HasPrefix(got, dir+string(filepath.Separator)) {
			t.Errorf("%s = %q, %v: want it under %s", name, got, err, dir)
		}
	}
	if got := DefaultRoot(); got != filepath.Join(dir, "home", "multiverse") {
		t.Errorf("DefaultRoot = %q", got)
	}
	if s := Suffix(); len(s) != 9 || s[0] != '-' {
		t.Errorf("Suffix = %q", s)
	}
	other := t.TempDir()
	t.Setenv(Env, other)
	if Suffix() == "" {
		t.Error("another directory must still get a suffix")
	}
}

// A rehearsal installs under its own directory even when the user names a
// root for real installs, which it would otherwise install into.
func TestARehearsalIgnoresTheRealInstallRoot(t *testing.T) {
	t.Setenv("MULTIVERSECTL_ROOT", "/srv/work")
	t.Setenv(Env, "")
	if got := RootEnv(); got != "/srv/work" {
		t.Errorf("RootEnv() = %q, want the user's root", got)
	}
	t.Setenv(Env, t.TempDir())
	if got := RootEnv(); got != "" {
		t.Errorf("RootEnv() = %q under a state dir, want none", got)
	}
}
