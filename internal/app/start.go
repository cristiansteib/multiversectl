package app

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// StartInput names the universe, the apps to start (all runnable ones when
// empty) and any optional service groups to include.
type StartInput struct {
	Universe string
	Apps     []string
	With     []string
	// RebindProxy lets the start reconfigure the shared proxy when universes of
	// other multiverses are running through it.
	RebindProxy bool
	// Wait says how to wait for each app to be usable once its services run.
	Wait WaitOptions
	// FailFast stops at the first app that does not start. By default the
	// others are still started and the failures are reported at the end.
	FailFast bool
}

// AppFailure is one app that did not start, and why.
type AppFailure struct {
	App string
	Err error
}

// StartFailed is what a start returns when some apps came up and some did not.
// What failed is already said, app by app, when it is returned.
type StartFailed struct {
	Up     []string
	Failed []AppFailure
	Apps   int
}

func (e *StartFailed) Unwrap() []error {
	out := make([]error, len(e.Failed))
	for i, f := range e.Failed {
		out[i] = f.Err
	}
	return out
}

func (e *StartFailed) Error() string {
	names := make([]string, len(e.Failed))
	for i, f := range e.Failed {
		names[i] = f.App
	}
	return fmt.Sprintf("%d of %d apps did not start (%s)", len(e.Failed), e.Apps, strings.Join(names, ", "))
}

// sayStartSummary is the end of a start that was not clean: which apps are up
// and which are not, so the answer is in one place and not in the scrollback.
func (s *Service) sayStartSummary(up []string, failed []AppFailure) {
	if len(up) > 0 {
		s.Out.OK("up: %s", strings.Join(up, ", "))
	}
	for _, f := range failed {
		s.Out.Warn("failed: %s", strings.ReplaceAll(strings.TrimSpace(f.Err.Error()), "\n", "\n    "))
	}
	s.Out.Info("the multiverse's own postUp hooks were not run; fix the failed apps and start them again")
}

// Start brings up the proxy and then each app of the universe.
func (s *Service) Start(ctx context.Context, in StartInput) error {
	st, err := s.Resolve(in.Universe)
	if err != nil {
		return err
	}
	unlock, err := s.lockUniverse(ctx, st.ID, st.Name)
	if err != nil {
		return err
	}
	defer unlock()
	// Read again now it is held: the universe may have been removed, or
	// changed, while this was waiting for whoever had it.
	if st, err = s.Resolve(in.Universe); err != nil {
		return err
	}
	// First, so a repository's file that a universe without a checkout of it
	// reads from the clone is the current one.
	s.KeepClonesCurrent(ctx)
	// The repositories' own files are read from this universe's worktrees.
	if s, err = s.forUniverse(st.Name); err != nil {
		return err
	}
	apps, err := s.appsToStart(st.Name, in.Apps)
	if err != nil {
		return err
	}
	u := s.UniverseOf(st)
	s.say(1, "universe `%s`: starting %s", st.Name, strings.Join(apps, ", "))

	// Before the proxy is touched, so a missing file leaves nothing half started.
	var runnable []string
	for _, appKey := range apps {
		if !s.NothingToRun(u, appKey) {
			runnable = append(runnable, appKey)
		}
	}
	s.say(1, "checking the secret files of %s", strings.Join(runnable, ", "))
	if err := s.PrepareSecrets(u, runnable); err != nil {
		return err
	}
	s.say(1, "making sure the proxy and its network are up")
	if err := s.EnsureProxy(ctx, st.Domain, in.RebindProxy); err != nil {
		return err
	}

	// Addresses may come from the compose files' labels, which can have
	// changed since the universe was created, so resolve them again and keep
	// the result for the commands that only read.
	s.say(1, "reading the addresses from the compose files")
	rules, err := s.resolveRules(ctx, u, st.Domain)
	if err != nil {
		return fmt.Errorf("%w%s", err, s.behindHint(ctx, st))
	}
	s.sayRules(rules)
	if err := s.forwardPorts(ctx, st, rules); err != nil {
		return err
	}
	st.Rules, st.Hosts = rules, hostnames(rules)
	st.LastStarted = time.Now().UTC()
	// Which manifest it starts with is kept: the file can be edited later, and the
	// universe goes on running on what it was started with until it is started again.
	if rec, err := s.recordConfig(st.ID); err != nil {
		s.Out.Warn("could not keep the manifest this universe starts with: %v", err)
	} else {
		if prev := st.StartedWith.SHA256; prev != "" && prev != rec.SHA256 {
			s.Out.Info("the manifest changed since this universe was last started; it starts with the current one (`multiversectl universe about --diff` shows what changed)")
		}
		st.StartedWith = rec
	}
	if err := s.Store.Save(st); err != nil {
		return err
	}

	if st, err = s.fire(ctx, domain.PreUp, st, multiverseHooks, nil, nil); err != nil {
		return err
	}
	var up []string
	var failed []AppFailure
	for _, appKey := range apps {
		if s.NothingToRun(u, appKey) {
			s.Out.Info("%s: no compose file in its repository, so there is nothing to start", appKey)
			continue
		}
		var err error
		if st, err = s.startApp(ctx, u, st, appKey, in); err != nil {
			if in.FailFast || ctx.Err() != nil {
				return err
			}
			// The apps are independent: one that cannot start must not keep the
			// others from it. Its hooks have not run, and are not run later.
			s.Out.Warn("%s did not start; going on with the other apps", appKey)
			if !strings.HasPrefix(err.Error(), appKey+":") {
				err = fmt.Errorf("%s: %w", appKey, err)
			}
			failed = append(failed, AppFailure{App: appKey, Err: err})
			continue
		}
		up = append(up, appKey)
	}
	if len(failed) == 1 && len(up) == 0 {
		return failed[0].Err // one app: nothing to sum up, and the error says it
	}
	if len(failed) > 0 {
		s.sayStartSummary(up, failed)
		return &StartFailed{Up: up, Failed: failed, Apps: len(up) + len(failed)}
	}
	_, err = s.fire(ctx, domain.PostUp, st, multiverseHooks, nil, nil)
	return err
}

