package app

import (
	"strings"
	"testing"
)

func TestPickName(t *testing.T) {
	tests := []struct {
		leaf, path string
		taken      map[string]bool
		want       string
	}{
		{"billing", "acme/backend/billing", nil, "billing"},
		{"billing", "acme/backend/billing", map[string]bool{"billing": true}, "backend-billing"},
		{"billing", "acme/backend/billing", map[string]bool{"billing": true, "backend-billing": true}, "acme-backend-billing"},
		{"billing", "billing", map[string]bool{"billing": true}, "billing-2"},
		{"billing", "billing", map[string]bool{"billing": true, "billing-2": true}, "billing-3"},
	}
	for _, tt := range tests {
		if got := pickName(tt.leaf, tt.path, tt.taken); got != tt.want {
			t.Errorf("pickName(%q, %q, %v) = %q, want %q", tt.leaf, tt.path, tt.taken, got, tt.want)
		}
	}
}

func TestRepoOwner(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/acme/billing.git": "acme",
		"git@github.com:acme/billing.git":     "acme",
		"ssh://git@example.test/acme/b.git":   "acme",
		"file:///tmp/x/y.git":                 "",
		"/local/path":                         "",
		"":                                    "",
	} {
		if got := repoOwner(in); got != want {
			t.Errorf("repoOwner(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveFindsANameOrACatalogPath(t *testing.T) {
	c := Config{Workspaces: map[string]WorkspaceEntry{"billing": {Root: "/b", From: "acme/backend/billing"}}}
	for _, key := range []string{"billing", "acme/backend/billing"} {
		name, e, err := c.Resolve(key)
		if err != nil || name != "billing" || e.Root != "/b" {
			t.Errorf("Resolve(%q) = %q %v %v", key, name, e, err)
		}
	}
	if _, _, err := c.Resolve("nope"); err == nil || !strings.Contains(err.Error(), "installed: billing") || !strings.Contains(err.Error(), "install nope") {
		t.Errorf("err = %v; it should list what is installed and say how to add it", err)
	}
}
