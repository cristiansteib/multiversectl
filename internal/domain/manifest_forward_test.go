package domain

import (
	"strings"
	"testing"
)

func TestValidatePortForward(t *testing.T) {
	m := func(keys ...string) *Manifest {
		return &Manifest{
			Name: "acme", Domain: "auto", Expose: ExposeLAN,
			Proxy: Proxy{Entrypoints: map[string]int{"web": 80}},
			Repos: map[string]Repo{"api": {URL: "x", Runtime: RuntimeCompose}},
			Apps:  map[string]App{"api": {Repo: "api", PortForward: keys}},
		}
	}
	if err := m("api", "devapi").Validate(); err != nil {
		t.Fatal(err)
	}
	if err := m("Not A Key").Validate(); err == nil || !strings.Contains(err.Error(), "portForward") {
		t.Fatalf("a bad host key must be named; got %v", err)
	}
}