// startApp brings one app up: renders its files, routes it, builds, starts,
// waits for it, and runs its hooks. It returns the state as the hooks left it.
func (s *Service) startApp(ctx context.Context, u domain.Universe, st domain.UniverseState, appKey string, in StartInput) (domain.UniverseState, error) {
	// Before the compose files are read and before anything starts: a file the
	// app reads at boot, or an env_file, must be there by then.
	if err := s.renderApp(ctx, u, st, appKey, false); err != nil {
		return st, err
	}
	// The compose files already name the services; asking them means a
	// manifest does not have to repeat the list, and cannot get it wrong.
	if _, err := s.composeFiles(u, appKey); err != nil {
		return st, err
	}
	base := s.request(u, st, appKey, in.With)
	s.say(1, "%s: reading its services from the compose files", appKey)
	s.say(3, "%s: compose project %s, files %s, profiles %v", appKey, base.Project, strings.Join(base.Files, ", "), base.Profiles)
	cfg, err := s.Runtime.Inspect(ctx, base)
	if err != nil {
		return st, fmt.Errorf("%s: reading services: %w", appKey, err)
	}
	defined := cfg.Names()
	s.say(1, "%s: services %s", appKey, strings.Join(defined, ", "))

	serviceEnv, err := s.serviceEnv(st, cfg)
	if err != nil {
		return st, fmt.Errorf("%s: %w", appKey, err)
	}
	routes := onTheirOwnNetworks(s.withoutUnroutable(appKey, s.Routes(st, appKey), cfg), cfg)
	extraFiles, err := s.Router.Materialize(ctx, OverrideRequest{
		Dir:       u.Dir(),
		App:       appKey,
		Routes:    routes,
		Unpublish: s.unpublished(appKey, defined, cfg),
		Env:       serviceEnv,
		Bind:      s.Manifest.BindAddress(),
	})
	if err != nil {
		return st, fmt.Errorf("%s: routing: %w", appKey, err)
	}
	req := s.request(u, st, appKey, in.With, extraFiles...)
	if len(req.Services) == 0 {
		req.Services = defined
	}
	// Before the hooks and the build: an image is tagged with the project's
	// name, so building for another root's project would replace its images.
	if err := s.guardProject(ctx, u, req.Project); err != nil {
		return st, fmt.Errorf("%s: %w", appKey, err)
	}
	if st, err = s.fire(ctx, domain.PreUp, st, appKey, &req, defined); err != nil {
		return st, err
	}
	if s.Manifest.Apps[appKey].Build || buildsAnImage(cfg) {
		s.say(1, "%s: building the images", appKey)
		if err := s.Runtime.Build(ctx, req); err != nil {
			return st, fmt.Errorf("%s: build: %w", appKey, err)
		}
	}
	s.say(1, "%s: starting %s", appKey, strings.Join(req.Services, ", "))
	s.say(3, "%s: environment given to compose: %s (names only)", appKey, strings.Join(slices.Sorted(maps.Keys(req.Env)), ", "))
	if err := s.Runtime.Up(ctx, req); err != nil {
		return st, fmt.Errorf("%s: start: %w", appKey, err)
	}
	s.Out.OK("%s up", appKey)
	// The proxy reaches the app on the app's own network, which exists now.
	if err := s.joinProxy(ctx, appKey, routes); err != nil {
		return st, err
	}
	// Before the hooks, which may rely on the app answering.
	if err := s.waitReady(ctx, appKey, routes, req, in.Wait); err != nil {
		return st, err
	}
	if st, err = s.fire(ctx, domain.PostUp, st, appKey, &req, defined); err != nil {
		return st, err
	}
	return st, nil
}

