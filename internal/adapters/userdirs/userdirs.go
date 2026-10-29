// Package userdirs is the one place that knows where multiversectl keeps what
// belongs to a user: its configuration, caches, state, the multiverses it
// installs and the agent and shell files it writes. Normally those are under
// the user's home and the XDG directories; with MULTIVERSECTL_STATE_DIR (or the
// --state-dir flag, which sets it) they are all under that one directory, so a
// first run can be rehearsed on a machine that has already been used, without
// faking $HOME, and thrown away by deleting a directory.
package userdirs

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
)

// Env is the variable that relocates everything.
const Env = "MULTIVERSECTL_STATE_DIR"

// Dir is the directory everything is relocated under, "" when nothing is.
func Dir() string {
	dir := os.Getenv(Env)
	if dir == "" {
		return ""
	}
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return dir
}

// Home stands in for the user's home directory: the multiverses are installed
// under it by default, and a path written ~/ is relative to it.
func Home() (string, error) {
	if dir := Dir(); dir != "" {
		return filepath.Join(dir, "home"), nil
	}
	return os.UserHomeDir()
}

// Config is the user's configuration directory ($XDG_CONFIG_HOME).
func Config() (string, error) {
	return under("config", "XDG_CONFIG_HOME", ".config")
}

// Data is the user's data directory ($XDG_DATA_HOME).
func Data() (string, error) {
	return under("data", "XDG_DATA_HOME", filepath.Join(".local", "share"))
}

// State is the user's state directory ($XDG_STATE_HOME).
func State() (string, error) {
	return under("state", "XDG_STATE_HOME", filepath.Join(".local", "state"))
}

// Cache is the user's cache directory ($XDG_CACHE_HOME).
func Cache() (string, error) {
	if dir := Dir(); dir != "" {
		return filepath.Join(dir, "cache"), nil
	}
	return os.UserCacheDir()
}

func under(sub, xdg, fromHome string) (string, error) {
	if dir := Dir(); dir != "" {
		return filepath.Join(dir, sub), nil
	}
	if base := os.Getenv(xdg); base != "" {
		return base, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, fromHome), nil
}

// RootEnv is $MULTIVERSECTL_ROOT, where the user installs multiverses, except
// when everything is relocated: a rehearsal must not install into the real
// root, so it uses its own unless --root or --base name another.
func RootEnv() string {
	if Dir() != "" {
		return ""
	}
	return os.Getenv("MULTIVERSECTL_ROOT")
}

// DefaultRoot is where multiverses are installed when nothing says otherwise.
func DefaultRoot() string {
	if Dir() != "" {
		return filepath.Join(Dir(), "home", "multiverse")
	}
	return "~/multiverse"
}

// Suffix tells apart what lives outside the directory and is named for the
// user's machine rather than for a path: the shared proxy is one compose
// project per machine, and a rehearsal in a state directory must not recreate
// the real one. It is "" when nothing is relocated.
func Suffix() string {
	dir := Dir()
	if dir == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(dir))
	return "-" + hex.EncodeToString(sum[:])[:8]
}
