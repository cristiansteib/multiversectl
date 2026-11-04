package domain

import (
	"fmt"
	"sort"
)

// Listener is one port the shared proxy listens on, and for whom: the address
// it is published on, and whether it speaks TLS. The proxy is one for every
// multiverse of the machine, so what it listens on is the union of what the
// multiverses served through it ask for, not the file of whichever started last.
type Listener struct {
	Name string // the proxy's name for the entrypoint
	Port int
	Bind string // the host address it is published on
	TLS  bool
}

// Listeners is what this multiverse asks the proxy to listen on: each of its
// entrypoints, published where its `expose` says.
func (m *Manifest) Listeners() []Listener {
	out := make([]Listener, 0, len(m.Proxy.Entrypoints))
	for _, name := range sortedEntrypoints(m.Proxy.Entrypoints) {
		out = append(out, Listener{Name: name, Port: m.Proxy.Entrypoints[name], Bind: m.BindAddress(), TLS: m.Proxy.TLS[name]})
	}
	return out
}

// PlanListeners adds what a multiverse wants to what the proxy listens on now.
// Nothing that is there is taken away, and a name that is in use keeps its port,
// since the containers already running were labelled with it. A port that is
// wanted by two multiverses is one listener when both want it published the same
// way; when they do not, the wanted way replaces the one in place, and it is the
// caller's job to have checked that nobody running is served by the old one.
// A wanted entrypoint whose name is taken by another port gets "name-port".
func PlanListeners(current, want []Listener) []Listener {
	out := append([]Listener(nil), current...)
	for _, w := range want {
		if i := indexOfPort(out, w.Port); i >= 0 {
			out[i].Bind, out[i].TLS = w.Bind, w.TLS
			continue
		}
		name := w.Name
		for _, l := range out {
			if l.Name == name {
				name = fmt.Sprintf("%s-%d", w.Name, w.Port)
				break
			}
		}
		w.Name = name
		out = append(out, w)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ListenerFor is the proxy's entrypoint on a port, which is how a multiverse's
// own name for one is translated: two multiverses may call one port by two names,
// or two ports by one.
func ListenerFor(listeners []Listener, port int) (Listener, bool) {
	if i := indexOfPort(listeners, port); i >= 0 {
		return listeners[i], true
	}
	return Listener{}, false
}

// KeepListeners is the listeners whose port an entrypoint of one of the given
// multiverses uses: what is left of the proxy when the universes that wanted the
// rest are gone.
func KeepListeners(current []Listener, needed ...*Manifest) []Listener {
	var out []Listener
	for _, l := range current {
		for _, m := range needed {
			if hasPort(m, l.Port) {
				out = append(out, l)
				break
			}
		}
	}
	return out
}

func hasPort(m *Manifest, port int) bool {
	for _, p := range m.Proxy.Entrypoints {
		if p == port {
			return true
		}
	}
	return false
}

func indexOfPort(list []Listener, port int) int {
	for i, l := range list {
		if l.Port == port {
			return i
		}
	}
	return -1
}

// Reach says who can open a listener, in the words a person uses.
func Reach(bind string) string {
	if bind == "127.0.0.1" {
		return "this machine only"
	}
	return "reachable from the network"
}

// ListenerClashes says where two multiverses cannot be served at the same time:
// the same port published two ways (one on the loopback address, one on every
// interface, or one with TLS and one without), which a proxy can only do one of.
// Different entrypoints, names and certificates are not clashes: the proxy
// listens on all of them. aName and bName are what the user calls the multiverses.
func ListenerClashes(aName string, a *Manifest, bName string, b *Manifest) []string {
	var out []string
	for _, x := range a.Listeners() {
		for _, y := range b.Listeners() {
			if x.Port != y.Port {
				continue
			}
			if x.Bind != y.Bind {
				out = append(out, fmt.Sprintf("%s (expose %s) and %s (expose %s) both want port %d, on %s and on %s, and a port can only be published one way",
					aName, exposeOf(a), bName, exposeOf(b), x.Port, x.Bind, y.Bind))
			} else if x.TLS != y.TLS {
				out = append(out, fmt.Sprintf("%s and %s both want port %d, one with TLS and one without, and a port can only be one",
					aName, bName, x.Port))
			}
		}
	}
	return out
}

// ClashFix is what to do about a listener clash, said once where it is shown.
func ClashFix(aName, bName string) string {
	return fmt.Sprintf("give one of %s and %s other proxy entrypoint ports (spec.proxy.entrypoints), give them the same `expose`, or stop the universes of one of them first", aName, bName)
}

// exposeOf is what a multiverse's expose is, the default included.
func exposeOf(m *Manifest) Exposure {
	if m.Expose == "" {
		return ExposeLAN
	}
	return m.Expose
}
