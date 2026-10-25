// Package v1alpha1 is the manifest's file format. These types are the single
// source of truth for it: schema/project.v1alpha1.yaml is generated from the
// doc comments and +markers below by `make schema`.
//
// Markers: +default=, +pattern=, +enum=a;b, +min=, +max=, +minItems=,
// +required, +preserveUnknown.
package v1alpha1

import "gopkg.in/yaml.v3"

const (
	APIVersion = "multiverse/v1alpha1"
	Kind       = "Project"
	// LegacyKind is what a project's file said before it was called one; it is
	// still read, and never written.
	LegacyKind = "Workspace"
)

// Project is a set of repositories that run together, and that can exist several
// times on a machine. Each copy is a universe: a git worktree of every
// repository here, one compose project per app, and hostnames of the form
// <host>-<universe>.<domain> served through one shared reverse proxy. A universe
// is numbered, and then its part of a hostname is u3, or named after a branch,
// and then it is fix-login.
type Project struct {
	TypeMeta `yaml:",inline"`

	// +required
	ObjectMeta ObjectMeta `yaml:"metadata"`

	// +required
	Spec Spec `yaml:"spec"`
}

type TypeMeta struct {
	// The only version this build understands.
	// +enum=multiverse/v1alpha1
	// +required
	APIVersion string `yaml:"apiVersion"`

	// +enum=Project
	// +required
	Kind string `yaml:"kind"`
}

type ObjectMeta struct {
	// Names the project and prefixes every compose project, so a universe's
	// containers are <name>-<universe>-<app>-*, e.g. acme-u3-api-web-1.
	// +pattern=^[a-z][a-z0-9-]*$
	// +required
	Name string `yaml:"name"`
}

type Spec struct {
	// Where this project's clones and universes live. Lets the manifest be used
	// from anywhere with -f, which is the point of keeping one manifest per
	// project in one directory. A leading ~/ is expanded. Overridden by --root
	// and by $MULTIVERSECTL_ROOT; falls back to the saved config, then to the
	// manifest's own directory.
	Root string `yaml:"root"`

	// Who can reach a universe. "lan" publishes the proxy on every interface
	// and, with domain "auto", builds hostnames under a public wildcard DNS
	// name resolving to this machine, so a phone or a reviewer on the same
	// network can open one. "local" publishes on the loopback address only
	// and, with domain "auto", uses <host>.localhost, which every browser
	// resolves to 127.0.0.1 without asking anyone: nothing about the project
	// leaves the machine, and nothing outside it can connect. Choose "local"
	// unless another device has to open a universe.
	// +enum=lan;local
	// +default=lan
	Expose string `yaml:"expose"`

	// Wildcard domain every hostname is built under. "auto" derives
	// it from expose: <dashed-lan-ip>.traefik.me for "lan", localhost for
	// "local". Set it to a domain you own when you want your own
	// certificates; expose still decides which interface answers.
	// +default=auto
	Domain string `yaml:"domain"`

	// Branch each universe's worktrees are checked out on. {{universe}}
	// expands to the number, so "u{{universe}}" gives universe 3 the branch
	// u3. Set it to follow your own convention — "dev/u{{universe}}", or a
	// fixed name every universe shares. `new --branch` overrides it.
	// +default=u{{universe}}
	Branch string `yaml:"branch"`

	// The shared ingress every universe is reached through.
	Proxy Proxy `yaml:"proxy"`

	// The repositories this project is made of. install clones them; each
	// universe gets a worktree of every one, sharing the clone's object store.
	// +minItems=1
	// +required
	Repos []Repo `yaml:"repos"`

	// The runnable units. A repository whose runtime is "compose" and that no
	// entry here names gets an app of its own name, so most repositories need
	// nothing: the compose files are found by convention, every service they
	// define is started, and the addresses come from their labels. Name a
	// repository here to change any of that, or to split it into several apps.
	Apps []App `yaml:"apps"`

	// Variables exported into every runtime invocation. {{uid}} and {{gid}}
	// expand to the current user, ${VAR} to the environment. HOST_UID and
	// HOST_GID are always provided; an entry here overrides them.
	Env []EnvVar `yaml:"env"`
}

type Proxy struct {
	// +default=traefik
	// +enum=traefik
	Kind string `yaml:"kind"`

	// External container network the proxy and every routed service join.
	// Created if missing.
	// +default=multiversenet
	Network string `yaml:"network"`

	// Ports the proxy listens on, named. Host rules pick one by name; "web" is
	// the default. A second entrypoint is how a browser application that
	// expects its API on a fixed port keeps working: the proxy answers there
	// too and routes by Host header, so every universe shares the one port.
	Entrypoints []Entrypoint `yaml:"entrypoints"`
}

