package cli

import (
	"strings"
	"testing"
)

func TestMarkupColoursCommandsAndReferencesDifferently(t *testing.T) {
	msg := "a project named `fichero` is already installed; run `multiversectl project uninstall fichero`, then `git -C /x push`"
	plain := Markup(msg, false)
	if plain != "a project named fichero is already installed; run multiversectl project uninstall fichero, then git -C /x push" {
		t.Errorf("without colour the text must be what it always was: %q", plain)
	}
	coloured := Markup(msg, true)
	cmd, ref := bold+cyan+"multiversectl project uninstall fichero", magenta+"fichero"
	if !strings.Contains(coloured, cmd) || !strings.Contains(coloured, bold+cyan+"git -C /x push") {
		t.Errorf("commands should be in the accent colour: %q", coloured)
	}
	if !strings.Contains(coloured, ref) || strings.Contains(coloured, magenta+"multiversectl") {
		t.Errorf("a name is a reference, not a command: %q", coloured)
	}
	if strings.Contains(coloured, "`") {
		t.Errorf("the markers must not be printed: %q", coloured)
	}
}

func TestOutputMarksUpWhatItPrints(t *testing.T) {
	var b strings.Builder
	o := NewOutput(&b, false)
	o.Info("see `%s`", "multiversectl catalog")
	o.Plain("in `%s`", "/x/y")
	if got := b.String(); got != "› see multiversectl catalog\nin /x/y\n" {
		t.Errorf("got %q", got)
	}
}
