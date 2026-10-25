package app

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
)

// Shell is a shell this tool can install completions for.
type Shell string

const (
	Bash Shell = "bash"
	Zsh  Shell = "zsh"
	Fish Shell = "fish"
)

// Shells are the ones with a user-level install; PowerShell's script can be
// printed but is loaded from a profile the user owns.
var Shells = []Shell{Bash, Zsh, Fish}

// ScriptFunc renders the completion script of a shell. It belongs to the
// command line, which knows the command tree; the use case only places it.
type ScriptFunc func(Shell) ([]byte, error)

// CompletionDeps are the ports Completions needs.
type CompletionDeps struct {
	Files   Files
	System  System
	Prompt  Prompter
	Out     Output
	Version string
}

// Completions installs the shell completion script and keeps it current.
type Completions struct{ CompletionDeps }

// NewCompletions builds the use case.
func NewCompletions(d CompletionDeps) *Completions { return &Completions{d} }

// versionKey heads an installed script. Reading it back is how a newer binary
// knows the file on disk is its own, and how old it is.
const versionKey = "# multiversectl-completion-version: "

const (
	rcBegin = "# >>> multiversectl completion >>>"
	rcEnd   = "# <<< multiversectl completion <<<"
)

// Target says where a shell's completions go and what, if anything, has to be
// added to a file the user owns for the shell to find them.
type Target struct {
	Shell Shell
	File  string
	// RC is a shell startup file that must source File. Empty when the shell
	// loads File by itself.
	RC string
}

// DetectShell reads $SHELL.
func (c *Completions) DetectShell() (Shell, error) {
	name := filepath.Base(c.System.Getenv("SHELL"))
	for _, s := range Shells {
		if string(s) == name {
			return s, nil
		}
	}
	return "", fmt.Errorf("cannot tell your shell from $SHELL (%q): pass --shell bash, zsh or fish", c.System.Getenv("SHELL"))
}

// TargetFor places the files under the user's home, never needing root.
func (c *Completions) TargetFor(s Shell) (Target, error) {
	home := c.System.Getenv("HOME")
	if home == "" {
		return Target{}, errors.New("$HOME is not set")
	}
	data := first(c.System.Getenv("XDG_DATA_HOME"), filepath.Join(home, ".local", "share"))
	conf := first(c.System.Getenv("XDG_CONFIG_HOME"), filepath.Join(home, ".config"))
	switch s {
	case Bash:
		// bash-completion loads this directory on demand.
		return Target{Shell: s, File: filepath.Join(data, "bash-completion", "completions", "multiversectl")}, nil
	case Fish:
		return Target{Shell: s, File: filepath.Join(conf, "fish", "completions", "multiversectl.fish")}, nil
	case Zsh:
		// Nothing puts a user directory on $fpath, so the startup file sources it.
		zdot := first(c.System.Getenv("ZDOTDIR"), home)
		return Target{Shell: s, File: filepath.Join(data, "multiversectl", "completions", "multiversectl.zsh"),
			RC: filepath.Join(zdot, ".zshrc")}, nil
	}
	return Target{}, fmt.Errorf("no completions to install for %q", s)
}

// InstalledVersion is the version that wrote the file, "" when there is none.
func (c *Completions) InstalledVersion(t Target) string {
	b, err := c.Files.ReadFile(t.File)
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(string(b), "\n")
	return strings.TrimSpace(strings.TrimPrefix(line, versionKey))
}

