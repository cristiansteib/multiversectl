package app

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// Labels a compose file uses to say how it is reached. The key names the host,
// the value is the port it listens on:
//
//	labels:
//	  dev.multiversectl.host.app: "4200"              # app-u3.<domain> -> 4200
//	  dev.multiversectl.host.api: "8001"
//	  dev.multiversectl.host.api.entrypoint: devapi   # default: web
//	  dev.multiversectl.host.api.aliases: app,adm     # same service, other hostnames
//	  dev.multiversectl.host.api.portForward: auto    # also reachable at <ip>:<free port>
//	  dev.multiversectl.host.rpc.scheme: h2c          # a gRPC service: the proxy speaks HTTP/2 to it
//	  dev.multiversectl.publish: "true"               # keep my published ports
//
// With no labels at all, a service that exposes exactly one port is reached at
// the app's own name, so the common case needs nothing.
const (
	hostLabel    = "dev.multiversectl.host."
	publishLabel = "dev.multiversectl.publish"
	// envLabel lets a service ask for a value only the universe knows:
	//
	//   dev.multiversectl.env.DJANGO_CSRF_TRUSTED_ORIGINS: origins
	//
	// A label rather than an ${MULTIVERSE_ORIGINS} in the service's own
	// environment, because a compose file is also run by hand: a reference
	// resolves to the empty string when multiversectl is not driving, while a
	// label is inert metadata until it is.
	envLabel = "dev.multiversectl.env."
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
	srcForward  = "forward:" // one host key's address by IP and port, when it is forwarded
)

// separators are what a list source (origins, hosts) can be joined with, named
// after the source and a colon: `origins:space`. A framework decides the shape
// of its own setting (Django's CORS_ALLOWED_ORIGINS is a space-separated list
// where CSRF_TRUSTED_ORIGINS is a comma-separated one), so the tool names the
// shapes and does not guess. Comma is the default.
var separators = map[string]string{"comma": ",", "space": " ", "semicolon": ";", "newline": "\n"}

// serviceEnv resolves the dev.multiversectl.env.* labels of every service of an app.
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
	if strings.Contains(source, "+") {
		return s.combineSources(source, st, hosts, origins)
	}
	if list, sep, ok := strings.Cut(source, ":"); ok && (list == srcOrigins || list == srcHosts) {
		joiner, known := separators[sep]
		if !known {
			return "", fmt.Errorf("unknown separator %q for %s: want %s", sep, list, strings.Join(slices.Sorted(maps.Keys(separators)), ", "))
		}
		if list == srcOrigins {
			return strings.Join(origins, joiner), nil
		}
		return strings.Join(hosts, joiner), nil
	}
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
			return "", fmt.Errorf("no host %q in this multiverse", key)
		}
		return s.hostOf(st, key, hostname), nil
	}
	if key, ok := strings.CutPrefix(source, srcURL); ok {
		if _, known := st.Hosts[key]; !known {
			return "", fmt.Errorf("no host %q in this multiverse", key)
		}
		return s.originOf(st, key, st.Hosts[key]), nil
	}
	if key, ok := strings.CutPrefix(source, srcForward); ok {
		rule, known := st.Rules[key]
		if !known {
			return "", fmt.Errorf("no host %q in this multiverse", key)
		}
		if rule.ForwardURL == "" {
			return "", fmt.Errorf("host %q is not forwarded: set portForward: auto on it", key)
		}
		return rule.ForwardURL, nil
	}
	return "", fmt.Errorf("unknown source %q: want %s, %s (each also as origins:space), %s, %s, %s, host:<key>, url:<key> or forward:<key>, or several joined with +",
		source, srcOrigins, srcHosts, srcDomain, srcUniverse, srcSlug)
}

