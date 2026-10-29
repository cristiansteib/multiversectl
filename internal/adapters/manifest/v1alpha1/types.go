// Package v1alpha1 is the manifest's file format. These types are the single
// source of truth for it: schema/multiverse.v1alpha1.yaml is generated from the
// doc comments and +markers below by `make schema`.
//
// Markers: +default=, +pattern=, +enum=a;b, +min=, +max=, +minItems=,
// +required, +preserveUnknown.
package v1alpha1

import "gopkg.in/yaml.v3"

const (
	APIVersion = "multiverse/v1alpha1"
	Kind       = "Multiverse"
)

// Multiverse is the definition of a set of repositories that run together, and
// that can exist several times on a machine. Each copy is a universe: a git worktree of every
// repository here, one compose project per app, and hostnames of the form
// <host>-<universe>.<domain> served through one shared reverse proxy. A universe
// is numbered, and then its part of a hostname is u3, or named after a branch,
// and then it is fix-login.
type Multiverse struct {
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

	// +enum=Multiverse
	// +required
	Kind string `yaml:"kind"`
}

type ObjectMeta struct {
	// Names the multiverse and prefixes every compose project, so a universe's
	// containers are <name>-<universe>-<app>-*, e.g. acme-u3-api-web-1.
	// +pattern=^[a-z][a-z0-9-]*$
	// +required
	Name string `yaml:"name"`
}

type Spec struct {
	// Where this multiverse's clones and universes live. Lets the manifest be used
	// from anywhere with -f, which is the point of keeping one manifest per
	// multiverse in one directory. A leading ~/ is expanded. Overridden by --root
	// and by $MULTIVERSECTL_ROOT; falls back to the saved config, then to the
	// manifest's own directory.
	Root string `yaml:"root"`

	// Who can reach a universe. "lan" publishes the proxy on every interface
	// and, with domain "auto", builds hostnames under a public wildcard DNS
	// name resolving to this machine, so a phone or a reviewer on the same
	// network can open one. "local" publishes on the loopback address only
	// and, with domain "auto", uses <host>.localhost, which every browser
	// resolves to 127.0.0.1 without asking anyone: nothing about the multiverse
	// leaves the machine, and nothing outside it can connect. Choose "local"
	// unless another device has to open a universe.
	// +enum=lan;local
	// +default=lan
	Expose string `yaml:"expose"`

	// Wildcard domain every hostname is built under. "auto" derives
	// it from expose: <dashed-lan-ip>.traefik.me for "lan", localhost for
	// "local". Set it to a domain you own when you want your own
	// certificates; expose still decides which interface answers. "none" opts
	// out of hostnames altogether: nothing is resolved, and every host is
	// reached at this machine's address on a port of its own (portForward is
	// implied), so a universe is just <ip>:<port>.
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

	// The repositories this multiverse is made of. install clones them; each
	// universe gets a worktree of every one, sharing the clone's object store.
	// Required in a multiverse; a repository's own file (.multiverse.yaml)
	// describes only itself and leaves it out.
	Repos []Repo `yaml:"repos"`

	// The runnable units. A repository whose runtime is "compose" and that no
	// entry here names gets an app of its own name, so most repositories need
	// nothing: the compose files are found by convention, every service they
	// define is started, and the addresses come from their labels. Name a
	// repository here to change any of that, or to split it into several apps.
	Apps []App `yaml:"apps"`

	// Commands around what the whole multiverse does, on the machine: after every
	// app is up, say, to register the universe's address in a service all of them
	// share.
	Hooks Hooks `yaml:"hooks"`

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

	// Clone directory name under the root. Defaults to the repository's own
	// name, as git would clone it (fichero-api for .../fichero-api.git), so it
	// is rarely needed; set it when the directory on disk is called otherwise.
	// A repository with no compose file is still checked out into every
	// universe for editing, and nothing starts for it.
	Dir string `yaml:"dir"`
}

