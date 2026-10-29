package app

import (
	"context"
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// A host asks to be reachable by IP and port with its own portForward label.
func TestPortForwardLabel(t *testing.T) {
	rules, err := rulesFromLabels(map[string]string{
		"dev.multiversectl.host.api":             "8001",
		"dev.multiversectl.host.api.portForward": "auto",
		"dev.multiversectl.host.devapi":          "8001",
	})
	if err != nil {
		t.Fatal(err)
	}
	if rules["api"].PortForward != domain.PortForwardAuto || rules["devapi"].PortForward != "" {
		t.Fatalf("label not read per host: %+v", rules)
	}
}

func TestSchemeLabelIsReadPerHostAndCheckedWhenResolved(t *testing.T) {
	rules, err := rulesFromLabels(map[string]string{
		"dev.multiversectl.host.rpc":        "4317",
		"dev.multiversectl.host.rpc.scheme": "h2c",
		"dev.multiversectl.host.web":        "8000",
	})
	if err != nil {
		t.Fatal(err)
	}
	if rules["rpc"].Scheme != "h2c" || rules["web"].Scheme != "" {
		t.Fatalf("scheme not read per host: %+v", rules)
	}
}

// A label that cannot be used is an error naming it: dropping the host instead
// left an address that did not answer and nothing to say why.
func TestABadHostLabelIsAnErrorNotAMissingHost(t *testing.T) {
	for label, want := range map[string]string{
		"dev.multiversectl.host.api=8080/tcp":        "not a port",
		"dev.multiversectl.host.api=${PORT}":         "not a port",
		"dev.multiversectl.host.api=70000":           "not a port",
		"dev.multiversectl.host.My_Api=8080":         "lowercase",
		"dev.multiversectl.host.api.alias=adm":       `no field "alias"`,
		"dev.multiversectl.host.web.entrypoint=web2": "required",
	} {
		key, value, _ := strings.Cut(label, "=")
		labels := map[string]string{key: value}
		if !strings.HasPrefix(key, "dev.multiversectl.host.api") {
			labels["dev.multiversectl.host.api"] = "8001"
		}
		if _, err := rulesFromLabels(labels); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", label, err, want)
		}
	}
	rules, err := rulesFromLabels(map[string]string{"dev.multiversectl.host.api": " 8001 ", "dev.multiversectl.publish": "true"})
	if err != nil || rules["api"].Port != 8001 {
		t.Errorf("a good label: %+v, %v", rules, err)
	}
}

// What makes a resolved address unreachable is refused where the address is
// worked out, wherever the host was declared.
func TestAResolvedHostMustBeReachable(t *testing.T) {
	for name, tc := range map[string]struct {
		universe string
		hosts    map[string]domain.HostRule
		want     string
	}{
		"an alias of no host": {"3", map[string]domain.HostRule{
			"api": {Service: "web", Port: 8000, AlsoMatch: []string{"adm"}},
		}, `alsoMatch names "adm"`},
		"an entrypoint the proxy lacks": {"3", map[string]domain.HostRule{
			"api": {Service: "web", Port: 8000, Entrypoint: "devapi"},
		}, `no proxy entrypoint "devapi"`},
		"a hostname part over 63": {strings.Repeat("b", 40), map[string]domain.HostRule{
			"customer-portal-backend": {Service: "web", Port: 8000},
		}, "over the 63"},
	} {
		manifest := &domain.Manifest{
			Name: "acme", Domain: "example.test", Expose: domain.ExposeLAN,
			Proxy: domain.Proxy{Entrypoints: map[string]int{"web": 80}},
			Repos: map[string]domain.Repo{"api": {}},
			Apps:  map[string]domain.App{"api": {Repo: "api", Hosts: tc.hosts}},
		}
		svc := New(Deps{System: fakeSystem{}, Files: &tempFiles{}}, "/ws", manifest, "test")
		if _, err := svc.resolveRules(context.Background(), svc.Universe(tc.universe), "example.test"); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
	}
}
