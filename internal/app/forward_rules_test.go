package app

import (
	"testing"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// A host asks to be reachable by IP and port with its own portForward label.
func TestPortForwardLabel(t *testing.T) {
	rules := rulesFromLabels(map[string]string{
		"dev.multiversectl.host.api":             "8001",
		"dev.multiversectl.host.api.portForward": "auto",
		"dev.multiversectl.host.devapi":          "8001",
	})
	if rules["api"].PortForward != domain.PortForwardAuto || rules["devapi"].PortForward != "" {
		t.Fatalf("label not read per host: %+v", rules)
	}
}

func TestSchemeLabelIsReadPerHostAndCheckedWhenResolved(t *testing.T) {
	rules := rulesFromLabels(map[string]string{
		"dev.multiversectl.host.rpc":        "4317",
		"dev.multiversectl.host.rpc.scheme": "h2c",
		"dev.multiversectl.host.web":        "8000",
	})
	if rules["rpc"].Scheme != "h2c" || rules["web"].Scheme != "" {
		t.Fatalf("scheme not read per host: %+v", rules)
	}
}
