package app

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// SkillDeps are the ports Skills needs.
type SkillDeps struct {
	Files   Files
	System  System
	Out     Output
	Version string
}

// Skills installs the agent skill that teaches a coding agent to use this tool
// and keeps what it installed in step with the binary, as Completions does for
// a shell. The text belongs to the command line; the use case only places it.
type Skills struct{ SkillDeps }

// NewSkills builds the use case.
func NewSkills(d SkillDeps) *Skills { return &Skills{d} }

// skillName is the folder a skill lives in, and its name.
const skillName = "multiversectl"

// skillMark heads the body of an installed skill. Reading it back is how a
// newer binary knows the file is its own, and how old it is; a file without it
// was written by someone else and is never touched.
var skillMark = regexp.MustCompile(`multiversectl-skill-version: (\S+) -->`)

// SkillInstall is one place a skill is installed.
type SkillInstall struct {
	// Dir is the folder of skills (such as ~/.claude/skills), File the SKILL.md.
	Dir, File string
	// Version is what the file says it is; empty when it is missing or not ours.
	Version string
	Present bool
	Ours    bool
}

// Current says the file is ours and written by this version.
func (i SkillInstall) Current(version string) bool { return i.Ours && i.Version == version }

func (s *Skills) home() (string, error) {
	h := s.System.Getenv("HOME")
	if h == "" {
		return "", errors.New("$HOME is not set")
	}
	return h, nil
}

// GlobalDir is where a user-level skill goes for Claude Code.
func (s *Skills) GlobalDir() (string, error) {
	h, err := s.home()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, ".claude", "skills"), nil
}

// installs is the list of places this tool put a skill, so an upgrade finds
// project-level ones too.
func (s *Skills) installsFile() (string, error) {
	h, err := s.home()
	if err != nil {
		return "", err
	}
	conf := s.System.Getenv("XDG_CONFIG_HOME")
	if conf == "" {
		conf = filepath.Join(h, ".config")
	}
	return filepath.Join(conf, "multiversectl", "skills.list"), nil
}

func (s *Skills) recorded() []string {
	file, err := s.installsFile()
	if err != nil {
		return nil
	}
	b, err := s.Files.ReadFile(file)
	if err != nil {
		return nil
	}
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func (s *Skills) record(dirs []string) error {
	file, err := s.installsFile()
	if err != nil {
		return err
	}
	if err := s.Files.MkdirAll(filepath.Dir(file)); err != nil {
		return err
	}
	return s.Files.WriteFile(file, []byte(strings.Join(dirs, "\n")+"\n"), 0o644)
}

func (s *Skills) render(body string) string {
	return strings.ReplaceAll(body, "{{version}}", s.Version)
}

// inspect reads one place.
func (s *Skills) inspect(dir string) SkillInstall {
	i := SkillInstall{Dir: dir, File: filepath.Join(dir, skillName, "SKILL.md")}
	b, err := s.Files.ReadFile(i.File)
	if err != nil {
		return i
	}
	i.Present = true
	if m := skillMark.FindSubmatch(b); m != nil {
		i.Ours, i.Version = true, string(m[1])
	}
	return i
}

// Install writes the skill under dir. It refuses to replace a skill of that
// name that this tool did not write.
func (s *Skills) Install(dir, body string) error {
	i := s.inspect(dir)
	if i.Present && !i.Ours {
		return fmt.Errorf("%s exists and multiversectl did not write it; move it away, or install somewhere else with --dir", i.File)
	}
	if err := s.write(i, body); err != nil {
		return err
	}
	dirs := s.recorded()
	for _, d := range dirs {
		if d == dir {
			s.Out.OK("skill %s installed at `%s`", s.Version, i.File)
			return nil
		}
	}
	if err := s.record(append(dirs, dir)); err != nil {
		return err
	}
	s.Out.OK("skill %s installed at `%s`; it follows upgrades of multiversectl", s.Version, i.File)
	return nil
}

func (s *Skills) write(i SkillInstall, body string) error {
	if err := s.Files.MkdirAll(filepath.Dir(i.File)); err != nil {
		return err
	}
	return s.Files.WriteFile(i.File, []byte(s.render(body)), 0o644)
}

// Uninstall removes the skill from dir, only if this tool wrote it.
func (s *Skills) Uninstall(dir string) error {
	i := s.inspect(dir)
	switch {
	case !i.Present:
		s.Out.Info("no skill at `%s`", i.File)
	case !i.Ours:
		return fmt.Errorf("%s was not written by multiversectl; remove it yourself", i.File)
	default:
		if err := s.Files.RemoveAll(filepath.Dir(i.File)); err != nil {
			return err
		}
		s.Out.OK("removed `%s`", i.File)
	}
	var keep []string
	for _, d := range s.recorded() {
		if d != dir {
			keep = append(keep, d)
		}
	}
	return s.record(keep)
}

// Status lists the places a skill is installed: the ones recorded, and the
// global one whether or not this tool recorded it.
func (s *Skills) Status() []SkillInstall {
	seen := map[string]bool{}
	var out []SkillInstall
	add := func(dir string) {
		if dir == "" || seen[dir] {
			return
		}
		seen[dir] = true
		if i := s.inspect(dir); i.Present {
			out = append(out, i)
		}
	}
	if g, err := s.GlobalDir(); err == nil {
		add(g)
	}
	for _, d := range s.recorded() {
		add(d)
	}
	return out
}

// Refresh rewrites installed skills written by another version, so an upgrade
// does not leave an agent reading old advice. It touches only what it wrote and
// installs nothing new.
func (s *Skills) Refresh(body string) {
	for _, i := range s.Status() {
		if i.Ours && i.Version != s.Version {
			_ = s.write(i, body)
		}
	}
}
