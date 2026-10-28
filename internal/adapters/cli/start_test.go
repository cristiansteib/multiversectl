package cli

import (
	"strings"
	"testing"
)

// `start <name> <source>` is the source with the name after it.
func TestStartReadsANameInFrontOfTheSource(t *testing.T) {
	for _, c := range []struct {
		args []string
		want string
		err  string
	}{
		{[]string{"acme/platform"}, "acme/platform", ""},
		{[]string{"billing", "acme/platform"}, "acme/platform:billing", ""},
		{[]string{"billing", "git@example.test:acme/platform.git"}, "git@example.test:acme/platform.git:billing", ""},
		{[]string{"billing", "https://example.test/acme/platform.git"}, "https://example.test/acme/platform.git:billing", ""},
		{[]string{"billing", "./checkout"}, "./checkout:billing", ""},
		{[]string{"acme/billing", "acme/platform"}, "", "is not a name"},
		{[]string{"billing", "acme/platform:search"}, "", "give the name once"},
	} {
		got, err := nameAndSource(c.args)
		if c.err != "" {
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Errorf("%v: error %v, want %q", c.args, err, c.err)
			}
			continue
		}
		if err != nil || strings.Join(got, " ") != c.want {
			t.Errorf("%v: got %v (%v), want %s", c.args, got, err, c.want)
		}
	}
}