// withoutUnroutable drops the routes to services that set network_mode. A service
// in another network mode (host, or another container's) is on none of its project's
// networks, so the proxy has nothing to join and cannot reach it whatever is
// written; a port forward is dropped with it, because the fragment cannot publish
// a port for such a service either.
func (s *Service) withoutUnroutable(appKey string, routes []Route, cfg ProjectConfig) []Route {
	var out []Route
	for _, r := range routes {
		mode := cfg.Services[r.Service].NetworkMode
		if mode == "" {
			out = append(out, r)
			continue
		}
		extra := ""
		if r.HostPort > 0 {
			extra = fmt.Sprintf("; its forwarded port %d is not published either (a service in network_mode %q is reached on that network directly)", r.HostPort, mode)
		}
		s.Out.Warn("%s: service %q sets network_mode %q, so it is on none of its app's networks and host %s cannot be reached through the proxy%s",
			appKey, r.Service, mode, r.Hostname, extra)
	}
	return out
}

// unpublished is every service that must not bind a host port: all of them,
// minus the few a manifest asks to keep. Universes are reached through the
// proxy, so a published port is the exception, not the rule.
func (s *Service) unpublished(appKey string, defined []string, cfg ProjectConfig) []string {
	keep := map[string]bool{}
	for _, name := range s.Manifest.Apps[appKey].Publish {
		keep[name] = true
	}
	for _, name := range publishes(cfg) {
		keep[name] = true
	}
	var out []string
	for _, name := range defined {
		if !keep[name] {
			out = append(out, name)
		}
	}
	return out
}

// buildsAnImage saves a manifest from saying `build: true` when the compose
// file already has a build section.
func buildsAnImage(cfg ProjectConfig) bool {
	for _, service := range cfg.Services {
		if service.Build {
			return true
		}
	}
	return false
}

// hostnames reduces resolved rules to the host -> hostname map that the
// reading commands and the runtime environment use.
func hostnames(rules map[string]domain.RouteRule) map[string]string {
	out := make(map[string]string, len(rules))
	for hostKey, rule := range rules {
		out[hostKey] = rule.Hostname
	}
	return out
}

// EnsureProxy makes sure the shared network and ingress are running.
//
// rebind lets it reconfigure a proxy that is running with different settings
// (the interface it is published on, its entrypoints, its TLS) even though
// universes of other multiverses are being served through it; without it that
// is refused, since they would stop answering.
func (s *Service) EnsureProxy(ctx context.Context, dom string, rebind bool) error {
	// Universes are started side by side, by the web view and by agents, and
	// the first of them to need the shared proxy must not race the others to
	// create it: one makes the network and the container, the rest find them.
	proxyMu.Lock()
	defer proxyMu.Unlock()
	return s.ensureProxy(ctx, dom, rebind)
}

