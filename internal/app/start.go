package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cristiansteib/multiversectl/internal/domain"
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
	apps, err := s.appsToAct(in.Apps)
	if err != nil {
		return err
	}
	if err := s.EnsureProxy(ctx, st.Domain); err != nil {
		return err
	}

	u := s.Universe(st.Name)

	// Addresses may come from the compose files' labels, which can have
	// changed since the universe was created, so resolve them again and keep
	// the result for the commands that only read.
	rules, err := s.resolveRules(ctx, u, st.Domain)
	if err != nil {
		return err
	}
	st.Rules, st.Hosts = rules, hostnames(rules)
	st.LastStarted = time.Now().UTC()
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
		cfg, err := s.Runtime.Inspect(ctx, base)
		if err != nil {
			return fmt.Errorf("%s: reading services: %w", appKey, err)
		}
		defined := cfg.Names()

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
		})
		if err != nil {
			return fmt.Errorf("%s: routing: %w", appKey, err)
		}
		req := s.request(u, st, appKey, in.With, extraFiles...)
		if len(req.Services) == 0 {
			req.Services = defined
		}
		if s.Manifest.Apps[appKey].Build || buildsAnImage(cfg) {
			if err := s.Runtime.Build(ctx, req); err != nil {
				return fmt.Errorf("%s: build: %w", appKey, err)
			}
		}
		if err := s.Runtime.Up(ctx, req); err != nil {
			return fmt.Errorf("%s: start: %w", appKey, err)
		}
		s.Out.OK("%s up", appKey)
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
	// One machine has one shared proxy, so a second workspace with different
	// entrypoints or a different exposure takes it over. Say so: the other
	// workspace's universes stop answering, and nothing else would tell you.
	if status, err := s.Proxy.Status(ctx); err == nil && status.Running {
		if status.Bind == "" {
			// No address in front of a published port means every interface.
			status.Bind = "0.0.0.0"
		}
		if change := proxyChange(status, want); change != "" {
			s.Out.Warn("the shared proxy is being reconfigured: %s — universes of any other workspace on this machine stop answering until it is started from there again", change)
		}
	}
	return s.Proxy.EnsureUp(ctx, want)
}

// proxyChange describes how a running proxy differs from what this workspace
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
// may be left empty when the app defines exactly one.
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
		cfg, err := s.Runtime.Inspect(ctx, req)
		if err != nil {
			return err
		}
		names := cfg.Names()
		if len(names) != 1 {
			return fmt.Errorf("app %q has %d services (%s): name the one to run in",
				in.App, len(names), strings.Join(names, ", "))
		}
		service = names[0]
	}
	if in.InRunning {
		return s.Runtime.Exec(ctx, req, service, in.Args)
	}
	return s.Runtime.Run(ctx, req, service, in.Args)
}
