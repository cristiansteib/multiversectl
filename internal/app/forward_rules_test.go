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
	rules, err := rulesFromLabels(map[string]string{"dev.multiversectl.host.api": "8001", "dev.multiversectl.host.rpc": "4317/tcp"})
	if _, kept := rules["api"]; err == nil || !kept || len(rules) != 1 {
		t.Errorf("a bad label must cost only its own host: %+v, %v", rules, err)
	}
	rules, err = rulesFromLabels(map[string]string{"dev.multiversectl.host.api": " 8001 ", "dev.multiversectl.publish": "true"})
	if err != nil || rules["api"].Port != 8001 {
		t.Errorf("a good label: %+v, %v", rules, err)
	}
}

// What makes a resolved address unreachable costs that host its address, with
// a warning naming it, and no other host: a start resolves every app, ones on
// demand included, so failing would take down apps the mistake is not in.
func TestAnUnreachableHostIsSaidAndLeftOut(t *testing.T) {
	for name, tc := range map[string]struct {
		universe string
		hosts    map[string]domain.HostRule
		want     string
		kept     int
	}{
		"an alias of no host": {"3", map[string]domain.HostRule{
			"api": {Service: "web", Port: 8000, AlsoMatch: []string{"adm"}},
		}, `alsoMatch names "adm"`, 1},
		"an entrypoint the proxy lacks": {"3", map[string]domain.HostRule{
			"api": {Service: "web", Port: 8000, Entrypoint: "devapi"}, "ok": {Service: "web", Port: 8000},
		}, `no proxy entrypoint "devapi"`, 1},
		"a hostname part over 63": {strings.Repeat("b", 40), map[string]domain.HostRule{
			"customer-portal-backend": {Service: "web", Port: 8000}, "ok": {Service: "web", Port: 8000},
		}, "over the 63", 1},
	} {
		manifest := &domain.Manifest{
			Name: "acme", Domain: "example.test", Expose: domain.ExposeLAN,
			Proxy: domain.Proxy{Entrypoints: map[string]int{"web": 80}},
			Repos: map[string]domain.Repo{"api": {}},
			Apps:  map[string]domain.App{"api": {Repo: "api", Hosts: tc.hosts}},
		}
		out := &warnings{}
		svc := New(Deps{System: fakeSystem{}, Files: &tempFiles{}, Out: out}, "/ws", manifest, "test")
		rules, err := svc.resolveRules(context.Background(), svc.Universe(tc.universe), "example.test")
		if err != nil || len(rules) != tc.kept || !strings.Contains(strings.Join(out.warned, "\n"), tc.want) {
			t.Errorf("%s: rules = %v, err = %v, warned %q; want %d kept and %q said", name, rules, err, out.warned, tc.kept, tc.want)
		}
	}
}