// proxyMu serialises the checks and the start of the one proxy a machine has,
// across every universe started from this process.
var proxyMu sync.Mutex

func (s *Service) ensureProxy(ctx context.Context, dom string, rebind bool) error {
	network := s.Manifest.Proxy.Network
	if network == "" {
		network = "multiversenet"
	}
	if err := s.Proxy.EnsureNetwork(ctx, network); err != nil {
		return err
	}
	want := ProxyRequest{
		Domain:       dom,
		Entrypoints:  s.Manifest.Proxy.Entrypoints,
		Network:      network,
		Bind:         s.Manifest.BindAddress(),
		TLS:          s.Manifest.Proxy.TLS,
		Certificates: s.Manifest.Proxy.Certificates,
	}
	// One machine has one shared proxy, so a second multiverse with different
	// entrypoints or a different exposure takes it over. Say so: the other
	// multiverse's universes stop answering, and nothing else would tell you.
	status, err := s.Proxy.Status(ctx)
	if err == nil && !status.Running {
		// Docker's own refusal arrives after it has created the container,
		// and names neither the entrypoint nor what to run to find the culprit.
		if err := s.checkProxyPorts(ctx); err != nil {
			return err
		}
	}
	if err == nil && status.Running {
		if status.Bind == "" {
			// No address in front of a published port means every interface.
			status.Bind = "0.0.0.0"
		}
		if change := proxyChange(status, want); change != "" {
			if others := s.otherMultiversesServed(ctx); len(others) > 0 && !rebind {
				return fmt.Errorf("this multiverse needs the shared proxy %s, which would stop universes of %s from answering: nothing was changed. "+
					"Make this multiverse match the running proxy (its `expose`, proxy entrypoints and TLS), stop those universes first, "+
					"or pass --rebind-proxy to reconfigure it anyway; they answer again when started from their own multiverse",
					change, strings.Join(others, ", "))
			}
			s.Out.Warn("the shared proxy is being reconfigured: %s — universes of any other multiverse on this machine stop answering until it is started from there again", change)
		}
	}
	return s.Proxy.EnsureUp(ctx, want)
}

// checkProxyPorts fails before the proxy is started when another process
// already holds one of its entrypoint ports. A running proxy holds them itself,
// so this only runs when it is down.
func (s *Service) checkProxyPorts(ctx context.Context) error {
	names := make([]string, 0, len(s.Manifest.Proxy.Entrypoints))
	for name := range s.Manifest.Proxy.Entrypoints {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		port := s.Manifest.Proxy.Entrypoints[name]
		// A check that cannot run is not a conflict; Docker will say if it is.
		if free, err := s.System.PortFree(ctx, port); err == nil && !free {
			return fmt.Errorf("proxy entrypoint %q needs port %d, which another process is using (find it with: lsof -nP -iTCP:%d -sTCP:LISTEN). "+
				"When it is another multiversectl proxy (docker ps --filter name=multiverse-proxy), such as the real one during a rehearsal with --state-dir, "+
				"stop it with `multiversectl system proxy down`, given the same --state-dir it was started with", name, port, port)
		}
	}
	return nil
}

// proxyChange describes how a running proxy differs from what this multiverse
// needs, or "" when it already matches.
func proxyChange(status ProxyStatus, want ProxyRequest) string {
	var changes []string
	if status.Bind != want.Bind {
		changes = append(changes, fmt.Sprintf("published on %s instead of %s", want.Bind, status.Bind))
	}
	if !samePorts(status.Entrypoints, want.Entrypoints) {
		changes = append(changes, fmt.Sprintf("entrypoints %s instead of %s",
			describePorts(want.Entrypoints), describePorts(status.Entrypoints)))
	}
	var certs []string
	for _, c := range want.Certificates {
		certs = append(certs, c.CertFile)
	}
	if !slices.Equal(status.Certificates, certs) {
		changes = append(changes, fmt.Sprintf("certificates %s instead of %s", orNone(certs), orNone(status.Certificates)))
	}
	for _, name := range slices.Sorted(maps.Keys(want.Entrypoints)) {
		if status.TLS[name] != want.TLS[name] && status.Entrypoints[name] != 0 {
			changes = append(changes, fmt.Sprintf("entrypoint %s %s TLS", name, map[bool]string{true: "with", false: "without"}[want.TLS[name]]))
		}
	}
	return strings.Join(changes, ", ")
}