type Entrypoint struct {
	// +pattern=^[a-z][a-z0-9-]*$
	// +required
	Name string `yaml:"name"`

	// +min=1
	// +max=65535
	// +required
	Port int `yaml:"port"`
}

type Repo struct {
	// Key used by apps, by the universe's subdirectory and by the <NAME>_DIR
	// variable passed to the runtime.
	// +pattern=^[a-z][a-z0-9-]*$
	// +required
	Name string `yaml:"name"`

	// Clone URL.
	// +required
	URL string `yaml:"url"`

	// Clone directory name under the root. Defaults to the repository's name;
	// set it when the directory on disk does not match the key you want.
	Dir string `yaml:"dir"`

	// "none" for a repository with no services — it is still checked out into
	// every universe for editing, but nothing starts.
	// +default=compose
	// +enum=compose;none
	Runtime string `yaml:"runtime"`
}

type App struct {
	// Also the compose project suffix, so each app's containers are namespaced
	// separately and two repositories may both call a service "app".
	// +pattern=^[a-z][a-z0-9-]*$
	// +required
	Name string `yaml:"name"`

	// Which repo, by name, holds this app.
	// +required
	Repo string `yaml:"repo"`

	// Build images before starting. Inferred when a service has a build
	// section, so it is rarely needed.
	Build bool `yaml:"build"`

	// Runtime files, in merge order. A plain path is relative to the repository
	// checkout; a path starting with @ is relative to the manifest's directory.
	// When omitted, two conventional places are tried, whichever exist:
	// docker-compose.yml in the repository, and overlays/<app>.yml or
	// <manifest-name>/<app>.yml next to the manifest.
	Compose []string `yaml:"compose"`

	// Compose profiles always active for this app.
	Profiles []string `yaml:"profiles"`

	// Services to start. Omit it to start every service the compose files
	// define for the active profiles, which is what `docker compose up` would
	// do. Name them only when a file also holds services a universe should not
	// run.
	Services []string `yaml:"services"`

	// Named groups of extra services, started on demand with --with <name>.
	// The name is also activated as a compose profile.
	Optional []Optional `yaml:"optional"`

	// Files that are deliberately not in git — credentials, service accounts —
	// copied from the clone into each universe's checkout. doctor fails when
	// one is missing, rather than letting the universe half-start.
	Secrets []string `yaml:"secrets"`

	// Services allowed to keep their published host ports. Every other
	// service's ports are dropped, because a universe is reached through the
	// proxy and published ports are what make two copies collide. A service can
	// ask for this itself with the label multiverse.publish: "true".
	Publish []string `yaml:"publish"`

	// A compose fragment written inline, merged last before the generated
	// routing file. Use it for the few things only this environment knows — a
	// trusted-origins variable, a dev server flag — or as the whole service
	// definition for a repository that has no compose file. It is compose
	// syntax, kept verbatim, and may reference the variables listed at the
	// bottom of this file.
	// +preserveUnknown
	Overlay yaml.Node `yaml:"overlay"`

	// Addresses this app answers on, for a repository you cannot change.
	// Usually leave it out: a service that carries multiverse.host.<name>
	// labels declares its own addresses, and a service exposing exactly one
	// port is reached at the app's own name with no configuration at all.
	// Declaring hosts here wins over both.
	Hosts []Host `yaml:"hosts"`
}

type Optional struct {
	// +required
	Name string `yaml:"name"`

	// +required
	Services []string `yaml:"services"`
}

type Host struct {
	// Host key. The hostname becomes <name>-<universe>.<domain>, and the
	// variables <NAME>_HOST and <NAME>_URL are passed to the runtime.
	// +pattern=^[a-z][a-z0-9-]*$
	// +required
	Name string `yaml:"name"`

	// Compose service that serves it.
	// +required
	Service string `yaml:"service"`

	// Port inside the container.
	// +required
	Port int `yaml:"port"`

	// Proxy entrypoint, by name.
	// +default=web
	Entrypoint string `yaml:"entrypoint"`

	// Other host keys routed to this same service. Used when one service must
	// answer on a second entrypoint under the front-ends' hostnames.
	AlsoMatch []string `yaml:"alsoMatch"`
}

type EnvVar struct {
	// +required
	Name string `yaml:"name"`

	// +required
	Value string `yaml:"value"`
}
