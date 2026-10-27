package app

import (
	"testing"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// portForward on an app marks hosts however they were declared, and naming a
// host the app does not have says which ones it has.
func TestPortForwardLabelAndErrors(t *testing.T) {
	rules := rulesFromLabels(map[string]string{
		"dev.multiversectl.host.api":             "8001",
		"dev.multiversectl.host.api.portForward": "auto",
		"dev.multiversectl.host.devapi":          "8001",
	})
	if rules["api"].PortForward != domain.PortForwardAuto || rules["devapi"].PortForward != "" {
		t.Fatalf("label not read per host: %+v", rules)
	}
	if got := hostsOf(map[string]domain.RouteRule{"api": {App: "api"}, "ui": {App: "ui"}, "mail": {App: "api"}}, "api"); got != "api, mail" {
		t.Fatalf("hostsOf = %q", got)
	}
}
