package app

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// Labels a compose file uses to say how it is reached. The key names the host,
// the value is the port it listens on:
//
//	labels:
//	  multiverse.host.app: "4200"              # app-u3.<domain> -> 4200
//	  multiverse.host.api: "8001"
//	  multiverse.host.api.entrypoint: devapi   # default: web
//	  multiverse.host.api.aliases: app,adm     # same service, other hostnames
//	  multiverse.publish: "true"               # keep my published ports
//
// With no labels at all, a service that exposes exactly one port is reached at
// the app's own name, so the common case needs nothing.
const (
	hostLabel    = "multiverse.host."
	publishLabel = "multiverse.publish"
	// envLabel lets a service ask for a value only the universe knows:
	//
	//   multiverse.env.DJANGO_CSRF_TRUSTED_ORIGINS: origins
	//
	// A label rather than an ${MULTIVERSE_ORIGINS} in the service's own
	// environment, because a compose file is also run by hand: a reference
	// resolves to the empty string when multiversectl is not driving, while a
	// label is inert metadata until it is.
	envLabel = "multiverse.env."
)

// Sources an env label can name.
const (
	srcOrigins  = "origins"  // every address as an origin, with ports
	srcHosts    = "hosts"    // every hostname
	srcDomain   = "domain"   // the wildcard domain
	srcUniverse = "universe" // the universe's name
	srcSlug     = "slug"     // the part its hostnames carry
	srcHost     = "host:"    // one host key's hostname
	srcURL      = "url:"     // one host key's address
)

// serviceEnv resolves the multiverse.env.* labels of every service of an app.
// It fails on a source it does not know rather than leaving a variable unset:
// an empty ALLOWED_HOSTS is a confusing runtime error far from its cause.
func (s *Service) serviceEnv(st domain.UniverseState, cfg ProjectConfig) ([]ServiceEnv, error) {
	hosts, origins := s.universeAddresses(st)
	var out []ServiceEnv
	for _, name := range cfg.Names() {
		var vars []EnvVar
		labels := cfg.Services[name].Labels
		for _, key := range mapKeys(labels) {
			varName, ok := strings.CutPrefix(key, envLabel)
			if !ok || varName == "" {
				continue
			}
			value, err := s.resolveEnvSource(labels[key], st, hosts, origins)
			if err != nil {
				return nil, fmt.Errorf("service %s: label %s: %w", name, key, err)
			}
			vars = append(vars, EnvVar{Name: varName, Value: value})
		}
		if len(vars) > 0 {
			out = append(out, ServiceEnv{Service: name, Vars: vars})
		}
	}
	return out, nil
}

func (s *Service) resolveEnvSource(source string, st domain.UniverseState, hosts, origins []string) (string, error) {
	switch source {
	case srcOrigins:
		return strings.Join(origins, ","), nil
	case srcHosts:
		return strings.Join(hosts, ","), nil
	case srcDomain:
		return st.Domain, nil
	case srcUniverse:
		return st.Name, nil
	case srcSlug:
		return st.Slug, nil
	}
	if key, ok := strings.CutPrefix(source, srcHost); ok {
		hostname, known := st.Hosts[key]
		if !known {
			return "", fmt.Errorf("no host %q in this project", key)
		}
		return hostname, nil
	}
	if key, ok := strings.CutPrefix(source, srcURL); ok {
		if _, known := st.Hosts[key]; !known {
			return "", fmt.Errorf("no host %q in this project", key)
		}
		return s.originOf(st, key, st.Hosts[key]), nil
	}
	return "", fmt.Errorf("unknown source %q: want %s, %s, %s, %s, %s, host:<key> or url:<key>",
		source, srcOrigins, srcHosts, srcDomain, srcUniverse, srcSlug)
}

