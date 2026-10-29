package domain

import (
	"strings"
	"testing"
)

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
		u := Universe{Name: tc.name, Multiverse: "/ws", Manifest: m}
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

func TestProxyClashes(t *testing.T) {
	web := map[string]int{"web": 80}
	a := &Manifest{Name: "acme", Proxy: Proxy{Network: "multiversenet", Entrypoints: map[string]int{"web": 80, "devapi": 8001}}}
	b := &Manifest{Name: "beta", Proxy: Proxy{Network: "multiversenet", Entrypoints: web}}

	got := strings.Join(ProxyClashes("acme", a, "beta", b), "\n")
	if !strings.Contains(got, `acme needs proxy entrypoint "devapi" (port 8001); starting beta reconfigures`) {
		t.Errorf("a missing entrypoint is the real clash: %q", got)
	}
	if strings.Contains(got, "network") || strings.Contains(got, "(port 80)") {
		t.Errorf("sharing the default network and port 80 is the design, not a clash: %q", got)
	}

	same := &Manifest{Name: "gamma", Proxy: Proxy{Network: "multiversenet", Entrypoints: web}}
	if c := ProxyClashes("beta", b, "gamma", same); len(c) != 0 {
		t.Errorf("two default projects must not warn: %v", c)
	}

	other := &Manifest{Name: "delta", Proxy: Proxy{Network: "edge", Entrypoints: map[string]int{"web": 8080}}}
	got = strings.Join(ProxyClashes("beta", b, "delta", other), "\n")
	for _, want := range []string{"different proxy networks (multiversenet, edge)", `entrypoint "web" is port 80 for beta and 8080 for delta`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}

	local := &Manifest{Name: "eps", Expose: ExposeLocal, Proxy: Proxy{Network: "multiversenet", Entrypoints: web}}
	if c := strings.Join(ProxyClashes("beta", b, "eps", local), "\n"); !strings.Contains(c, "moves it") {
		t.Errorf("a different bind address moves the proxy: %q", c)
	}
}

// An install named like its manifest keeps the project it always had; one
// installed under another name gets its own, so two installs of one manifest
// do not share containers or volumes.
func TestProjectNamesTheInstallWhenItDiffersFromTheManifest(t *testing.T) {
	m := &Manifest{Name: "acme"}
	for _, c := range []struct{ install, want string }{
		{"", "acme-u1"},
		{"acme", "acme-u1"},
		{"acme-staging", "acme-acme-staging-u1"},
		{"Other Team", "acme-other-team-u1"},
	} {
		u := Universe{Name: "1", Manifest: m, Install: c.install}
		if got := u.Project(); got != c.want {
			t.Errorf("install %q: Project() = %q, want %q", c.install, got, c.want)
		}
	}
	u := Universe{Name: "1", Manifest: m, Install: "copy"}
	if got := u.ProjectFor("api"); got != "acme-copy-u1-api" {
		t.Errorf("ProjectFor = %q", got)
	}
}