func orNone(list []string) string {
	if len(list) == 0 {
		return "none"
	}
	return strings.Join(list, ", ")
}

func samePorts(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for name, port := range a {
		if b[name] != port {
			return false
		}
	}
	return true
}

func describePorts(ports map[string]int) string {
	names := make([]string, 0, len(ports))
	for name := range ports {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, fmt.Sprintf("%s:%d", name, ports[name]))
	}
	return strings.Join(out, " ")
}

// RunInput is a command inside one of a universe's services, for the things a
// shell is for: a migration, a database client, a REPL.
type RunInput struct {
	Universe string
	App      string
	Service  string
	Args     []string
	// InRunning picks an already running container instead of a fresh one.
	InRunning bool
}

// Run executes a command in a new container of the given service. The service
// may be left empty when the app defines exactly one, or names a defaultService.
func (s *Service) Run(ctx context.Context, in RunInput) error {
	st, err := s.Resolve(in.Universe)
	if err != nil {
		return err
	}
	s = s.forUniverseOrSelf(st.Name)
	if _, ok := s.Manifest.Apps[in.App]; !ok {
		return fmt.Errorf("app %q: %w in the manifest", in.App, domain.ErrNotFound)
	}
	u := s.UniverseOf(st)
	req := s.request(u, st, in.App, nil)

	service := in.Service
	if service == "" {
		service = s.Manifest.Apps[in.App].DefaultService
	}
	if service == "" {
		cfg, err := s.Runtime.Inspect(ctx, req)
		if err != nil {
			return err
		}
		names := cfg.Names()
		if len(names) != 1 {
			return fmt.Errorf("app %q has %d services (%s): name the one to run in (--service), or set defaultService on the app",
				in.App, len(names), strings.Join(names, ", "))
		}
		service = names[0]
	}
	if in.InRunning {
		return s.Runtime.Exec(ctx, req, service, in.Args)
	}
	return s.Runtime.Run(ctx, req, service, in.Args)
}

// forwardPorts gives every host that asked for portForward a host port and the
// address that port is reached at. The port is chosen once, from a free one,
// and kept in the universe's state: a phone given the address keeps working
// across restarts, and a stopped universe keeps its port from being handed to
// another. A kept port is only replaced when something else took it while the
// universe was down.
func (s *Service) forwardPorts(ctx context.Context, st domain.UniverseState, rules map[string]domain.RouteRule) error {
	keys := make([]string, 0, len(rules))
	for hostKey, rule := range rules {
		if domain.Forwarded(rule.PortForward) {
			keys = append(keys, hostKey)
		}
	}
	if len(keys) == 0 {
		return nil
	}
	sort.Strings(keys)

	address := "127.0.0.1"
	if !s.Manifest.LocalOnly() {
		ip, err := s.System.LANIP(ctx)
		if err != nil {
			return fmt.Errorf("portForward: %w", err)
		}
		address = ip
	}

	taken := map[int]bool{}
	if all, err := s.Store.List(); err == nil {
		for _, other := range all {
			if other.ID == st.ID {
				continue
			}
			for _, rule := range other.Rules {
				if rule.HostPort != 0 {
					taken[rule.HostPort] = true
				}
			}
		}
	}
	running := s.universeRunning(ctx, st)

	for _, hostKey := range keys {
		rule := rules[hostKey]
		port := st.Rules[hostKey].HostPort
		if port != 0 && domain.Forwarded(st.Rules[hostKey].PortForward) && !taken[port] {
			// Our own containers hold the port while the universe runs, which
			// reads as "in use"; only a stopped universe proves a clash.
			if free, err := s.System.PortFree(ctx, port); err != nil || free || running {
				taken[port] = true
				rule.HostPort = port
			}
		}
		for rule.HostPort == 0 {
			picked, err := s.System.FreePort(ctx)
			if err != nil {
				return fmt.Errorf("%s: portForward: %w", hostKey, err)
			}
			if !taken[picked] {
				taken[picked] = true
				rule.HostPort = picked
			}
		}
		rule.ForwardURL = fmt.Sprintf("http://%s:%d", address, rule.HostPort)
		rules[hostKey] = rule
	}
	return nil
}