type App struct {
	// Also the compose project suffix, so each app's containers are namespaced
	// separately and two repositories may both call a service "app".
	// +pattern=^[a-z][a-z0-9-]*$
	// +required
	Name string `yaml:"name"`

	// Which repo, by name, holds this app. Leave it out in a repository's own
	// file: the app is that repository's.
	Repo string `yaml:"repo"`

	// Whether `universe up` and `universe start` bring this app up when no app is
	// named. Set it to false for an app that should be checked out and have its
	// addresses but run only when asked for (`universe up 3 <app>`): a tool that
	// needs setup, a code generator, a heavy optional service. Commands that look
	// after what runs (down, rm, status, doctor, ls) still deal with it when it is
	// up, and do not report it as a problem when it is not.
	// +default=true
	Autostart *bool `yaml:"autostart"`

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

	// Where a secret is in neither the clone nor the universe, it is made from
	// the file named here, in the repository, and kept in the clone so every
	// universe copies the same one. By default that is the secret's own name
	// with .example added (.env from .env.example); name another file to use it
	// instead, or "" to never make one, so a missing secret stops the start and
	// says where to put it.
	SecretTemplates map[string]string `yaml:"secretTemplates"`

	// Commands run at points of this app's life in a universe, the way Skaffold
	// runs lifecycle hooks: in one of its services, or on this machine. The tool
	// knows nothing of what they do; it gives them the universe's addresses.
	Hooks Hooks `yaml:"hooks"`

	// Files made from a template and this universe's values before the app
	// starts, for an app that reads a config file and cannot take environment
	// variables. Written on every `universe up` (and by `universe render`), only
	// when the content changed, and kept out of git's sight.
	Render []Render `yaml:"render"`

	// Services allowed to keep their published host ports. Every other
	// service's ports are dropped, because a universe is reached through the
	// proxy and published ports are what make two copies collide. A service can
	// ask for this itself with the label dev.multiversectl.publish: "true".
	Publish []string `yaml:"publish"`

	// The service `universe exec` and `universe run` use when --service is not
	// given. Needed only when the app has several services; with one, that one
	// is used.
	DefaultService string `yaml:"defaultService"`

	// A compose fragment written inline, merged last before the generated
	// routing file. Use it for the few things only this environment knows — a
	// trusted-origins variable, a dev server flag — or as the whole service
	// definition for a repository that has no compose file. It is compose
	// syntax, kept verbatim, and may reference the variables listed at the
	// bottom of this file.
	// +preserveUnknown
	Overlay yaml.Node `yaml:"overlay"`

	// Addresses this app answers on, for a repository you cannot change.
	// Usually leave it out: a service that carries dev.multiversectl.host.<name>
	// labels declares its own addresses, and a service exposing exactly one
	// port is reached at the app's own name with no configuration at all.
	// Declaring hosts here wins over both.
	Hosts []Host `yaml:"hosts"`
}

// Render is one file made from a Go text/template. The template can read the
// universe's name, slug, number and domain, the address of every host (host,
// url, forward, origins, hosts), the environment a hook receives (.Env) and has
// default, required, env and toJson; a value that is missing fails the start
// with the template and the key named, rather than rendering empty.
type Render struct {
	// Where the file goes, relative to the app's repository in the universe. It
	// cannot leave it. The path is added to the checkout's git exclude file, so it
	// does not show as an untracked change.
	// +required
	To string `yaml:"to"`

	// The template, written inline. Give this or from.
	Template string `yaml:"template"`

	// A file holding the template: a plain path is relative to the repository
	// checkout, a path starting with @ to the manifest's directory (as compose
	// does). Give this or template.
	From string `yaml:"from"`

	// The file's permission, as an octal string. Default 0644. Use "0600" for a
	// file that holds a credential.
	// +pattern=^0?[0-7]{3,4}$
	Mode string `yaml:"mode"`

	// Allow the file to replace one that git tracks. Without it that is an
	// error, so a template cannot clobber source; with it, the change shows in
	// git status like any other.
	Overwrite bool `yaml:"overwrite"`
}