// Install writes the script and, for zsh, the block that loads it. Unless yes,
// it says what it will touch and asks first.
func (c *Completions) Install(s Shell, gen ScriptFunc, yes bool) error {
	t, err := c.TargetFor(s)
	if err != nil {
		return err
	}
	have := c.InstalledVersion(t)
	if have == c.Version && c.rcBlockPresent(t) {
		c.Out.OK("%s completions are up to date (%s)", s, t.File)
		return nil
	}
	if !yes {
		c.Out.Plain("This will write %s%s", t.File, c.rcNote(t))
		ok, err := c.Prompt.Confirm(fmt.Sprintf("Install %s completions?", s), true)
		if errors.Is(err, ErrNotInteractive) {
			return errors.New("no terminal to ask on: pass --yes to install, or print the script with: `multiversectl completion " + string(s) + "`")
		}
		if err != nil {
			return err
		}
		if !ok {
			c.Out.Info("nothing changed")
			return nil
		}
	}
	if err := c.write(t, gen); err != nil {
		return err
	}
	if t.RC != "" {
		if err := c.upsertBlock(t); err != nil {
			return err
		}
	}
	verb := "installed"
	if have != "" {
		verb = "updated " + have + " →"
	}
	c.Out.OK("%s %s completions %s (%s)", verb, s, c.Version, t.File)
	if s == Bash {
		c.Out.Info("bash loads these through the bash-completion package; open a new shell")
	} else {
		c.Out.Info("open a new shell to use them")
	}
	return nil
}

// Uninstall removes what Install wrote, the startup-file block included.
func (c *Completions) Uninstall(s Shell) error {
	t, err := c.TargetFor(s)
	if err != nil {
		return err
	}
	removed := false
	if c.Files.Exists(t.File) {
		if err := c.Files.RemoveAll(t.File); err != nil {
			return err
		}
		removed = true
	}
	if t.RC != "" && c.rcBlockPresent(t) {
		b, err := c.Files.ReadFile(t.RC)
		if err != nil {
			return err
		}
		if err := c.Files.WriteFile(t.RC, []byte(dropBlock(string(b))), 0o644); err != nil {
			return err
		}
		removed = true
	}
	if !removed {
		c.Out.Info("no %s completions were installed", s)
		return nil
	}
	c.Out.OK("removed %s completions", s)
	return nil
}

// Refresh rewrites a script this tool installed earlier when it came from
// another version. It never installs: a person who did not ask for completions
// does not get them. It is quiet and best effort, because it runs before
// ordinary commands.
func (c *Completions) Refresh(gen ScriptFunc) {
	for _, s := range Shells {
		t, err := c.TargetFor(s)
		if err != nil {
			return
		}
		if have := c.InstalledVersion(t); have != "" && have != c.Version {
			_ = c.write(t, gen)
		}
	}
}

func (c *Completions) write(t Target, gen ScriptFunc) error {
	script, err := gen(t.Shell)
	if err != nil {
		return err
	}
	if err := c.Files.MkdirAll(filepath.Dir(t.File)); err != nil {
		return err
	}
	return c.Files.WriteFile(t.File, append([]byte(versionKey+c.Version+"\n"), script...), 0o644)
}

func (c *Completions) rcNote(t Target) string {
	if t.RC == "" {
		return ""
	}
	return " and add a block to " + t.RC + " that loads it"
}

func (c *Completions) rcBlockPresent(t Target) bool {
	if t.RC == "" {
		return true
	}
	b, err := c.Files.ReadFile(t.RC)
	return err == nil && strings.Contains(string(b), rcBegin)
}

// upsertBlock appends the loader to the startup file, or replaces the one a
// previous install left, so running it twice changes nothing.
func (c *Completions) upsertBlock(t Target) error {
	b, err := c.Files.ReadFile(t.RC)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	text := dropBlock(string(b))
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	block := rcBegin + "\n" +
		"if [ -f \"" + t.File + "\" ]; then\n" +
		"  (( $+functions[compdef] )) || { autoload -Uz compinit && compinit; }\n" +
		"  source \"" + t.File + "\"\n" +
		"fi\n" + rcEnd + "\n"
	return c.Files.WriteFile(t.RC, []byte(text+block), 0o644)
}

// dropBlock removes the block between the markers, and the blank line before it.
func dropBlock(text string) string {
	i := strings.Index(text, rcBegin)
	if i < 0 {
		return text
	}
	j := strings.Index(text[i:], rcEnd)
	if j < 0 {
		return text
	}
	end := i + j + len(rcEnd)
	if end < len(text) && text[end] == '\n' {
		end++
	}
	before := strings.TrimRight(text[:i], "\n")
	if before != "" {
		before += "\n"
	}
	return before + text[end:]
}

func first(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