// resolveRules works out every address of a universe, in order of how
// explicit it is: hosts declared in the manifest, then labels in the compose
// files, then a single exposed port.
//
// A host that could not be reached however it is routed (a label that cannot
// be read, an entrypoint the proxy lacks, a hostname too long to resolve) is
// left out with a warning naming it, rather than failing: every universe is
// resolved for every app, including ones on demand that this start will not
// touch, and one bad label should cost its own address and no other.
func (s *Service) resolveRules(ctx context.Context, u domain.Universe, dom string) (map[string]domain.RouteRule, error) {
	rules := map[string]domain.RouteRule{}
	add := func(hostKey string, rule domain.RouteRule) error {
		if existing, clash := rules[hostKey]; clash {
			return fmt.Errorf("host %q is claimed twice: by %s/%s and by %s/%s",
				hostKey, existing.App, existing.Service, rule.App, rule.Service)
		}
		if rule.Scheme != "" && !slices.Contains(domain.Schemes, rule.Scheme) {
			return fmt.Errorf("host %q: scheme %q: want %s", hostKey, rule.Scheme, strings.Join(domain.Schemes, ", "))
		}
		rule.Entrypoint = orWeb(rule.Entrypoint)
		if _, ok := s.Manifest.Proxy.Entrypoints[rule.Entrypoint]; !ok {
			s.Out.Warn("host %s (%s/%s) gets no address: no proxy entrypoint %q; want one of spec.proxy.entrypoints (%s)",
				hostKey, rule.App, rule.Service, rule.Entrypoint, strings.Join(slices.Sorted(maps.Keys(s.Manifest.Proxy.Entrypoints)), ", "))
			return nil
		}
		// With no domain the name is only the proxy's own, and no resolver sees it.
		if n := len(hostKey) + 1 + len(u.Slug()); n > domain.DNSLabelLimit && !s.Manifest.IPOnly() {
			s.Out.Warn("host %s (%s/%s) gets no address: %s-%s is %d characters, over the %d a hostname's part has room for; shorten the host or the universe's name",
				hostKey, rule.App, rule.Service, hostKey, u.Slug(), n, domain.DNSLabelLimit)
			return nil
		}
		if rule.PortForward != "" && !domain.Forwarded(rule.PortForward) {
			return fmt.Errorf("host %q: portForward %q: want %s or %s", hostKey, rule.PortForward, domain.PortForwardAuto, domain.PortForwardOnly)
		}
		if s.Manifest.IPOnly() {
			// The address is the forwarded port: without a hostname that is the
			// only way to tell one universe's copy of a host from another's.
			rule.PortForward = domain.PortForwardOnly
		}
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
					Entrypoint: rule.Entrypoint, Aliases: rule.AlsoMatch, PortForward: rule.PortForward, Scheme: rule.Scheme,
					Ready: rule.Ready, Probe: rule.Probe,
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
			fromLabels, err := rulesFromLabels(cfg.Services[service].Labels)
			if err != nil {
				s.Out.Warn("%s: service %s: %v", appKey, service, err)
			}
			for hostKey, rule := range fromLabels {
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
	// An alias is a host key, and one that names no host would quietly stop a
	// route answering under a hostname someone relies on. It may be a host of
	// a service behind a profile, which is not read here, so it is said and not
	// refused.
	for _, hostKey := range mapKeys(rules) {
		for _, alias := range rules[hostKey].Aliases {
			if _, ok := rules[alias]; !ok {
				s.Out.Warn("host %s: alsoMatch names %q, which no app of this universe answers on; it does not answer under that name", hostKey, alias)
			}
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
			return found, fmt.Errorf("%s: service %q exposes %d ports, so which one serves %s is a guess — add a label: dev.multiversectl.host.%s: \"<port>\"",
				appKey, name, len(ports), appKey, appKey)
		case found.Port != 0:
			return found, fmt.Errorf("%s: services %q and %q both expose a port — label the one that should answer: dev.multiversectl.host.%s: \"<port>\"",
				appKey, found.Service, name, appKey)
		default:
			found = domain.RouteRule{App: appKey, Service: name, Port: ports[0]}
		}
	}
	return found, nil
}

// rulesFromLabels reads one service's dev.multiversectl.host.* labels. A label
// it cannot use is reported, with the hosts that could be read, not a host that
// quietly is not there: a typo in a field or a port written as "8080/tcp"
// would otherwise leave nothing to say why the address does not answer.
func rulesFromLabels(labels map[string]string) (map[string]domain.RouteRule, error) {
	out := map[string]domain.RouteRule{}
	var errs []string
	badPort := map[string]bool{} // said once, not again as a missing port
	for _, key := range mapKeys(labels) {
		rest, ok := strings.CutPrefix(key, hostLabel)
		if !ok || rest == "" {
			continue
		}
		// "dev.multiversectl.host.app" is the port; "dev.multiversectl.host.app.entrypoint"
		// is a detail about that same host.
		name, field, hasField := strings.Cut(rest, ".")
		if !domain.ValidKey(name) {
			errs = append(errs, fmt.Sprintf("%s: host %q: want lowercase letters, digits and dashes, as it becomes part of a hostname", key, name))
			continue
		}
		rule := out[name]
		value := labels[key]
		switch {
		case !hasField:
			port, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || port <= 0 || port > 65535 {
				errs = append(errs, fmt.Sprintf("%s: %q is not a port: want the number the service listens on inside its container", key, value))
				badPort[name] = true
				continue
			}
			rule.Port = port
		case field == "entrypoint":
			rule.Entrypoint = value
		case field == "portForward":
			rule.PortForward = value
		case field == "scheme":
			rule.Scheme = value
		case field == "ready":
			if !domain.ValidReady(value) {
				errs = append(errs, fmt.Sprintf("%s: %q: want true, false, tcp or a path such as /healthz", key, value))
				continue
			}
			rule.Ready = value
		case field == "probe":
			if !slices.Contains(domain.Probes, value) {
				errs = append(errs, fmt.Sprintf("%s: %q: want %s", key, value, strings.Join(domain.Probes, ", ")))
				continue
			}
			rule.Probe = value
		case field == "aliases":
			for _, alias := range strings.Split(value, ",") {
				if alias = strings.TrimSpace(alias); alias != "" {
					rule.Aliases = append(rule.Aliases, alias)
				}
			}
		default:
			errs = append(errs, fmt.Sprintf("%s: no field %q: want %s", key, field, strings.Join(hostLabelFields, ", ")))
			continue
		}
		out[name] = rule
	}
	for _, name := range mapKeys(out) {
		if out[name].Port == 0 {
			if !badPort[name] {
				errs = append(errs, fmt.Sprintf("%s%s: required — the port the host's other labels are about", hostLabel, name))
			}
			delete(out, name)
		}
	}
	if len(errs) > 0 {
		return out, errors.New(strings.Join(errs, "; "))
	}
	return out, nil
}

// hostLabelFields are what a dev.multiversectl.host.<name>.<field> label can say.
var hostLabelFields = []string{"entrypoint", "aliases", "portForward", "scheme", "ready", "probe"}

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

// combineSources resolves several sources joined with "+", for a setting that
// has to list addresses of more than one kind: `forward:web+url:web`. Each
// source gives its items (a list source all of its own), duplicates are dropped
// with the first kept, and the order is the one written. They are joined with a
// separator named after the last one, `forward:web+url:web:space`, comma by
// default. A forwarded address that has not been allocated yet is left out and
// is not an error, as in origins; a host that does not exist is.
func (s *Service) combineSources(source string, st domain.UniverseState, hosts, origins []string) (string, error) {
	terms := strings.Split(source, "+")
	joiner := separators["comma"]
	last := len(terms) - 1
	if i := strings.LastIndex(terms[last], ":"); i >= 0 {
		head, sep := terms[last][:i], terms[last][i+1:]
		_, named := separators[sep]
		// "url:web" ends in a host that may be called anything, so only the
		// three-part form "url:web:space" ends in a separator; a source with no
		// key has nothing to be mistaken for.
		if named && (slices.Contains([]string{srcOrigins, srcHosts, srcDomain, srcUniverse, srcSlug}, head) || strings.Count(terms[last], ":") >= 2) {
			joiner, terms[last] = separators[sep], head
		}
	}
	var items []string
	for _, term := range terms {
		term = strings.TrimSpace(term)
		switch {
		case term == "":
			return "", fmt.Errorf("%q: an empty source between +", source)
		case term == srcOrigins:
			items = appendAllNew(items, origins)
		case term == srcHosts:
			items = appendAllNew(items, hosts)
		case strings.HasPrefix(term, srcOrigins+":") || strings.HasPrefix(term, srcHosts+":"):
			return "", fmt.Errorf("%q: a separator goes once, at the end of the whole value", term)
		default:
			if key, ok := strings.CutPrefix(term, srcForward); ok {
				if _, known := st.Hosts[key]; known && st.Rules[key].ForwardURL == "" {
					continue
				}
			}
			value, err := s.resolveEnvSource(term, st, hosts, origins)
			if err != nil {
				return "", err
			}
			items = appendNew(items, value)
		}
	}
	return strings.Join(items, joiner), nil
}

func appendAllNew(list, more []string) []string {
	for _, item := range more {
		list = appendNew(list, item)
	}
	return list
}
