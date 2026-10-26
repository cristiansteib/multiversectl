package app_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/adapters/fsx"
	"github.com/cristiansteib/multiversectl/internal/app"
)

const skillBody = "---\nname: multiversectl\n---\n<!-- multiversectl-skill-version: {{version}} -->\nbody\n"

func skillsAt(t *testing.T, home, version string) *app.Skills {
	t.Helper()
	return app.NewSkills(app.SkillDeps{
		Files: fsx.New(), Out: &quiet{}, Version: version,
		System: envSystem{env: map[string]string{"HOME": home}},
	})
}

func TestASkillIsInstalledWithItsVersionAndFollowsUpgrades(t *testing.T) {
	home := t.TempDir()
	old := skillsAt(t, home, "1.0.0")
	dir, _ := old.GlobalDir()
	if err := old.Install(dir, skillBody); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(home, ".claude/skills/multiversectl/SKILL.md")
	if !strings.Contains(read(t, file), "skill-version: 1.0.0 -->") {
		t.Fatalf("not versioned:\n%s", read(t, file))
	}

	// A project-level copy is recorded too, so an upgrade finds it.
	project := filepath.Join(t.TempDir(), ".claude", "skills")
	if err := old.Install(project, skillBody); err != nil {
		t.Fatal(err)
	}

	newer := skillsAt(t, home, "1.1.0")
	newer.Refresh(skillBody)
	for _, f := range []string{file, filepath.Join(project, "multiversectl", "SKILL.md")} {
		if !strings.Contains(read(t, f), "skill-version: 1.1.0 -->") {
			t.Errorf("%s was not refreshed:\n%s", f, read(t, f))
		}
	}
}

func TestASkillThatIsNotOursIsNeverOverwrittenOrRemoved(t *testing.T) {
	home := t.TempDir()
	s := skillsAt(t, home, "1.0.0")
	dir, _ := s.GlobalDir()
	file := filepath.Join(dir, "multiversectl", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("my own skill"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Install(dir, skillBody); err == nil || !strings.Contains(err.Error(), "did not write it") {
		t.Errorf("install over someone else's: %v", err)
	}
	skillsAt(t, home, "2.0.0").Refresh(skillBody)
	if err := s.Uninstall(dir); err == nil {
		t.Error("uninstall removed a file it did not write")
	}
	if read(t, file) != "my own skill" {
		t.Error("the file was changed")
	}
}

func TestUninstallRemovesTheSkillAndForgetsIt(t *testing.T) {
	home := t.TempDir()
	s := skillsAt(t, home, "1.0.0")
	dir, _ := s.GlobalDir()
	if err := s.Install(dir, skillBody); err != nil {
		t.Fatal(err)
	}
	if err := s.Uninstall(dir); err != nil {
		t.Fatal(err)
	}
	if len(s.Status()) != 0 {
		t.Errorf("status = %+v", s.Status())
	}
}

func TestTheNoteAndPRAreKeptWithAUniverse(t *testing.T) {
	b := newBench(t)
	if _, err := b.create(app.CreateInput{Name: "1"}); err != nil {
		t.Fatal(err)
	}
	text, pr := "fixing the login redirect", "https://example.test/acme/api/pull/42"
	if _, err := b.svc.SetNote("1", app.NoteInput{Text: &text}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.svc.SetNote("1", app.NoteInput{PR: &pr}); err != nil { // leaves the text
		t.Fatal(err)
	}
	gotText, gotPR, err := b.svc.Note("1")
	if err != nil || gotText != text || gotPR != pr {
		t.Fatalf("%q %q %v", gotText, gotPR, err)
	}
	list, _ := b.svc.List(context.Background(), app.ListOptions{})
	if len(list) != 1 || list[0].Note != text || list[0].PR != pr {
		t.Errorf("list = %+v", list)
	}
	empty := ""
	if _, err := b.svc.SetNote("1", app.NoteInput{Text: &empty, PR: &empty}); err != nil {
		t.Fatal(err)
	}
	if gotText, gotPR, _ = b.svc.Note("1"); gotText != "" || gotPR != "" {
		t.Errorf("not cleared: %q %q", gotText, gotPR)
	}
}
