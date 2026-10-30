package domain

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Universe is one self-contained instance of a multiverse: a checkout of every
// repository, one compose project per app and one set of hostnames.
//
// A universe is identified by the name the caller gave it — a number, or a
// branch-like name such as "fix/login". Everything else about its identity is
// derived from that name, so there is one place to look and nothing to keep in
// step: ID is where it lives on disk, Slug is what appears in hostnames and
// multiverse names.
type Universe struct {
	Name       string // as typed: "3" or "fix/login"
	Multiverse string // absolute path to the multiverse root
	Manifest   *Manifest
	Branch     map[string]string // repo key -> branch checked out
	// Install is the name this multiverse is installed under on the machine,
	// when it is not the manifest's own (`start --as`). Two installs of one
	// manifest would otherwise give the same universe the same compose project.
	Install string
	// Recorded is the project the universe was created with, as its state
	// keeps it. Once a universe exists its project never changes, whatever
	// the install is called now: its containers and volumes carry that name.
	Recorded string
}

var (
	notSlug   = regexp.MustCompile(`[^a-z0-9]+`)
	numberRe  = regexp.MustCompile(`^u?([0-9]+)$`)
	slugLimit = 40 // keeps <host>-<slug> inside a 63-character DNS label
)

// Slugify reduces a name to what DNS labels and compose project names both
// accept: lowercase letters, digits and single dashes. "fix/My Login" becomes
// "fix-my-login".
func Slugify(name string) string {
	return strings.Trim(notSlug.ReplaceAllString(strings.ToLower(name), "-"), "-")
}

// UniverseNumber is the number a name denotes, or 0 when it names something
// else. "3" and "u3" are the same universe, since one is how it is written in
// a hostname and the other is how it is typed.
func UniverseNumber(name string) int {
	m := numberRe.FindStringSubmatch(strings.ToLower(strings.TrimSpace(name)))
	if m == nil {
		return 0
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0
	}
	return n
}

// CanonicalUniverseName is the name a universe is stored under: a number in
// its bare form, anything else slugified. It is what the caller typed, made
// unambiguous, so "u3", "3" and "03" are one universe and "Fix/Login" and
// "fix-login" are another.
func CanonicalUniverseName(name string) string {
	if n := UniverseNumber(name); n > 0 {
		return strconv.Itoa(n)
	}
	return Slugify(name)
}

// ValidateUniverseName reports why a name cannot be used, naming the limit it
// breaks rather than silently changing it.
func ValidateUniverseName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return fmt.Errorf("universe name: required — a number, or a branch name like fix/login")
	}
	if numberRe.MatchString(strings.ToLower(trimmed)) {
		if UniverseNumber(trimmed) <= 0 {
			return fmt.Errorf("universe %q: a numbered universe must be positive", name)
		}
		return nil
	}
	slug := Slugify(trimmed)
	switch {
	case slug == "":
		return fmt.Errorf("universe %q: nothing is left of it once reduced to letters, digits and dashes", name)
	case len(slug) > slugLimit:
		return fmt.Errorf("universe %q: its hostname part %q is %d characters, over the %d a hostname has room for — give it a shorter name",
			name, slug, len(slug), slugLimit)
	}
	return nil
}

// Number is the universe's number, or 0 when it is named rather than numbered.
func (u Universe) Number() int { return UniverseNumber(u.Name) }

// ID is the universe's directory under universes/ and the key its state is
// stored under. A numbered universe keeps its bare number, which is where it
// has lived since before universes could be named.
func (u Universe) ID() string { return CanonicalUniverseName(u.Name) }

// Slug is the per-universe part of hostnames and multiverse names. A number
// carries a "u" so that a hostname reads api-u3 rather than api-3, which
// would look like a port or a version.
func (u Universe) Slug() string {
	if n := u.Number(); n > 0 {
		return fmt.Sprintf("u%d", n)
	}
	return Slugify(u.Name)
}

// Multiverse is the compose project name; it namespaces containers,
// networks and volumes, which is what keeps universes from colliding.
func (u Universe) Project() string {
	if u.Recorded != "" {
		return u.Recorded
	}
	name := u.Manifest.Name
	// The default case, an install named like its manifest, keeps the name it
	// has always had so that existing installs keep their projects.
	if inst := Slugify(u.Install); inst != "" && inst != name {
		name += "-" + inst
	}
	return fmt.Sprintf("%s-%s", name, u.Slug())
}

// ProxyNetwork is the network the proxy reaches an app's routed services on: the
// app's own default network, which compose makes for its project. It is private to
// that app of that universe, so a service name on it means one service. A network
// shared by several universes would not: every service answers to its plain name
// there, and "auth" would be the auth service of each universe at once.
//
// It is the name before the compose files are read; they have the last word, since
// a compose file that names its own networks has no default network at all.
func (u Universe) ProxyNetwork(appKey string) string { return u.ProjectFor(appKey) + "_default" }

// ProjectFor is the compose project of one app. Apps of a universe get a
// multiverse each: two repositories may both call a service "app", and inside one
// multiverse that is the same container.
func (u Universe) ProjectFor(appKey string) string {
	return fmt.Sprintf("%s-%s", u.Project(), appKey)
}

// Dir is the universe's directory, holding one checkout per repository.
func (u Universe) Dir() string {
	return filepath.Join(u.Multiverse, "universes", u.ID())
}

// RepoDir is where a repository is checked out inside the universe.
func (u Universe) RepoDir(repoKey string) string { return filepath.Join(u.Dir(), repoKey) }

// Hostname renders a host rule's name, e.g. "app-u3.example.test".
func (u Universe) Hostname(hostKey, domain string) string {
	return fmt.Sprintf("%s-%s.%s", hostKey, u.Slug(), domain)
}

// Hostnames maps every host rule of every app to its hostname.
func (u Universe) Hostnames(domain string) map[string]string {
	out := map[string]string{}
	for _, app := range u.Manifest.Apps {
		for hostKey := range app.Hosts {
			out[hostKey] = u.Hostname(hostKey, domain)
		}
	}
	return out
}

// EnvVarName is the variable an overlay uses to reference a hostname:
// host key "app" is passed to the runtime as APP_HOST.
func EnvVarName(hostKey string) string { return envPrefix(hostKey) + "_HOST" }

// EnvVarURL is the matching variable holding the whole address, port and all:
// host key "app" becomes APP_URL. A framework that wants an origin rather than
// a hostname can then take it as it is.
func EnvVarURL(hostKey string) string { return envPrefix(hostKey) + "_URL" }

// EnvVarForwardURL holds the address a host can also be reached at by IP and
// port, when it is forwarded: APP_FORWARD_URL. A client that cannot use the
// hostname (a phone refusing plain http to a name) is given this one.
func EnvVarForwardURL(hostKey string) string { return envPrefix(hostKey) + "_FORWARD_URL" }

func envPrefix(hostKey string) string {
	return strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(hostKey))
}

// DefaultBranch is the branch a universe's worktrees are checked out on when
// the caller names none. A universe named after a branch is that branch —
// which is the point of naming it so — and a numbered one follows the
// multiverse's own template.
func (u Universe) DefaultBranch() string {
	if n := u.Number(); n > 0 {
		if u.Manifest == nil {
			return u.Slug()
		}
		return u.Manifest.DefaultBranchFor(n)
	}
	return u.Name
}
