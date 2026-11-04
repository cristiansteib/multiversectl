package domain

import (
	"fmt"
	"strings"
	"testing"
)

func mv(expose Exposure, eps map[string]int) *Manifest {
	return &Manifest{Expose: expose, Proxy: Proxy{Entrypoints: eps}}
}

func names(ls []Listener) string {
	var out []string
	for _, l := range ls {
		out = append(out, fmt.Sprintf("%s=%s:%d", l.Name, l.Bind, l.Port))
	}
	return strings.Join(out, " ")
}

// A local and a lan multiverse on ports of their own are served by one proxy that
// listens on both, each where its own expose says; neither moves the other.
func TestLocalAndLanMultiversesShareTheProxyOnDistinctPorts(t *testing.T) {
	local := mv(ExposeLocal, map[string]int{"web": 80})
	lan := mv(ExposeLAN, map[string]int{"web": 8080})

	got := PlanListeners(nil, local.Listeners())
	got = PlanListeners(got, lan.Listeners())
	if want := "web=127.0.0.1:80 web-8080=0.0.0.0:8080"; names(got) != want {
		t.Errorf("listeners = %s, want %s", names(got), want)
	}
	if c := ListenerClashes("alpha", local, "beta", lan); len(c) != 0 {
		t.Errorf("clashes = %v", c)
	}
}

// The name of a listener that is already there is kept whoever asks next: the
// containers that run were labelled with it.
func TestPlanKeepsTheNamesAlreadyInUse(t *testing.T) {
	current := []Listener{{Name: "web", Port: 80, Bind: "127.0.0.1"}, {Name: "devapi", Port: 8001, Bind: "127.0.0.1"}}
	other := mv(ExposeLocal, map[string]int{"http": 80, "devapi": 9001})
	got := PlanListeners(current, other.Listeners())
	if want := "devapi=127.0.0.1:8001 devapi-9001=127.0.0.1:9001 web=127.0.0.1:80"; names(got) != want {
		t.Errorf("listeners = %s, want %s", names(got), want)
	}
	if l, ok := ListenerFor(got, 80); !ok || l.Name != "web" {
		t.Errorf("port 80 is %+v: a multiverse that calls it http must use the proxy's name", l)
	}
	if again := PlanListeners(got, other.Listeners()); names(again) != names(got) {
		t.Errorf("asking twice changes the plan: %s", names(again))
	}
}

// Taking a universe away leaves the others served: only what no remaining
// multiverse uses goes.
func TestKeepListenersDropsWhatNobodyRunningUses(t *testing.T) {
	local := mv(ExposeLocal, map[string]int{"web": 80})
	lan := mv(ExposeLAN, map[string]int{"web": 8080})
	plan := PlanListeners(PlanListeners(nil, local.Listeners()), lan.Listeners())
	if got := KeepListeners(plan, local); names(got) != "web=127.0.0.1:80" {
		t.Errorf("only alpha left: %s", names(got))
	}
	if got := KeepListeners(plan, lan); names(got) != "web-8080=0.0.0.0:8080" {
		t.Errorf("only beta left: %s", names(got))
	}
	if got := KeepListeners(plan, local, lan); len(got) != 2 {
		t.Errorf("both left: %s", names(got))
	}
}

func TestSamePortPublishedTwoWaysIsAClashWithAWayOut(t *testing.T) {
	local := mv(ExposeLocal, map[string]int{"web": 80})
	lan := mv(ExposeLAN, map[string]int{"web": 80})
	got := ProxyClashes("alpha", local, "beta", lan)
	if len(got) != 1 || !strings.Contains(got[0], "alpha (expose local) and beta (expose lan) both want port 80") ||
		!strings.Contains(got[0], "other proxy entrypoint ports") {
		t.Fatalf("clashes = %v", got)
	}
	tls := mv(ExposeLocal, map[string]int{"web": 80})
	tls.Proxy.TLS = map[string]bool{"web": true}
	if got := ListenerClashes("a", local, "b", tls); len(got) != 1 || !strings.Contains(got[0], "TLS") {
		t.Errorf("clashes = %v", got)
	}
	// What used to be a clash and is not any more: one has an entrypoint the other lacks.
	if got := ProxyClashes("a", mv(ExposeLocal, map[string]int{"web": 80, "devapi": 8001}), "b", local); len(got) != 0 {
		t.Errorf("clashes = %v", got)
	}
}

func TestDifferentProxyNetworksStillClash(t *testing.T) {
	a, b := mv(ExposeLocal, map[string]int{"web": 80}), mv(ExposeLocal, map[string]int{"web": 80})
	b.Proxy.Network = "other"
	if got := ProxyClashes("a", a, "b", b); len(got) != 1 || !strings.Contains(got[0], "different proxy networks") {
		t.Errorf("clashes = %v", got)
	}
}
