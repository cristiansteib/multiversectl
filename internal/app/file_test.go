package app

import "testing"

// A file the viewer asks for is relative to the checkout and may not climb out
// of it, however it is spelled.
func TestSafeJoinRefusesPathsOutsideTheRepository(t *testing.T) {
	dir := "/work/u3/api"
	// A leading slash does not escape: Join contains it under the repo, where it
	// simply will not exist, rather than letting it reach the real /etc.
	ok := []string{"go.mod", "cmd/main.go", "a/b/../c.txt", "./README.md", "/etc/passwd"}
	for _, f := range ok {
		if _, in := safeJoin(dir, f); !in {
			t.Errorf("safeJoin(%q, %q) = outside, want inside", dir, f)
		}
	}
	out := []string{"../secret", "../../etc/passwd", "a/../../b"}
	for _, f := range out {
		if full, in := safeJoin(dir, f); in {
			t.Errorf("safeJoin(%q, %q) = %q inside, want refused", dir, f, full)
		}
	}
}

func TestIsBinaryDetectsNulBytes(t *testing.T) {
	if isBinary([]byte("# Title\n\nplain text, no nul\n")) {
		t.Error("text with no NUL read as binary")
	}
	if !isBinary([]byte{'P', 'K', 3, 4, 0, 'x'}) {
		t.Error("bytes with a NUL read as text")
	}
}
