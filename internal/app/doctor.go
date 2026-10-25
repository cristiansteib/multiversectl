package app

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// Status is the outcome of one check.
type Status string

const (
	StatusOK   Status = "ok"
	StatusWarn Status = "warn"
	StatusFail Status = "fail"
)

// Check is one finding of Doctor.
type Check struct {
	Name   string
	Status Status
	Detail string
}

// Report is everything Doctor found.
type Report struct {
	Checks []Check
}

// DoctorInput picks the scope: a universe number, or 0 for the workspace only.
type DoctorInput struct {
	Universe string
}

// Doctor inspects the host and the workspace. A failing check is a finding in
// the report, never an error.
func (s *Service) Doctor(ctx context.Context, in DoctorInput) (Report, error) {
	var r Report
	add := func(name string, status Status, format string, a ...any) {
		r.Checks = append(r.Checks, Check{Name: name, Status: status, Detail: fmt.Sprintf(format, a...)})
	}

	s.checkRuntime(ctx, add)
	if s.Manifest == nil {
		add("manifest", StatusWarn, "no manifest loaded; run bootstrap")
	} else {
		s.checkProxy(ctx, add)
		s.checkWorkspaces(add)
	}

	var st domain.UniverseState
	loaded := false
	if in.Universe != "" {
		var err error
		if st, err = s.Resolve(in.Universe); err != nil {
			add("state", StatusFail, "universe %s: %v", in.Universe, err)
		} else {
			loaded = true
			add("state", StatusOK, "universe %s loaded", st.Name)
		}
	}

	if s.Manifest != nil {
		s.checkDomain(ctx, add, st, loaded)
	}
	if _, found := s.System.Which("git"); found {
		add("git", StatusOK, "found")
	} else {
		add("git", StatusFail, "not on PATH")
	}

	if loaded && s.Manifest != nil {
		s.checkUniverse(ctx, add, st)
	}
	return r, nil
}

type addCheck func(name string, status Status, format string, a ...any)

func (s *Service) checkRuntime(ctx context.Context, add addCheck) {
	version, err := s.Runtime.Version(ctx)
	if err != nil {
		add("runtime", StatusFail, "%v", err)
		return
	}
	add("runtime", StatusOK, "%s", version)
}

func (s *Service) checkProxy(ctx context.Context, add addCheck) {
	status, err := s.Proxy.Status(ctx)
	if err != nil {
		add("proxy", StatusFail, "%v", err)
		return
	}
	if status.Running {
		// A published port with no address written in front of it is every
		// interface, which is how the proxy was configured before exposure
		// was a choice.
		where := status.Bind
		if where == "" {
			where = "0.0.0.0"
		}
		reach := "reachable from this network"
		if where == "127.0.0.1" {
			reach = "this machine only"
		}
		if want := s.Manifest.BindAddress(); want != where {
			add("proxy", StatusWarn,
				"running on %s (%s), but this workspace asks for %s — starting a universe moves it",
				where, reach, want)
			return
		}
		add("proxy", StatusOK, "running on %s — %s", where, reach)
		return
	}
	add("proxy", StatusWarn, "not running; the first start brings it up")

	names := make([]string, 0, len(s.Manifest.Proxy.Entrypoints))
	for name := range s.Manifest.Proxy.Entrypoints {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		port := s.Manifest.Proxy.Entrypoints[name]
		label := fmt.Sprintf("port %d", port)
		free, err := s.System.PortFree(ctx, port)
		switch {
		case err != nil:
			add(label, StatusWarn, "entrypoint %s: cannot tell if it is free: %v", name, err)
		case !free:
			add(label, StatusFail, "entrypoint %s needs port %d, which is taken by another process", name, port)
		default:
			add(label, StatusOK, "entrypoint %s: free", name)
		}
	}
}

