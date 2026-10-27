package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// A script that does not parse leaves the page blank on "connecting…", and no
// Go test would notice. Node is on the CI runners; elsewhere this is skipped.
func TestPageScriptParses(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	src, err := uiFiles.ReadFile("ui/app.js")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "app.js")
	if err := os.WriteFile(path, src, 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, "--check", path).CombinedOutput(); err != nil {
		t.Fatalf("app.js does not parse:\n%s", out)
	}
}

// Nothing on the page is set smaller than 1rem: small print was the first thing
// anybody said about it, and one `font: 12px` is all it takes to bring it back.
func TestNoTextIsSmallerThanOneRem(t *testing.T) {
	css, err := uiFiles.ReadFile("ui/style.css")
	if err != nil {
		t.Fatal(err)
	}
	decl := regexp.MustCompile(`(?:^|[;{\s])font(?:-size)?:[^;}]*`)
	size := regexp.MustCompile(`(\d+(?:\.\d+)?)(px|em|rem)\b|\d+%`)
	for _, d := range decl.FindAllString(string(css), -1) {
		for _, m := range size.FindAllStringSubmatch(d, -1) {
			switch {
			case m[2] == "px" && atof(m[1]) < 16, m[2] == "em" && atof(m[1]) < 1, m[2] == "rem" && atof(m[1]) < 1, m[2] == "" && m[0] != "":
				t.Errorf("text smaller than 1rem: %s", strings.TrimSpace(d))
			}
		}
	}
}

func atof(s string) float64 {
	f, _ := strconv.ParseFloat(s, 64)
	return f
}
