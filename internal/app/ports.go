// Package app holds the use cases. It depends on the domain and on the ports
// declared here; never on a concrete adapter.
package app

import (
	"context"
	"errors"
	"io/fs"
	"sort"
	"time"

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
	// Checkout detaches HEAD at ref, a branch (as of origin), tag or commit.
	Checkout(ctx context.Context, repoDir, ref string) error

	// The rest is what writing into a catalog repository needs.
	Init(ctx context.Context, dir string) error
	// Toplevel fails when dir is not inside a work tree.
	Toplevel(ctx context.Context, dir string) (string, error)
	IsClean(ctx context.Context, dir string) (bool, error)
	HasRemote(ctx context.Context, dir string) bool
	// RemoteURL is where origin points, which is how a local checkout is
	// named in a catalog other people read.
	RemoteURL(ctx context.Context, dir string) (string, error)
	// Commit records only paths, so unrelated changes in a user's own
	// repository are never swept into it.
	Commit(ctx context.Context, dir, message string, paths ...string) error
	// Push sends HEAD to the named branch of origin.
	Push(ctx context.Context, dir, branch string) error
	// Behind counts the commits origin has, as last fetched, that dir's HEAD
	// lacks; ref is a branch, empty for the default one.
	Behind(ctx context.Context, dir, ref string) (int, error)
	// FastForward moves dir's branch to origin's, and refuses anything else.
	FastForward(ctx context.Context, dir, branch string) error
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

// Origin is where a workspace's manifest came from. It is recorded at install
// so update and status can tell when the manifest or the catalog has moved on.
type Origin struct {
	Repo string `yaml:"repo,omitempty"`
	Ref  string `yaml:"ref,omitempty"`
	Path string `yaml:"path,omitempty"`
}

// WorkspaceEntry is one workspace this machine knows how to reach.
type WorkspaceEntry struct {
	Root     string `yaml:"root"`
	Manifest string `yaml:"manifest"`

	// From is the catalog path it was installed from, empty when it was
	// installed from a repository or file directly.
	From   string `yaml:"from,omitempty"`
	Source Origin `yaml:"source,omitempty"`
}

// Config is where this machine keeps its workspaces, so the tool works from
// any directory once bootstrapped. Several can coexist; Current is the one
// commands use when no flag says otherwise.
type Config struct {
	Current    string                    `yaml:"current,omitempty"`
	Workspaces map[string]WorkspaceEntry `yaml:"workspaces,omitempty"`

	// Catalog is where this machine's root catalog lives: a git URL or a
	// local file. Empty until one is chosen.
	Catalog string `yaml:"catalog,omitempty"`

	// Root and Manifest are what releases before multiple workspaces wrote.
	// Normalize folds them into Workspaces; nothing writes them any more.
	Root     string `yaml:"root,omitempty"`
	Manifest string `yaml:"manifest,omitempty"`
}

// ConfigStore persists Config for the current user.
type ConfigStore interface {
	Load() (Config, error)
	Save(c Config) error
	Path() string
}

// ErrNotInteractive is what a Prompter returns when input is not a terminal:
// the caller should have been given the answer as a flag.
var ErrNotInteractive = errors.New("an answer is needed but input is not a terminal; pass it as a flag")

// Prompter asks the user a question. Every use case that prompts also takes the
// answer as a flag, so scripts never reach it.
type Prompter interface {
	Select(label string, options []Option, def int) (int, error)
	// MultiSelect returns no indexes when the user presses Enter alone: that
	// is how they back out.
	MultiSelect(label string, options []Option) ([]int, error)
	Confirm(label string, def bool) (bool, error)
	Input(label, def string) (string, error)
}

// Option is one line of a menu. A Disabled one is shown, with its Note saying
// why, but cannot be chosen: hiding it would leave the user wondering where it
// went.
type Option struct {
	Label    string
	Note     string
	Disabled bool
}

// RepoHost creates repositories on a hosting service.
type RepoHost interface {
	// Available says why repositories cannot be created, nil when they can.
	Available(ctx context.Context) error
	Create(ctx context.Context, name string, private bool) (url string, err error)
}

// CatalogEditor changes catalog files while keeping the comments and layout
// a person wrote, which a decode and re-encode of the domain model would lose.
type CatalogEditor interface {
	Scaffold(name string) []byte
	AddCatalogRef(data []byte, ref domain.CatalogRef) ([]byte, error)
	AddWorkspaceRef(data []byte, ref domain.WorkspaceRef) ([]byte, error)
	RemoveRef(data []byte, name string) ([]byte, error)
	// WorkspaceName reads metadata.name from a multiverse.yaml.
	WorkspaceName(data []byte) (string, error)
	// WorkspaceRoot reads spec.root, which is a path on one machine.
	WorkspaceRoot(data []byte) string
}

// ManifestLoader reads a workspace manifest from disk, so a use case can look
// at workspaces other than the one it was built for.
type ManifestLoader interface {
	Load(path string) (*domain.Manifest, error)
}

// CatalogDecoder turns a catalog file into the domain model; the app stays
// ignorant of the file format.
type CatalogDecoder interface {
	Decode(data []byte) (*domain.Catalog, error)
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

// ServerProcess runs the web view as a background process of its own. The web
// view is a separate program (multiversectl-server); this is how the command
// line starts and stops it without containing it.
type ServerProcess interface {
	// Start launches it detached and returns once it answers. A server that is
	// already running is returned as it is, with ErrServerRunning.
	Start(ctx context.Context, o ServerStart) (ServerInfo, error)
	// Stop ends the server this port started, ErrServerNotRunning when none is.
	Stop(ctx context.Context) (ServerInfo, error)
	Status(ctx context.Context) (ServerInfo, error)
}

// ServerStart says where the server listens.
type ServerStart struct {
	Addr   string
	Expose bool
}

// ServerInfo is a running server.
type ServerInfo struct {
	PID     int
	URL     string
	Started time.Time
	Log     string
}

var (
	ErrServerRunning    = errors.New("the server is already running")
	ErrServerNotRunning = errors.New("the server is not running")
)
