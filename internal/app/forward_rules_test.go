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
