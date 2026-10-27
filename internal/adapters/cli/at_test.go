package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestABareAtSaysWhatItNeedsAndHowToChoose(t *testing.T) {
	root, _ := New(Factory{})
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"catalog", "add", "acme/billing", "--at"})
	err := root.Execute()
	if err == nil {
		t.Fatal("a bare --at is an error")
	}
	for _, want := range []string{"--at needs the path of a catalog", "/ for the root", "leave --at out to choose from a list"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error lacks %q: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "flag needs an argument") {
		t.Errorf("the library's own words say nothing useful: %v", err)
	}
}
