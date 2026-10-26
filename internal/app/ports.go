// Package app holds the use cases. It depends on the domain and on the ports
// declared here; never on a concrete adapter.
package app

import (
	"context"
	"errors"
	"io"
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
	// RemoveWorktree takes a worktree away. discard says whether to throw away
	// work that is only in it: git refuses otherwise, and whether that refusal
	// is right is a question for the use case, not for this port.
	RemoveWorktree(ctx context.Context, repoDir, dest string, discard bool) error
	CurrentBranch(ctx context.Context, dir string) (string, error)
	RefExists(ctx context.Context, repoDir, ref string) (RefKind, error)
	DefaultBranch(ctx context.Context, repoDir string) (string, error)
	// Branches lists a repository's branches by short name: its own, and those
	// origin had when last fetched.
	Branches(ctx context.Context, repoDir string) ([]string, error)
	// CheckedOutAt is the worktree a branch is checked out in, "" when none is.
	// Git allows a branch in one worktree only.
	CheckedOutAt(ctx context.Context, repoDir, branch string) (string, error)
	// AddWorktreeDetached checks ref out without a branch of its own.
	AddWorktreeDetached(ctx context.Context, repoDir, dest, ref string) error
	// DeleteBranchIfSafe removes a local branch only when nothing is lost by it:
	// every commit is already on origin's base branch or on the branch's own
	// remote counterpart. It says whether it did, and why not when it did not.
	DeleteBranchIfSafe(ctx context.Context, repoDir, branch, base string) (deleted bool, why string, err error)
	// Unpushed counts the commits on local branches that are on no remote: work
	// that exists only in this clone.
	Unpushed(ctx context.Context, repoDir string) (int, error)
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
	// AddRemote points a new repository's origin at url.
	AddRemote(ctx context.Context, dir, name, url string) error
	// Push sends HEAD to the named branch of origin.
	Push(ctx context.Context, dir, branch string) error
	// Behind counts the commits origin has, as last fetched, that dir's HEAD
	// lacks; ref is a branch, empty for the default one.
	Behind(ctx context.Context, dir, ref string) (int, error)
	// Ahead counts the commits dir's HEAD has that origin, as last fetched, lacks;
	// ref is a branch, empty for the default one.
	Ahead(ctx context.Context, dir, ref string) (int, error)
	// LastFetch is when dir last fetched from origin, the zero time if never.
	LastFetch(ctx context.Context, dir string) time.Time
	// FastForward moves dir's branch to origin's, and refuses anything else.
	FastForward(ctx context.Context, dir, branch string) error
	// Merge brings origin's branch into dir's HEAD with a merge commit. A merge
	// that conflicts is abandoned, leaving dir as it was.
	Merge(ctx context.Context, dir, branch string) error
	// DiffStat measures how much has changed. With a ref it is the work on
	// dir's HEAD that origin's branch lacks, counted from where the two
	// parted; with none it is what is edited in dir and not committed.
	DiffStat(ctx context.Context, dir, ref string) (DiffStat, error)
}

// DiffStat is a change measured in files and lines, as `git diff --stat` says
// it: what a branch amounts to, where Ahead only says how many commits.
type DiffStat struct {
	Files   int
	Added   int
	Removed int
}

// Empty says nothing changed.
func (d DiffStat) Empty() bool { return d.Files == 0 && d.Added == 0 && d.Removed == 0 }

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

// ContainerStats is one reading of a container's resource use, as the runtime
// reports it: CPU and Memory are percentages, MemUsage is "used / limit", and
// MemBytes and MemLimit are that pair in bytes, so it can be totalled and
// plotted rather than only printed.
type ContainerStats struct {
	Name     string
	CPU      float64
	Memory   float64
	MemUsage string
	MemBytes float64
	MemLimit float64
	NetIO    string
	PIDs     int
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
	// Restart restarts one service in place, reporting to the runtime's output.
	Restart(ctx context.Context, r RuntimeRequest, service string) error
	// Logs writes the last tail lines of one container to w, and with follow
	// keeps writing as it logs until ctx ends. Both streams arrive on w.
	Logs(ctx context.Context, container string, tail int, follow bool, w io.Writer) error
	// Stats takes one reading of the named containers' resource use.
	Stats(ctx context.Context, containers []string) ([]ContainerStats, error)
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
	// HostPort, when set, is the host port the service is also published on
	// (bound to OverrideRequest.Bind). ForwardURL is the address that gives.
	HostPort   int
	ForwardURL string
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
	// Bind is the address forwarded host ports are published on.
	Bind string
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
	// Size is the bytes a file or directory takes, links not followed.
	Size(path string) (int64, error)
}