// Hooks tie commands to the points of a universe's life. Each event lists
// hooks, run in order; every command gets the universe's name, directory and
// addresses as environment variables (the ones a compose file can use, plus
// MULTIVERSE_EVENT, MULTIVERSE_APP and, on the machine, MULTIVERSE_CONTEXT_FILE:
// a JSON file with the same and more). Under spec, they run on the machine only.
type Hooks struct {
	// Once the universe's worktrees exist, before anything starts. On the machine.
	PostCreate []Hook `yaml:"postCreate"`

	// Before the app is built and started, to prepare what it reads. A failure
	// stops the start unless the hook says otherwise.
	PreUp []Hook `yaml:"preUp"`

	// Once the app's services are up (for spec.hooks, once every app is). A hook
	// in a container is tried again for a few minutes when it fails, because the
	// service may still be starting (a database migrating under it).
	PostUp []Hook `yaml:"postUp"`

	// Before the app's containers are stopped; a command in a service still
	// finds it running.
	PreDown []Hook `yaml:"preDown"`

	// Once they are stopped. On the machine.
	PostDown []Hook `yaml:"postDown"`

	// Before the universe is destroyed, to take back what a hook put somewhere
	// else. On the machine.
	PreRemove []Hook `yaml:"preRemove"`

	// The earlier name of postUp.
	After []Hook `yaml:"after"`
}

// Hook is one command: give it container, to run it in a service, or host, to
// run it on this machine.
type Hook struct {
	// Name says which hook it is in the output.
	Name string `yaml:"name"`

	// Run in a running service, with no terminal. For postUp and preDown.
	Container *ContainerHook `yaml:"container"`

	// Run on this machine, in any event. No shell: to use one, say it.
	Host *HostHook `yaml:"host"`

	// Run on the first start of each universe only, and never again: for what
	// loads data a person may then change, such as the user to log in with.
	// Without it the hook runs after every start, so it should be safe to repeat.
	Once bool `yaml:"once"`

	// What it means when the command does not succeed: warn says so and goes on
	// (the default, so a hook that breaks cannot keep a universe from being
	// stopped or removed), fail ends the command, ignore goes on silently. A
	// preUp hook fails by default, since what it prepares is what the start needs.
	// +enum=warn;fail;ignore
	OnFailure string `yaml:"onFailure"`

	// Keep trying for this long, as a duration such as 30s or 3m, because what
	// the command needs may still be starting. A command in a container after the
	// app is up defaults to 3m; any other to a single try.
	RetryFor string `yaml:"retryFor"`

	// End one try that takes longer, as a duration. By default none does.
	Timeout string `yaml:"timeout"`
}

// ContainerHook is a command run in a running service.
type ContainerHook struct {
	// Compose service to run it in. Leave it out when the app has one service, or
	// names a defaultService.
	Service string `yaml:"service"`

	// The command and its arguments, as compose writes a command, with no shell:
	// ["python", "manage.py", "loaddata"].
	// +required
	Command []string `yaml:"command"`
}

// HostHook is a command run on the machine that runs multiversectl.
type HostHook struct {
	// The command and its arguments, with no shell: ["sh", "-c", "curl ... | jq"]
	// when one is wanted.
	// +required
	Command []string `yaml:"command"`

	// Where it runs, relative to the universe's directory: "api" is that
	// repository's checkout. By default the universe's directory.
	Dir string `yaml:"dir"`
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

	// "auto" also publishes the host on a free host port, so a client that
	// refuses plain http to a hostname (an iPhone app) can call
	// http://<this machine's address>:<port>. The port is chosen once and kept;
	// `universe ls` shows the address as <host>:forward. A host that comes from
	// a compose label asks for the same with dev.multiversectl.host.<name>.portForward.
	// "only" is the same without the hostname: the host is reached by that IP and
	// port alone, and no address shown or handed to a service is a name, for a
	// service that should not be known by one. `domain: none` does this for
	// every host.
	// +enum=auto;only
	PortForward string `yaml:"portForward"`

	// How the proxy talks to the service. Leave it out for plain http; "h2c" is
	// for a service that speaks HTTP/2 without TLS, such as a gRPC one, which
	// otherwise answers the proxy's HTTP/1.1 with an error. A host that comes
	// from a compose label asks for the same with
	// dev.multiversectl.host.<name>.scheme.
	// +enum=http;https;h2c
	Scheme string `yaml:"scheme"`
}

type EnvVar struct {
	// +required
	Name string `yaml:"name"`

	// +required
	Value string `yaml:"value"`
}
