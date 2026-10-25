package app_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/adapters/fsx"
	"github.com/cristiansteib/multiversectl/internal/app"
)

type noTerminal struct{ app.Prompter }

func (noTerminal) Confirm(string, bool) (bool, error) { return false, app.ErrNotInteractive }

func completionsAt(t *testing.T, version string, p app.Prompter) (*app.Completions, string, *quiet) {
	t.Helper()
	home := t.TempDir()
	out := &quiet{}
	return app.NewCompletions(app.CompletionDeps{
		Files: fsx.New(), Prompt: p, Out: out, Version: version,
		System: envSystem{env: map[string]string{"HOME": home, "SHELL": "/bin/zsh"}},
	}), home, out
}

func script(app.Shell) ([]byte, error) { return []byte("#compdef multiversectl\nbody\n"), nil }

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestCompletionInstallIsVersionedAndIdempotent(t *testing.T) {
	c, home, _ := completionsAt(t, "1.2.0", &scripted{})
	if err := c.Install(app.Zsh, script, true); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(home, ".local/share/multiversectl/completions/multiversectl.zsh")
	if got := read(t, file); !strings.HasPrefix(got, "# multiversectl-completion-version: 1.2.0\n") || !strings.Contains(got, "body") {
		t.Fatalf("file = %q", got)
	}
	rc := filepath.Join(home, ".zshrc")
	first := read(t, rc)
	if !strings.Contains(first, file) {
		t.Fatalf(".zshrc does not load the script:\n%s", first)
	}
	if err := c.Install(app.Zsh, script, true); err != nil {
		t.Fatal(err)
	}
	if read(t, rc) != first {
		t.Error("installing twice changed the startup file")
	}
}

func TestCompletionRefreshUpdatesOnlyWhatWasInstalled(t *testing.T) {
	old, home, _ := completionsAt(t, "1.0.0", &scripted{})
	file := filepath.Join(home, ".config/fish/completions/multiversectl.fish")

	old.Refresh(script)
	if _, err := os.Stat(file); err == nil {
		t.Fatal("refresh installed completions nobody asked for")
	}
	if err := old.Install(app.Fish, script, true); err != nil {
		t.Fatal(err)
	}
	newer := app.NewCompletions(app.CompletionDeps{
		Files: fsx.New(), Version: "1.1.0", Out: &quiet{},
		System: envSystem{env: map[string]string{"HOME": home}},
	})
	newer.Refresh(func(app.Shell) ([]byte, error) { return []byte("new body\n"), nil })
	if got := read(t, file); !strings.HasPrefix(got, "# multiversectl-completion-version: 1.1.0\n") || !strings.Contains(got, "new body") {
		t.Errorf("not refreshed: %q", got)
	}
}

func TestCompletionAsksUnlessYes(t *testing.T) {
	c, home, _ := completionsAt(t, "1.0.0", noTerminal{})
	err := c.Install(app.Bash, script, false)
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("err = %v, want a hint to pass --yes", err)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".local/share/bash-completion")); statErr == nil {
		t.Error("wrote something without an answer")
	}

	declined, home, _ := completionsAt(t, "1.0.0", &scripted{confirms: []bool{false}})
	if err := declined.Install(app.Bash, script, false); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".local/share/bash-completion")); statErr == nil {
		t.Error("wrote after the user said no")
	}
}

func TestCompletionUninstallRestoresTheStartupFile(t *testing.T) {
	c, home, _ := completionsAt(t, "1.0.0", &scripted{})
	rc := filepath.Join(home, ".zshrc")
	if err := os.WriteFile(rc, []byte("export A=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := c.Install(app.Zsh, script, true); err != nil {
		t.Fatal(err)
	}
	if err := c.Uninstall(app.Zsh); err != nil {
		t.Fatal(err)
	}
	if got := read(t, rc); got != "export A=1\n" {
		t.Errorf(".zshrc = %q, want what it was", got)
	}
	if _, err := os.Stat(filepath.Join(home, ".local/share/multiversectl/completions/multiversectl.zsh")); err == nil {
		t.Error("script left behind")
	}
}

func TestDetectShell(t *testing.T) {
	c, _, _ := completionsAt(t, "1", &scripted{})
	if s, err := c.DetectShell(); err != nil || s != app.Zsh {
		t.Errorf("got %q, %v", s, err)
	}
}

type envSystem struct {
	app.System
	env map[string]string
}

func (e envSystem) Getenv(k string) string { return e.env[k] }
