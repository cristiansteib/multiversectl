package domain

import "testing"

// A universe is identified by the name the user typed; everything else about
// its identity is derived from it, and the derivations are what hostnames,
// directories and project names are built from.
func TestUniverseIdentity(t *testing.T) {
	m := &Manifest{Name: "acme"}
	for _, tc := range []struct {
		name, id, slug, dir, project, branch string
		number                               int
	}{
		{name: "3", id: "3", slug: "u3", dir: "/ws/universes/3", project: "acme-u3", branch: "u3", number: 3},
		// "u3" is how a number appears in a hostname, so typing it back means
		// the same universe rather than a new one called "u3".
		{name: "u3", id: "3", slug: "u3", dir: "/ws/universes/3", project: "acme-u3", branch: "u3", number: 3},
		{name: "fix/login", id: "fix-login", slug: "fix-login", dir: "/ws/universes/fix-login",
			project: "acme-fix-login", branch: "fix/login"},
		// The branch keeps the name verbatim; only what DNS and the runtime
		// must accept is reduced.
		{name: "feat/ARCA 123", id: "feat-arca-123", slug: "feat-arca-123",
			dir: "/ws/universes/feat-arca-123", project: "acme-feat-arca-123", branch: "feat/ARCA 123"},
	} {
		u := Universe{Name: tc.name, Workspace: "/ws", Manifest: m}
		if got := u.ID(); got != tc.id {
			t.Errorf("%q: ID = %q, want %q", tc.name, got, tc.id)
		}
		if got := u.Slug(); got != tc.slug {
			t.Errorf("%q: Slug = %q, want %q", tc.name, got, tc.slug)
		}
		if got := u.Dir(); got != tc.dir {
			t.Errorf("%q: Dir = %q, want %q", tc.name, got, tc.dir)
		}
		if got := u.Project(); got != tc.project {
			t.Errorf("%q: Project = %q, want %q", tc.name, got, tc.project)
		}
		if got := u.DefaultBranch(); got != tc.branch {
			t.Errorf("%q: DefaultBranch = %q, want %q", tc.name, got, tc.branch)
		}
		if got := u.Number(); got != tc.number {
			t.Errorf("%q: Number = %d, want %d", tc.name, got, tc.number)
		}
	}
}

func TestUniverseHostname(t *testing.T) {
	m := &Manifest{Name: "acme"}
	if got := (Universe{Name: "fix/login", Manifest: m}).Hostname("api", "example.test"); got != "api-fix-login.example.test" {
		t.Errorf("got %q", got)
	}
	if got := (Universe{Name: "3", Manifest: m}).Hostname("api", "example.test"); got != "api-u3.example.test" {
		t.Errorf("got %q", got)
	}
}

func TestValidateUniverseName(t *testing.T) {
	for _, ok := range []string{"3", "u3", "fix/login", "feat/ARCA-123", "release-2026-01"} {
		if err := ValidateUniverseName(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "   ", "0", "u0", "///", "---"} {
		if err := ValidateUniverseName(bad); err == nil {
			t.Errorf("%q: want an error", bad)
		}
	}
	// A name whose hostname part would not fit in a DNS label is refused with
	// the limit named, rather than silently truncated into a collision.
	long := "feat/" + string(make([]byte, 0)) + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err := ValidateUniverseName(long); err == nil {
		t.Error("want an error for a name too long for a hostname")
	}
}