// System is the host port: identity, network facts and tool lookup.
type System interface {
	UID() int
	GID() int
	Getenv(key string) string
	LANIP(ctx context.Context) (string, error)
	Resolve(ctx context.Context, host string) ([]string, error)
	PortFree(ctx context.Context, port int) (bool, error)
	// FreePort asks the kernel for a port nothing is listening on.
	FreePort(ctx context.Context) (int, error)
	// Probe asks url once and returns the HTTP status it answered with, without
	// following redirects: any answer means something is listening.
	Probe(ctx context.Context, url string) (int, error)
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
	// Download writes the URL to dest and returns the SHA-256 of what it
	// wrote, in lower-case hex. The digest is taken from the bytes as they
	// land, so it describes the file that is there and not a second read of
	// something that may have changed.
	Download(ctx context.Context, url, dest string) (sum string, err error)
}

// Origin is where a project's manifest came from. It is recorded at install
// so update and status can tell when the manifest or the catalog has moved on.
type Origin struct {
	Repo string `yaml:"repo,omitempty"`
	Ref  string `yaml:"ref,omitempty"`
	Path string `yaml:"path,omitempty"`
}

// WorkspaceEntry is one project this machine knows how to reach.
type WorkspaceEntry struct {
	Root     string `yaml:"root"`
	Manifest string `yaml:"manifest"`

	// From is the catalog path it was installed from, empty when it was
	// installed from a repository or file directly.
	From   string `yaml:"from,omitempty"`
	Source Origin `yaml:"source,omitempty"`
}

// Config is where this machine keeps its projects, so the tool works from
// any directory once bootstrapped. Several can coexist; Current is the one
// commands use when no flag says otherwise.
type Config struct {
	Current    string                    `yaml:"current,omitempty"`
	Workspaces map[string]WorkspaceEntry `yaml:"projects,omitempty"`
	// LegacyWorkspaces is where a config written before projects were called
	// that kept them. Normalize folds it into Projects; nothing writes it.
	LegacyWorkspaces map[string]WorkspaceEntry `yaml:"workspaces,omitempty"`

	// Catalog is where this machine's root catalog lives: a git URL or a
	// local file. Empty until one is chosen.
	Catalog string `yaml:"catalog,omitempty"`

	// Root and Manifest are what releases before multiple projects wrote.
	// Normalize folds them into Projects; nothing writes them any more.
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
	// Checked starts a MultiSelect with the line ticked.
	Checked bool
}

// RepoHost creates repositories on a hosting service.
type RepoHost interface {
	// Name is the service's own name, such as GitHub.
	Name() string
	// Available says why the service cannot be used, nil when it can. A
	// *HostUnavailable says whether signing in would fix it.
	Available(ctx context.Context) error
	// Create makes a repository; name is owner/name, or just a name for the
	// signed-in account's own.
	Create(ctx context.Context, name string, private bool) (url string, err error)
	// Login signs in, handing the terminal to the service's own tool, which
	// opens a browser or shows a code. It returns when that is done.
	Login(ctx context.Context) error
	// Accounts are the places a repository can be made or listed: the signed-in
	// user first, then the organizations or groups they belong to.
	Accounts(ctx context.Context) ([]Account, error)
	// Repos lists an account's repositories, for searching through them.
	Repos(ctx context.Context, account string) ([]RemoteRepo, error)
	// ReposWithTopic finds, across every account the user can see, the
	// repositories carrying a topic. It is how a new machine finds a root.
	ReposWithTopic(ctx context.Context, topic string) ([]RemoteRepo, error)
	// AddTopic puts a topic on a repository, named by its URL.
	AddTopic(ctx context.Context, repoURL, topic string) error
	// Owns says whether a repository URL is on this service.
	Owns(repoURL string) bool
	// OpenReview is the open pull or merge request whose source is branch, nil
	// when there is none. It only reads: it opens nothing and signs nobody in,
	// and a service that cannot be reached is an error, not an absence.
	OpenReview(ctx context.Context, repoURL, branch string) (*Review, error)
}

// Review is a pull request on GitHub or a merge request on GitLab: the one
// thing about a universe's branch that is neither in the checkout nor in the
// clone, and the thing people most want to know about it.
type Review struct {
	// Number is as the service counts them, drawn as #12 or !12.
	Number int
	Title  string
	URL    string
	// Draft says it is not asking to be merged yet.
	Draft bool
	// Author is who opened it, Updated when it last moved.
	Author  string
	Updated time.Time
}

// Account is a user, organization or group on a hosting service.
type Account struct {
	Name string
	Org  bool
}

// RemoteRepo is a repository on a hosting service.
type RemoteRepo struct {
	Name        string // owner/name
	URL         string
	Description string
	Private     bool
}

// ProjectDraft is what a new project's manifest is written from.
type ProjectDraft struct {
	Name  string
	Repos []DraftRepo
}

// DraftRepo is one repository of the project; Dir is where it sits under the
// project root. A repository runs from its own docker-compose.yml unless
// NoRuntime says it has none, which is what a library or a mobile app wants.
type DraftRepo struct {
	Key, URL, Dir string
	NoRuntime     bool
}