// universeRunning reports whether any container of the universe is up.
func (s *Service) universeRunning(ctx context.Context, st domain.UniverseState) bool {
	containers, err := s.Runtime.Containers(ctx, st.Project)
	if err != nil {
		return false
	}
	for _, c := range containers {
		if isRunning(c) {
			return true
		}
	}
	return false
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// sayRules tells which hostnames a universe answers on, which is what a person
// following a start wants to see before anything is up.
func (s *Service) sayRules(rules map[string]domain.RouteRule) {
	if s.Verbosity < 3 {
		return
	}
	for _, key := range slices.Sorted(maps.Keys(rules)) {
		s.say(3, "address %s: %s", key, rules[key].Hostname)
	}
}

// otherMultiversesServed names the other multiverses on this machine that have
// containers running: those are the ones a reconfigured shared proxy would stop
// answering. A multiverse is told apart by where its containers were started
// from, so a container that carries no such label is not counted; and one whose
// manifest cannot be read has no name to look its containers up by.
func (s *Service) otherMultiversesServed(ctx context.Context) []string {
	if s.Config == nil || s.Manifests == nil {
		return nil
	}
	cfg, err := s.Config.Load()
	if err != nil {
		return nil
	}
	var out []string
	for _, name := range slices.Sorted(maps.Keys(cfg.Multiverses)) {
		entry := cfg.Multiverses[name]
		if entry.Root == "" || samePath(entry.Root, s.Multiverse) || samePath(entry.Manifest, s.Manifest.Path) {
			continue
		}
		other, err := s.Manifests.Load(entry.Manifest)
		if err != nil {
			continue
		}
		containers, err := s.Runtime.Containers(ctx, other.Name)
		if err != nil {
			continue
		}
		for _, c := range containers {
			if isRunning(c) && (within(c.WorkingDir, entry.Root) || within(c.ConfigFile, entry.Root)) {
				out = append(out, name)
				break
			}
		}
	}
	return out
}

// joinProxy connects the proxy to the network each routed service of the app is on.
// The proxy is on no network of a universe until now: a name on a network shared
// by universes would be an alias of the service of every one of them.
func (s *Service) joinProxy(ctx context.Context, appKey string, routes []Route) error {
	seen := map[string]bool{}
	for _, r := range routes {
		if r.Network == "" || seen[r.Network] {
			continue
		}
		seen[r.Network] = true
		if err := s.Proxy.ConnectNetwork(ctx, r.Network); err != nil {
			return fmt.Errorf("%s: connecting the proxy to %s: %w", appKey, r.Network, err)
		}
	}
	return nil
}

// leaveProxy disconnects the proxy from the app's networks before the app is taken
// down: compose cannot remove a network that something is still connected to. It
// goes by the compose project rather than by one network name, so an app whose
// compose files name their own networks is covered too.
// There is nothing to undo when the proxy is not there, so a failure is not one.
func (s *Service) leaveProxy(ctx context.Context, u domain.Universe, appKey string) {
	if s.Proxy == nil {
		return
	}
	_ = s.Proxy.DisconnectProject(ctx, u.ProjectFor(appKey))
}

// onTheirOwnNetworks points each route at the network docker really puts its
// service on. The name is guessed before the compose files are read, and the guess
// is wrong for a compose file that names its own networks: there is no
// <project>_default then, and connecting the proxy to it would fail the start.
func onTheirOwnNetworks(routes []Route, cfg ProjectConfig) []Route {
	out := make([]Route, 0, len(routes))
	for _, r := range routes {
		if nets := cfg.Services[r.Service].Networks; r.Network != "" && len(nets) > 0 {
			r.Network = nets[0]
		}
		out = append(out, r)
	}
	return out
}
