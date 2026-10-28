package domain

import (
	"strings"
	"testing"
)

// A template is read from the shared clone, so one that points out of it, or
// names something that is not a secret, is refused when the manifest loads.
func TestASecretTemplateMustBeInsideTheRepositoryAndForASecret(t *testing.T) {
	for template, want := range map[string]string{
		"../outside":  "inside the repository",
		"/etc/passwd": "inside the repository",
		".env":        "from itself",
	} {
		m := &Manifest{Name: "acme", Domain: "example.test", Expose: ExposeLocal, Repos: map[string]Repo{"api": {}},
			Apps: map[string]App{"api": {Repo: "api", Secrets: []string{".env"}, SecretTemplates: map[string]string{".env": template}}}}
		if err := m.Validate(); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("template %q: err = %v, want %q", template, err, want)
		}
	}
	m := &Manifest{Name: "acme", Domain: "example.test", Expose: ExposeLocal, Repos: map[string]Repo{"api": {}},
		Apps: map[string]App{"api": {Repo: "api", Secrets: []string{".env"}, SecretTemplates: map[string]string{"other": "x"}}}}
	if err := m.Validate(); err == nil || !strings.Contains(err.Error(), "not one of its secrets") {
		t.Errorf("a template for a file that is no secret: err = %v", err)
	}
	for _, ok := range []string{"", "env.local", "config/env.sample"} {
		m.Apps["api"] = App{Repo: "api", Secrets: []string{".env"}, SecretTemplates: map[string]string{".env": ok}}
		if err := m.Validate(); err != nil && strings.Contains(err.Error(), "secretTemplates") {
			t.Errorf("template %q was refused: %v", ok, err)
		}
	}
}
