package app

import (
	"context"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// servedMultiverse is a multiverse that has containers running, which is what
// makes the shared proxy's listeners its own: the universes of it that run, and
// the manifest that says what they need from the proxy.
type servedMultiverse struct {
	Name      string
	Manifest  *domain.Manifest
	Universes []string
}

// who names the running universes the way a person would look for them.
func (m servedMultiverse) who() string {
	switch len(m.Universes) {
	case 0:
		return m.Name
	case 1:
		return fmt.Sprintf("universe %s of %s", m.Universes[0], m.Name)
	}
	return fmt.Sprintf("universes %s of %s", strings.Join(m.Universes, ", "), m.Name)
}

// servedMultiverses are the multiverses of this machine with containers running.
// A multiverse is told apart by where its containers were started from, so a
// container that carries no such label is not counted; and one whose manifest
// cannot be read has no name to look its containers up by. Its own multiverse is
// left out unless self is set.
func (s *Service) servedMultiverses(ctx context.Context, self bool) []servedMultiverse {
	var out []servedMultiverse
	if self && s.Manifest != nil {
		if m, ok := s.runningIn(ctx, s.Manifest.Name, s.Multiverse, s.Manifest); ok {
			out = append(out, m)
		}
	}
	if s.Config == nil || s.Manifests == nil {
		return out
	}
	cfg, err := s.Config.Load()
	if err != nil {
		return out
	}
	for _, name := range slices.Sorted(maps.Keys(cfg.Multiverses)) {
		entry := cfg.Multiverses[name]
		if entry.Root == "" || samePath(entry.Root, s.Multiverse) || samePath(entry.Manifest, s.Manifest.Path) {
			continue
		}
		other, err := s.Manifests.Load(entry.Manifest)
		if err != nil {
			continue
		}
		if m, ok := s.runningIn(ctx, name, entry.Root, other); ok {
			out = append(out, m)
		}
	}
	return out
}

func (s *Service) runningIn(ctx context.Context, name, root string, m *domain.Manifest) (servedMultiverse, bool) {
	containers, err := s.Runtime.Containers(ctx, m.Name)
	if err != nil {
		return servedMultiverse{}, false
	}
	served := servedMultiverse{Name: name, Manifest: m}
	found := false
	for _, c := range containers {
		if !isRunning(c) {
			continue
		}
		for _, dir := range []string{c.WorkingDir, c.ConfigFile} {
			if !within(dir, root) {
				continue
			}
			found = true
			if id := universeIn(root, dir); id != "" && !slices.Contains(served.Universes, id) {
				served.Universes = append(served.Universes, id)
			}
			break
		}
	}
	slices.Sort(served.Universes)
	return served, found
}

// universeIn is the universe a path inside a multiverse's root belongs to:
// universes/<id>/<repo>/...
func universeIn(root, path string) string {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil {
		return ""
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) >= 2 && parts[0] == "universes" {
		return parts[1]
	}
	return ""
}

func manifestsOf(served []servedMultiverse) []*domain.Manifest {
	out := make([]*domain.Manifest, 0, len(served))
	for _, m := range served {
		out = append(out, m.Manifest)
	}
	return out
}

// listenerClashes are the reasons this multiverse cannot be served by the proxy
// at the same time as the universes that run now, each naming both sides and
// what to do. who is how this multiverse is called in them.
func listenerClashes(mine *domain.Manifest, myName, who string, served []servedMultiverse) []string {
	var out []string
	for _, o := range served {
		for _, c := range domain.ListenerClashes(who, mine, o.who(), o.Manifest) {
			out = append(out, c+"; "+domain.ClashFix(myName, o.Name))
		}
	}
	return out
}

// ProxyConflicts says, before anything is built or started, what stops this
// multiverse from being served by the proxy while the universes that run now
// are. Nothing is returned when they coexist, which is the usual case: the proxy
// listens on the union of what every running multiverse asks for.
func (s *Service) ProxyConflicts(ctx context.Context) []string {
	return s.proxyConflictsFor(ctx, "this multiverse")
}

// proxyConflictsFor is ProxyConflicts with this side called who, such as
// "universe 3 of beta", so a message names both universes.
func (s *Service) proxyConflictsFor(ctx context.Context, who string) []string {
	return listenerClashes(s.Manifest, s.Manifest.Name, who, s.servedMultiverses(ctx, false))
}

// proxyChange describes how the listeners that are there are moved by the plan,
// or "" when nothing that is there changes; a listener that is only added moves
// nothing.
func proxyChange(current, plan []domain.Listener) string {
	var changes []string
	for _, c := range current {
		l, ok := domain.ListenerFor(plan, c.Port)
		if !ok {
			continue
		}
		if l.Bind != c.Bind {
			changes = append(changes, fmt.Sprintf("port %d published on %s instead of %s", c.Port, l.Bind, c.Bind))
		}
		if l.TLS != c.TLS {
			changes = append(changes, fmt.Sprintf("entrypoint %s %s TLS", c.Name, map[bool]string{true: "with", false: "without"}[l.TLS]))
		}
	}
	return strings.Join(changes, ", ")
}

// addedListeners names the ports the plan adds to what is there.
func addedListeners(current, plan []domain.Listener) string {
	var added []string
	for _, l := range plan {
		if _, ok := domain.ListenerFor(current, l.Port); !ok {
			added = append(added, fmt.Sprintf("%s:%d on %s", l.Name, l.Port, l.Bind))
		}
	}
	return strings.Join(added, ", ")
}

// mergeCertificates is the certificates of both lists, once each.
func mergeCertificates(have, want []domain.Certificate) []domain.Certificate {
	out := append([]domain.Certificate(nil), have...)
	for _, c := range want {
		if !slices.Contains(out, c) {
			out = append(out, c)
		}
	}
	return out
}

// onProxyListeners gives each route the name the proxy has for its port. A
// multiverse's entrypoint is a port; the proxy may know that port by the name
// another multiverse gave it, and the router's label has to use that one.
func (s *Service) onProxyListeners(ctx context.Context, routes []Route) []Route {
	status, err := s.Proxy.Status(ctx)
	if err != nil || len(status.Listeners) == 0 {
		return routes
	}
	out := append([]Route(nil), routes...)
	for i, r := range out {
		if l, ok := domain.ListenerFor(status.Listeners, s.Manifest.Proxy.Entrypoints[r.Entrypoint]); ok {
			out[i].Entrypoint = l.Name
		}
	}
	return out
}

// trimProxy takes off the proxy what no running multiverse needs once a universe
// has stopped: a port opened on the network for a multiverse that is no longer
// running should not stay open under one that keeps to the loopback address. With
// nothing running the proxy is left as it is, as it always was.
func (s *Service) trimProxy(ctx context.Context) {
	if s.Proxy == nil {
		return
	}
	proxyMu.Lock()
	defer proxyMu.Unlock()
	status, err := s.Proxy.Status(ctx)
	if err != nil || !status.Running {
		return
	}
	served := s.servedMultiverses(ctx, true)
	if len(served) == 0 {
		return
	}
	keep := domain.KeepListeners(status.Listeners, manifestsOf(served)...)
	if len(keep) == len(status.Listeners) {
		return
	}
	if err := s.Proxy.EnsureUp(ctx, ProxyRequest{Network: s.proxyNetwork(), Listeners: keep, Certificates: status.Certificates}); err != nil {
		s.Out.Warn("the shared proxy keeps listening on ports no running universe needs: %v", err)
		return
	}
	s.Out.Info("the shared proxy stopped listening on %s: no running universe needs it", addedListeners(keep, status.Listeners))
}

func (s *Service) proxyNetwork() string {
	if s.Manifest.Proxy.Network == "" {
		return "multiversenet"
	}
	return s.Manifest.Proxy.Network
}

// describeListeners says what the proxy listens on and who can reach each port.
func describeListeners(listeners []domain.Listener) string {
	out := make([]string, len(listeners))
	for i, l := range listeners {
		out[i] = fmt.Sprintf("%s:%d on %s (%s)", l.Name, l.Port, l.Bind, strings.ToLower(domain.Reach(l.Bind)))
	}
	return strings.Join(out, ", ")
}