// resolveRules works out every address of a universe, in order of how
// explicit it is: hosts declared in the manifest, then labels in the compose
// files, then a single exposed port.
func (s *Service) resolveRules(ctx context.Context, u domain.Universe, dom string) (map[string]domain.RouteRule, error) {
	rules := map[string]domain.RouteRule{}
	add := func(hostKey string, rule domain.RouteRule) error {
		if existing, clash := rules[hostKey]; clash {
			return fmt.Errorf("host %q is claimed twice: by %s/%s and by %s/%s",
				hostKey, existing.App, existing.Service, rule.App, rule.Service)
		}
		rule.Entrypoint = orWeb(rule.Entrypoint)
		rule.Hostname = u.Hostname(hostKey, dom)
		rules[hostKey] = rule
		return nil
	}

	for _, appKey := range s.Manifest.RunnableApps() {
		if s.NothingToRun(u, appKey) {
			continue
		}
		app := s.Manifest.Apps[appKey]
		if len(app.Hosts) > 0 {
			for hostKey, rule := range app.Hosts {
				if err := add(hostKey, domain.RouteRule{
					App: appKey, Service: rule.Service, Port: rule.Port,
					Entrypoint: rule.Entrypoint, Aliases: rule.AlsoMatch,
				}); err != nil {
					return nil, err
				}
			}
			continue
		}

		cfg, err := s.inspect(ctx, u, appKey)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", appKey, err)
		}
		labelled := false
		for _, service := range cfg.Names() {
			for hostKey, rule := range rulesFromLabels(cfg.Services[service].Labels) {
				labelled = true
				rule.App, rule.Service = appKey, service
				if err := add(hostKey, rule); err != nil {
					return nil, err
				}
			}
		}
		if labelled {
			continue
		}
		rule, err := soleExposedPort(appKey, cfg)
		if err != nil {
			return nil, err
		}
		if rule.Port == 0 {
			continue // nothing listens; a worker-only app is fine
		}
		if err := add(appKey, rule); err != nil {
			return nil, err
		}
	}
	return rules, nil
}

// soleExposedPort is the no-configuration case: exactly one service exposing
// exactly one port is reached at the app's own name. Anything ambiguous asks
// for a label rather than guessing.
func soleExposedPort(appKey string, cfg ProjectConfig) (domain.RouteRule, error) {
	var found domain.RouteRule
	for _, name := range cfg.Names() {
		ports := cfg.Services[name].Ports
		switch {
		case len(ports) == 0:
			continue
		case len(ports) > 1:
			return found, fmt.Errorf("%s: service %q exposes %d ports, so which one serves %s is a guess — add a label: multiverse.host.%s: \"<port>\"",
				appKey, name, len(ports), appKey, appKey)
		case found.Port != 0:
			return found, fmt.Errorf("%s: services %q and %q both expose a port — label the one that should answer: multiverse.host.%s: \"<port>\"",
				appKey, found.Service, name, appKey)
		default:
			found = domain.RouteRule{App: appKey, Service: name, Port: ports[0]}
		}
	}
	return found, nil
}

// rulesFromLabels reads one service's multiverse.host.* labels.
func rulesFromLabels(labels map[string]string) map[string]domain.RouteRule {
	out := map[string]domain.RouteRule{}
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		rest, ok := strings.CutPrefix(key, hostLabel)
		if !ok || rest == "" {
			continue
		}
		// "multiverse.host.app" is the port; "multiverse.host.app.entrypoint"
		// is a detail about that same host.
		name, field, hasField := strings.Cut(rest, ".")
		rule := out[name]
		switch {
		case !hasField:
			rule.Port, _ = strconv.Atoi(labels[key])
		case field == "entrypoint":
			rule.Entrypoint = labels[key]
		case field == "aliases":
			for _, alias := range strings.Split(labels[key], ",") {
				if alias = strings.TrimSpace(alias); alias != "" {
					rule.Aliases = append(rule.Aliases, alias)
				}
			}
		}
		out[name] = rule
	}
	for name, rule := range out {
		if rule.Port == 0 {
			delete(out, name) // a host without a port cannot be reached
		}
	}
	return out
}

// publishes reports the services whose labels ask to keep their host ports.
func publishes(cfg ProjectConfig) []string {
	var out []string
	for _, name := range cfg.Names() {
		if cfg.Services[name].Labels[publishLabel] == "true" {
			out = append(out, name)
		}
	}
	return out
}

func (s *Service) inspect(ctx context.Context, u domain.Universe, appKey string) (ProjectConfig, error) {
	st := domain.UniverseState{Name: u.Name, ID: u.ID(), Slug: u.Slug(), Project: u.Project()}
	return s.Runtime.Inspect(ctx, s.request(u, st, appKey, nil))
}

func orWeb(entrypoint string) string {
	if entrypoint == "" {
		return "web"
	}
	return entrypoint
}