// checkWorkspaces compares this workspace's proxy with every other workspace
// on the machine: two that want the same host port or network cannot both run
// their proxy. A manifest that cannot be read is skipped; it is that
// workspace's own doctor run that reports it.
func (s *Service) checkWorkspaces(add addCheck) {
	if s.Config == nil || s.Manifests == nil {
		return
	}
	cfg, err := s.Config.Load()
	if err != nil {
		return
	}
	names := make([]string, 0, len(cfg.Workspaces))
	for name := range cfg.Workspaces {
		names = append(names, name)
	}
	sort.Strings(names)
	others, clashes := 0, 0
	for _, name := range names {
		path := cfg.Workspaces[name].Manifest
		if path == s.Manifest.Path {
			continue
		}
		other, err := s.Manifests.Load(path)
		if err != nil {
			continue
		}
		others++
		for _, clash := range domain.ProxyClashes(s.Manifest, other) {
			clashes++
			add("workspaces", StatusWarn, "%s", clash)
		}
	}
	if others > 0 && clashes == 0 {
		add("workspaces", StatusOK, "no proxy clash with the %d other workspaces on this machine", others)
	}
}

// checkDomain resolves one hostname of the universe, or a probe name when
// there is none, and compares the answer with this machine's address.
func (s *Service) checkDomain(ctx context.Context, add addCheck, st domain.UniverseState, loaded bool) {
	dom := st.Domain
	if !loaded {
		var err error
		if dom, err = s.ResolveDomain(ctx); err != nil {
			add("domain", StatusWarn, "%v", err)
			return
		}
	}
	host := "probe-u0." + dom
	if loaded {
		if name, ok := firstHost(st.Hosts); ok {
			host = name
		}
	}

	addrs, err := s.System.Resolve(ctx, host)
	if err != nil || len(addrs) == 0 {
		add("domain", StatusWarn, "%s does not resolve; a router or local DNS server may be stripping private-IP answers", host)
		return
	}
	lan, err := s.System.LANIP(ctx)
	if err != nil {
		add("domain", StatusWarn, "%s resolves to %s, but this machine's address is unknown: %v", host, strings.Join(addrs, ", "), err)
		return
	}
	for _, addr := range addrs {
		if addr == lan || strings.HasPrefix(addr, "127.") {
			add("domain", StatusOK, "%s resolves to %s", host, addr)
			return
		}
	}
	add("domain", StatusWarn, "%s resolves to %s, not to this machine (%s)", host, strings.Join(addrs, ", "), lan)
}

func (s *Service) checkUniverse(ctx context.Context, add addCheck, st domain.UniverseState) {
	u := s.Universe(st.Name)
	appKeys := make([]string, 0, len(s.Manifest.Apps))
	for key := range s.Manifest.Apps {
		appKeys = append(appKeys, key)
	}
	sort.Strings(appKeys)

	for _, appKey := range appKeys {
		for _, pair := range s.SecretPaths(u, appKey) {
			if s.Files.Exists(pair[1]) {
				add("secret "+appKey, StatusOK, "%s", pair[1])
			} else {
				add("secret "+appKey, StatusFail, "missing %s", pair[1])
			}
		}
	}

	containers, err := s.Runtime.Containers(ctx, st.Project)
	if err != nil {
		add("containers", StatusWarn, "%v", err)
		return
	}
	for _, appKey := range s.Manifest.RunnableApps() {
		// Each app runs as its own runtime project, so that is the prefix
		// its container names carry.
		add("app "+appKey, StatusOK, "%d running", runningFor(containers, s.Universe(st.Name).ProjectFor(appKey)))
	}
}

// runningFor counts running containers of an app's services, relying on the
// "<project>-<service>-<n>" naming the runtime gives them.
// Count by the app's own runtime project: the manifest need not list services,
// so the project prefix is the only thing that always identifies them.
func runningFor(containers []Container, project string) int {
	n := 0
	for _, c := range containers {
		if isRunning(c) && strings.HasPrefix(c.Name, project+"-") {
			n++
		}
	}
	return n
}

func firstHost(hosts map[string]string) (string, bool) {
	keys := make([]string, 0, len(hosts))
	for key := range hosts {
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return "", false
	}
	sort.Strings(keys)
	return hosts[keys[0]], true
}
