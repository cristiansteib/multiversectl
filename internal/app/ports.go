// Package app holds the use cases. It depends on the domain and on the ports
// declared here; never on a concrete adapter.
package app

import (
	"context"
	"io/fs"
	"sort"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// RefKind says where a git ref was found.
type RefKind int

const (
	RefNone RefKind = iota
	RefLocal
	RefRemote
)

// Git is the source-control port. Universes are worktrees of one clone, so
// the implementation must keep that clone as the only object store.
type Git interface {
	Clone(ctx context.Context, url, dest string) error
	Fetch(ctx context.Context, repoDir string) error
	AddWorktree(ctx context.Context, repoDir, dest, branch, base string) error
	RemoveWorktree(ctx context.Context, repoDir, dest string) error
	CurrentBranch(ctx context.Context, dir string) (string, error)
	RefExists(ctx context.Context, repoDir, ref string) (RefKind, error)
	DefaultBranch(ctx context.Context, repoDir string) (string, error)
}

// RuntimeRequest is one invocation of the container runtime.
type RuntimeRequest struct {
	Project  string
	Files    []string
	Profiles []string
	Services []string
	Env      map[string]string
	WorkDir  string
}

// Container is a running unit as the runtime reports it.
type Container struct {
	Name    string
	Service string // the compose service it was started from
	Status  string
	Project string
	Ports   string // every port the runtime reports, exposed ones included
	// Published is only the host port mappings: the ones that would collide
	// with another universe.
	Published string
}

// Volume is one named volume the runtime holds for a project. A universe's
// data lives in volumes named after its project, which is what a wide listing
// shows to prove two universes share nothing.
type Volume struct {
	Name    string
	Project string
	Driver  string
}

// Runtime is the container port.
type Runtime interface {
	Build(ctx context.Context, r RuntimeRequest) error
	Up(ctx context.Context, r RuntimeRequest) error
	Down(ctx context.Context, r RuntimeRequest, removeVolumes bool) error
	Run(ctx context.Context, r RuntimeRequest, service string, args []string) error
	// Exec runs a command inside an already running container, which is what a
	// database shell or a log tail wants; Run would start a fresh one.
	Exec(ctx context.Context, r RuntimeRequest, service string, args []string) error
	// Inspect reports the merged configuration of the given files: the
	// services defined for the active profiles, their labels and whether they
	// build an image. It is what lets a compose file describe itself instead
	// of a manifest repeating it.
	Inspect(ctx context.Context, r RuntimeRequest) (ProjectConfig, error)
	Containers(ctx context.Context, namePrefix string) ([]Container, error)
	// Volumes reports the named volumes whose name starts with the prefix,
	// each tagged with the project that owns it.
	Volumes(ctx context.Context, namePrefix string) ([]Volume, error)
	Version(ctx context.Context) (string, error)
}

// ProjectConfig is the merged view of an app's compose files.
type ProjectConfig struct {
	Services map[string]ServiceConfig
}

// ServiceConfig is one service as the runtime understands it.
type ServiceConfig struct {
	Labels map[string]string
	Build  bool
	Ports  []int // container ports it exposes or publishes
}

// Names returns the service names, sorted.
func (p ProjectConfig) Names() []string {
	out := make([]string, 0, len(p.Services))
	for name := range p.Services {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// ProxyRequest is the shared ingress every universe is reached through.
type ProxyRequest struct {
	Domain      string
	Entrypoints map[string]int
	Network     string
	// Bind is the host address the entrypoints are published on. The loopback
	// address is what keeps universes off the network; it is not the
	// hostnames' job.
	Bind string
}

// ProxyStatus is what Doctor reports about the ingress.
type ProxyStatus struct {
	Running     bool
	Entrypoints map[string]int
	Bind        string // the host address its ports are published on
}

// Proxy is the ingress port.
type Proxy interface {
	EnsureNetwork(ctx context.Context, name string) error
	EnsureUp(ctx context.Context, r ProxyRequest) error
	Down(ctx context.Context) error
	Status(ctx context.Context) (ProxyStatus, error)
}

// Route is one hostname the proxy must send to one service port.
type Route struct {
	Name       string // unique per universe, e.g. "app-u3"
	Hostname   string
	Aliases    []string // further hostnames routed to the same service
	Service    string
	Port       int
	Entrypoint string
	Project    string
	Network    string
}

// Router materialises routing for a universe's apps. Returning extra runtime
// files lets an implementation express routes in the runtime's own terms
// (labels) instead of a side channel the runtime cannot see.
type Router interface {
	Materialize(ctx context.Context, r OverrideRequest) (extraFiles []string, err error)
}

// OverrideRequest is everything the generated compose fragment carries for one
// app of one universe: where it answers, which services must give up their
// host ports, and the values only this universe knows.
type OverrideRequest struct {
	Dir       string
	App       string
	Routes    []Route
	Unpublish []string
	Env       []ServiceEnv
}

// ServiceEnv is the environment one service asked for with labels.
type ServiceEnv struct {
	Service string
	Vars    []EnvVar
}

// EnvVar is one variable written into the generated fragment.
type EnvVar struct {
	Name  string
	Value string
}

// Files is the filesystem port.
type Files interface {
	Exists(path string) bool
	MkdirAll(path string) error
	CopyFile(src, dst string) error
	ReadFile(path string) ([]byte, error)
	WriteFile(path string, data []byte, perm fs.FileMode) error
	RemoveAll(path string) error
	Symlink(target, link string) error
	ListDirs(path string) ([]string, error)
	Rename(oldPath, newPath string) error
	Chmod(path string, perm fs.FileMode) error
	TempFile(pattern string) (string, error)
}

// System is the host port: identity, network facts and tool lookup.
type System interface {
	UID() int
	GID() int
	Getenv(key string) string
	LANIP(ctx context.Context) (string, error)
	Resolve(ctx context.Context, host string) ([]string, error)
	PortFree(ctx context.Context, port int) (bool, error)
	Which(name string) (string, bool)
}

// Release is one published build of this tool.
type Release struct {
	Version string
	Assets  map[string]string // asset name -> download URL
}

// Releases is the self-update port.
type Releases interface {
	Latest(ctx context.Context) (Release, error)
	Download(ctx context.Context, url, dest string) error
}

// Config is where this machine keeps its workspace, so the tool works from
// any directory once bootstrapped.
type Config struct {
	Root     string `yaml:"root"`
	Manifest string `yaml:"manifest"`
}

// ConfigStore persists Config for the current user.
type ConfigStore interface {
	Load() (Config, error)
	Save(c Config) error
	Path() string
}

// Store persists universe state.
type Store interface {
	Save(st domain.UniverseState) error
	Load(id string) (domain.UniverseState, error)
	List() ([]domain.UniverseState, error)
	Delete(id string) error
}

// Output is the presentation port; use cases never write to stdout directly.
type Output interface {
	Info(format string, a ...any)
	OK(format string, a ...any)
	Warn(format string, a ...any)
	Plain(format string, a ...any)
	Table(headers []string, rows [][]string)
}
