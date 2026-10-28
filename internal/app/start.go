package app

import (
	"cmp"
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

// How long a hook is tried, and how often. The services are up, but a database
// may still be migrating under them, and a command that needs its tables fails
// until it is done.
var (
	hookEvery  = 3 * time.Second
	hookGiveUp = 3 * time.Minute
)

// StartInput names the universe, the apps to start (all runnable ones when
// empty) and any optional service groups to include.
type StartInput struct {
	Universe string
	Apps     []string
	With     []string
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
	apps, err := s.appsToAct(in.Apps)
	if err != nil {
		return err
	}
	u := s.Universe(st.Name)
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
	if err := s.EnsureProxy(ctx, st.Domain); err != nil {
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
			s.Out.Info("the manifest changed since this universe was last started; it starts with the current one (`multiversectl universe config` shows what changed)")
		}
		st.StartedWith = rec
	}
	if err := s.Store.Save(st); err != nil {
		return err
	}

	for _, appKey := range apps {
		if s.NothingToRun(u, appKey) {
			s.Out.Info("%s: no compose file in its repository, so there is nothing to start", appKey)
			continue
		}
		// The compose files already name the services; asking them means a
		// manifest does not have to repeat the list, and cannot get it wrong.
		if _, err := s.composeFiles(u, appKey); err != nil {
			return err
		}
		base := s.request(u, st, appKey, in.With)
		s.say(1, "%s: reading its services from the compose files", appKey)
		s.say(3, "%s: compose project %s, files %s, profiles %v", appKey, base.Project, strings.Join(base.Files, ", "), base.Profiles)
		cfg, err := s.Runtime.Inspect(ctx, base)
		if err != nil {
			return fmt.Errorf("%s: reading services: %w", appKey, err)
		}
		defined := cfg.Names()
		s.say(1, "%s: services %s", appKey, strings.Join(defined, ", "))

		serviceEnv, err := s.serviceEnv(st, cfg)
		if err != nil {
			return fmt.Errorf("%s: %w", appKey, err)
		}
		extraFiles, err := s.Router.Materialize(ctx, OverrideRequest{
			Dir:       u.Dir(),
			App:       appKey,
			Routes:    s.Routes(st, appKey),
			Unpublish: s.unpublished(appKey, defined, cfg),
			Env:       serviceEnv,
			Bind:      s.Manifest.BindAddress(),
		})
		if err != nil {
			return fmt.Errorf("%s: routing: %w", appKey, err)
		}
		req := s.request(u, st, appKey, in.With, extraFiles...)
		if len(req.Services) == 0 {
			req.Services = defined
		}
		if s.Manifest.Apps[appKey].Build || buildsAnImage(cfg) {
			s.say(1, "%s: building the images", appKey)
			if err := s.Runtime.Build(ctx, req); err != nil {
				return fmt.Errorf("%s: build: %w", appKey, err)
			}
		}
		s.say(1, "%s: starting %s", appKey, strings.Join(req.Services, ", "))
		s.say(3, "%s: environment given to compose: %s (names only)", appKey, strings.Join(slices.Sorted(maps.Keys(req.Env)), ", "))
		if err := s.Runtime.Up(ctx, req); err != nil {
			return fmt.Errorf("%s: start: %w", appKey, err)
		}
		s.Out.OK("%s up", appKey)
		st = s.runHooks(ctx, st, appKey, req, defined)
	}
	return nil
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
func (s *Service) EnsureProxy(ctx context.Context, dom string) error {
	// Universes are started side by side, by the web view and by agents, and
	// the first of them to need the shared proxy must not race the others to
	// create it: one makes the network and the container, the rest find them.
	proxyMu.Lock()
	defer proxyMu.Unlock()
	return s.ensureProxy(ctx, dom)
}

// proxyMu serialises the checks and the start of the one proxy a machine has,
// across every universe started from this process.
var proxyMu sync.Mutex

func (s *Service) ensureProxy(ctx context.Context, dom string) error {
	network := s.Manifest.Proxy.Network
	if network == "" {
		network = "multiversenet"
	}
	if err := s.Proxy.EnsureNetwork(ctx, network); err != nil {
		return err
	}
	want := ProxyRequest{
		Domain:      dom,
		Entrypoints: s.Manifest.Proxy.Entrypoints,
		Network:     network,
		Bind:        s.Manifest.BindAddress(),
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
			return fmt.Errorf("proxy entrypoint %q needs port %d, which another process is using (find it with: lsof -nP -iTCP:%d -sTCP:LISTEN)", name, port, port)
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
	return strings.Join(changes, ", ")
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
	if _, ok := s.Manifest.Apps[in.App]; !ok {
		return fmt.Errorf("app %q: %w in the manifest", in.App, domain.ErrNotFound)
	}
	u := s.Universe(st.Name)
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
		if rule.PortForward == domain.PortForwardAuto {
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
		if port != 0 && st.Rules[hostKey].PortForward == domain.PortForwardAuto && !taken[port] {
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

// runHooks runs the hooks an app asks for once its services are up, in order.
// A hook marked Once runs on a universe's first start only, and is remembered:
// it is what a repository needs before a universe is of any use (the data a
// person logs in with), and running it on every start would load it over
// whatever was changed since.
//
// A hook that never succeeds is said, with how to run it by hand, and is not an
// error: the universe is up, and refusing it for a seed that did not load would
// be the worse of the two. The hooks after it wait, since they may depend on it,
// and it is tried again on the next start.
func (s *Service) runHooks(ctx context.Context, st domain.UniverseState, appKey string, req RuntimeRequest, services []string) domain.UniverseState {
	for _, hook := range s.Manifest.Apps[appKey].After {
		key := hook.Key(appKey)
		if hook.Once && slices.Contains(st.HooksDone, key) {
			continue
		}
		service := cmp.Or(hook.Service, s.Manifest.Apps[appKey].DefaultService)
		if service == "" && len(services) == 1 {
			service = services[0]
		}
		line := strings.Join(hook.Command, " ")
		if service == "" {
			s.Out.Warn("%s: hook `%s` names no service, and the app has %d: set `container.service` on it", appKey, line, len(services))
			return st
		}
		s.say(3, "%s: hook %s, once=%v, already done=%v", appKey, key, hook.Once, slices.Contains(st.HooksDone, key))
		s.Out.Info("%s: running `%s` in %s", appKey, line, service)
		if out, err := s.tryUntil(ctx, func() (string, error) { return s.Runtime.ExecQuiet(ctx, req, service, hook.Command) }); err != nil {
			s.Out.Warn("%s: `%s` did not succeed (%v). The universe is up; run it yourself with `multiversectl universe exec %s %s --service %s -- %s`, and it is tried again on the next start",
				appKey, line, err, st.Name, appKey, service, line)
			if tail := lastLines(out, 6); tail != "" {
				s.Out.Plain("%s", tail)
			}
			return st
		}
		s.Out.OK("%s: ran `%s`", appKey, line)
		if !hook.Once {
			continue
		}
		st.HooksDone = append(slices.Clone(st.HooksDone), key)
		if err := s.Store.Save(st); err != nil {
			s.Out.Warn("%s: `%s` ran, but that could not be kept, so it runs again on the next start: %v", appKey, line, err)
			return st
		}
	}
	return st
}

// tryUntil runs f until it succeeds, for up to hookGiveUp, and returns what the
// last try printed with its error.
func (s *Service) tryUntil(ctx context.Context, f func() (string, error)) (string, error) {
	deadline := time.Now().Add(hookGiveUp)
	for {
		out, err := f()
		if err == nil {
			return out, nil
		}
		s.say(3, "not yet (%v); trying again in %s", err, hookEvery)
		if ctx.Err() != nil || time.Now().Add(hookEvery).After(deadline) {
			return out, err
		}
		select {
		case <-ctx.Done():
			return out, ctx.Err()
		case <-time.After(hookEvery):
		}
	}
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