// ManifestWriter turns a draft into a multiverse.yaml.
type ManifestWriter interface {
	Workspace(d ProjectDraft) ([]byte, error)
	// Agents is a short AGENTS.md for the project's repository: what it is and
	// how to work with it and with the tool. source is how someone installs
	// the project, the repository's URL or, before it has one, a path.
	Agents(d ProjectDraft, source string) ([]byte, error)
}

// CatalogEditor changes catalog files while keeping the comments and layout
// a person wrote, which a decode and re-encode of the domain model would lose.
type CatalogEditor interface {
	Scaffold(name string) []byte
	// ScaffoldRoot is Scaffold for the catalog a machine starts from, whose kind
	// is CatalogRoot.
	ScaffoldRoot(name string) []byte
	// MarkRoot changes a Catalog into a CatalogRoot, and nothing else about it.
	MarkRoot(data []byte) ([]byte, error)
	AddCatalogRef(data []byte, ref domain.CatalogRef) ([]byte, error)
	AddWorkspaceRef(data []byte, ref domain.WorkspaceRef) ([]byte, error)
	RemoveRef(data []byte, name string) ([]byte, error)
	// WorkspaceName reads metadata.name from a multiverse.yaml.
	WorkspaceName(data []byte) (string, error)
	// WorkspaceRoot reads spec.root, which is a path on one machine.
	WorkspaceRoot(data []byte) string
}

// ManifestLoader reads a project manifest from disk, so a use case can look
// at projects other than the one it was built for.
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
	// Restart stops the running server and starts it again with the options it
	// had, so a new build is the one that runs. ErrServerNotRunning when none is.
	Restart(ctx context.Context) (ServerInfo, error)
	// Pair asks the running server for a code that lets another machine in
	// without the link. ErrServerNotRunning when none is.
	Pair(ctx context.Context) (ServerInvite, error)
}

// ServerInvite is a way into the running web view for a machine that cannot be
// handed the link: a short code, said out loud or typed, and where to type it.
type ServerInvite struct {
	Code    string
	Expires time.Time
	// At is where another machine on this network would open it, best first.
	// Empty, or loopback only, when the server was not started with --expose.
	At []string
	// Exposed says whether the server answers anywhere but this machine.
	Exposed bool
}

// ServerStart says where the server listens.
type ServerStart struct {
	Addr   string
	Expose bool
	// ReadOnly starts the web view without any action.
	ReadOnly bool
}

// ServerInfo is a running server.
type ServerInfo struct {
	PID     int
	URL     string
	Started time.Time
	Log     string
	// Outdated is when the program on disk was replaced after this one started,
	// so what is running is an older build than what is installed.
	Outdated bool
}

var (
	ErrServerRunning    = errors.New("the server is already running")
	ErrServerNotRunning = errors.New("the server is not running")
)

// HostUnavailable is why a hosting service cannot be used right now. CanSignIn
// says the tool is there and only the person is missing, which Login fixes;
// otherwise something has to be installed first.
type HostUnavailable struct {
	Reason    string
	CanSignIn bool
}

func (e *HostUnavailable) Error() string { return e.Reason }

// Progress shows that something slow is happening and ends with it, leaving
// nothing behind. Without a terminal it just runs fn.
type Progress interface {
	Do(label string, fn func(ctx context.Context) error) error
	// Parallel runs the jobs, at most limit at a time, drawing one line for each
	// (waiting, running, done, failed). It returns one error per job, in order.
	// A job must not print.
	Parallel(label string, limit int, jobs []Job) []error
}

// Job is one named piece of work for Progress.Parallel.
type Job struct {
	Name string
	Run  func(ctx context.Context) error
}

// Locks keeps two processes out of each other's way: the command line and the
// web view are separate programs acting on the same universes, and nothing in
// one can see what the other is in the middle of.
//
// A lock is released when it is let go, and by the operating system when the
// process that holds it ends, however it ends — there is no stale lock to
// clean up after a kill or a crash.
type Locks interface {
	// Acquire takes the lock that path names, waiting up to wait for whoever
	// holds it. It returns ErrBusy, naming the holder when it can say, rather
	// than waiting forever: somebody watching a command wants to be told that
	// something else is doing the work, not to sit in silence.
	Acquire(ctx context.Context, path string, wait time.Duration) (Unlock, error)
}

// Unlock releases a lock. It is safe to call more than once.
type Unlock func()

// Busy says something else holds the lock, and who.
type Busy struct {
	What   string // what is being waited for, as a person would say it
	Holder string // the process that has it, when it could be read
}

func (e *Busy) Error() string {
	if e.Holder == "" {
		return e.What + " is busy: another multiversectl is working on it"
	}
	return e.What + " is busy: " + e.Holder + " is working on it"
}
