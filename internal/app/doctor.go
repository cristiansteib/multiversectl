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

// DoctorInput picks the scope: a universe number, or 0 for the multiverse only.
type DoctorInput struct {
	Universe string
}

// Doctor inspects the host and the multiverse. A failing check is a finding in
// the report, never an error.
func (s *Service) Doctor(ctx context.Context, in DoctorInput) (Report, error) {
	var r Report
	add := func(name string, status Status, format string, a ...any) {
		r.Checks = append(r.Checks, Check{Name: name, Status: status, Detail: fmt.Sprintf(format, a...)})
	}

	s.checkRuntime(ctx, add)
	if s.Manifest == nil {
		add("manifest", StatusWarn, "no manifest loaded; multiversectl start <owner/repo>")
	} else {
		s.checkProxy(ctx, add)
		s.checkMultiverses(add)
		s.checkClones(ctx, add)
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
		s.checkHosting(ctx, add)
	}
	if _, found := s.System.Which("git"); found {
		add("git", StatusOK, "found")
	} else {
		add("git", StatusFail, "not on PATH")
	}

	if loaded && s.Manifest != nil {
		s.forUniverseOrSelf(st.Name).checkUniverse(ctx, add, st)
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
				"running on %s (%s), but this multiverse asks for %s — starting a universe moves it",
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

// checkClones says which shared clones are behind their remote. Their files
// are what a universe falls back on for a repository's own settings, secrets
// and the code new universes branch from, and nothing else brings them forward
// by itself when they have changes.
func (s *Service) checkClones(ctx context.Context, add addCheck) {
	fresh := s.CloneFreshness(ctx)
	behind := 0
	for _, key := range mapKeys(fresh) {
		f := fresh[key]
		if !f.Stale() {
			continue
		}
		behind++
		dir := s.Manifest.CloneDir(s.Multiverse, key)
		switch {
		case f.Dirty:
			add("clone "+key, StatusWarn, "%d commits behind origin/%s as of the last fetch, and %s has uncommitted changes, so it is never moved on its own: commit or stash them, then `multiversectl catalog update`", f.Behind, f.Follows, dir)
		case f.Ahead > 0:
			add("clone "+key, StatusWarn, "%d commits behind origin/%s and %d ahead of it: update %s by hand", f.Behind, f.Follows, f.Ahead, dir)
		default:
			add("clone "+key, StatusWarn, "%d commits behind origin/%s as of the last fetch; a universe with no checkout of it reads its files from here. Run: multiversectl catalog update", f.Behind, f.Follows)
		}
	}
	if len(fresh) > 0 && behind == 0 {
		add("clones", StatusOK, "the %d shared clones are up to date with their remotes, as of the last fetch", len(fresh))
	}
}

// checkMultiverses compares this multiverse's proxy with every other multiverse
// on the machine: two that want the same host port or network cannot both run
// their proxy. A manifest that cannot be read is skipped; it is that
// multiverse's own doctor run that reports it.
func (s *Service) checkMultiverses(add addCheck) {
	if s.Config == nil || s.Manifests == nil {
		return
	}
	cfg, err := s.Config.Load()
	if err != nil {
		return
	}
	names := make([]string, 0, len(cfg.Multiverses))
	for name := range cfg.Multiverses {
		names = append(names, name)
	}
	sort.Strings(names)
	others, clashes := 0, 0
	mine := s.Manifest.Name
	for _, name := range names {
		path := cfg.Multiverses[name].Manifest
		// Not a string comparison: -f takes a relative path, while the config
		// records an absolute one, and this multiverse would then be counted and
		// compared as another one.
		if samePath(path, s.Manifest.Path) {
			continue
		}
		other, err := s.Manifests.Load(path)
		if err != nil {
			continue
		}
		others++
		for _, clash := range domain.ProxyClashes(mine, s.Manifest, name, other) {
			clashes++
			add("multiverses", StatusWarn, "%s", clash)
		}
	}
	if others > 0 && clashes == 0 {
		add("multiverses", StatusOK, "no proxy clash with the %d other multiverses on this machine", others)
	}
}

// checkDomain resolves one hostname of the universe, or a probe name when
// there is none, and compares the answer with this machine's address.
func (s *Service) checkDomain(ctx context.Context, add addCheck, st domain.UniverseState, loaded bool) {
	if loaded && s.Manifest.IPOnly() {
		add("domain", StatusOK, "none: universes are reached by this machine's address and a port of their own, so no name needs to resolve")
		return
	}
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

	containers, err := s.Runtime.Containers(ctx, st.Project)
	// An app on demand that is not running is not started by anything, so a
	// secret it lacks is not yet a problem: it is checked when it is started.
	idle := func(appKey string) bool {
		return err == nil && s.Manifest.Apps[appKey].OnDemand && runningFor(containers, u.ProjectFor(appKey)) == 0
	}
	for _, appKey := range appKeys {
		for _, pair := range s.SecretPaths(u, appKey) {
			switch {
			case s.Files.Exists(pair.Dst):
				add("secret "+appKey, StatusOK, "%s", pair.Dst)
			case idle(appKey):
				add("secret "+appKey, StatusOK, "%s is not there yet; it is needed when the app is started", pair.Dst)
			default:
				add("secret "+appKey, StatusFail, "missing %s", pair.Dst)
			}
		}
	}

	if err != nil {
		add("containers", StatusWarn, "%v", err)
		return
	}
	for _, appKey := range s.Manifest.RunnableApps() {
		// Each app runs as its own compose project, so that is the prefix
		// its container names carry.
		running := runningFor(containers, s.Universe(st.Name).ProjectFor(appKey))
		if running == 0 && s.Manifest.Apps[appKey].OnDemand {
			add("app "+appKey, StatusOK, "off, on demand (start it with: multiversectl universe up %s %s)", st.Name, appKey)
			continue
		}
		add("app "+appKey, StatusOK, "%d running", running)
		if running > 0 {
			s.checkAddresses(ctx, add, st, appKey)
		}
	}
}

// checkAddresses asks each address of a running app once. An answer from the
// app counts, even a 401 or a 404: it proves the route and the service are
// there, which a container listing cannot. What are findings, as warnings
// because an app may legitimately not serve "/", are a refused connection, the
// proxy's own answers (its "no backend" statuses, and the 404 it gives an
// address no route matches) and a service that fails on its front page.
func (s *Service) checkAddresses(ctx context.Context, add addCheck, st domain.UniverseState, appKey string) {
	for _, route := range s.Routes(st, appKey) {
		url := s.routeURL(route)
		name := "url " + appKey + "/" + strings.TrimSuffix(route.Name, "-"+st.Slug)
		// Not every host speaks HTTP: a gRPC service answers an HTTP/1.1 request
		// with an error that says nothing about whether it is up.
		switch probeFor(route) {
		case "none":
			continue
		case "tcp", "grpc":
			address, err := tcpAddress(url)
			if err == nil {
				err = s.System.Dial(ctx, address)
			}
			if err != nil {
				add(name, StatusWarn, "%s does not accept connections: %v", url, shortError(err))
			} else {
				add(name, StatusOK, "%s accepts connections%s", url, map[bool]string{true: " (the gRPC health check is not run: multiversectl carries no gRPC client)", false: ""}[probeFor(route) == "grpc"])
			}
			continue
		}
		answer, err := s.System.Probe(ctx, url)
		status := answer.Status
		switch {
		case err != nil:
			add(name, StatusWarn, "%s does not answer: %v", url, err)
		case status == 502 || status == 503 || status == 504:
			add(name, StatusWarn, "%s answered %d: the proxy has no running service behind it", url, status)
		case status == 404 && isProxyNotFound(answer.Body):
			add(name, StatusWarn, "%s answered the proxy's own 404: no route matches this address (check the host, its entrypoint and that the service joined the proxy's network)", url)
		case status >= 500:
			add(name, StatusWarn, "%s answered %d: the service is failing on it (a gRPC service needs `scheme: h2c` on its host; otherwise see its logs)", url, status)
		default:
			add(name, StatusOK, "%s answers %d", url, status)
		}
	}
}

// probeFor is how doctor checks a host: what it says, else a TCP connection for
// a service the proxy speaks h2c to, else an HTTP request.
func probeFor(route Route) string {
	switch {
	case route.Probe != "":
		return route.Probe
	case route.Scheme == "h2c":
		return "tcp"
	}
	return "http"
}

// runningFor counts running containers of an app's services, relying on the
// "<multiverse>-<service>-<n>" naming the runtime gives them.
// Count by the app's own compose project: the manifest need not list services,
// so the multiverse prefix is the only thing that always identifies them.
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

// isProxyNotFound recognises the page Traefik gives an address no router
// matches, as opposed to a 404 from an app that does not serve that path.
func isProxyNotFound(body string) bool {
	return strings.TrimSpace(body) == "404 page not found"
}
