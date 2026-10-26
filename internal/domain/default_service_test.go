package domain

import (
	"strings"
	"testing"
)

func TestDefaultServiceMustBeOneOfTheAppsListedServices(t *testing.T) {
	m := &Manifest{
		Name: "acme", Domain: "example.test", Expose: ExposeLocal,
		Repos: map[string]Repo{"api": {}},
		Apps:  map[string]App{"api": {Repo: "api", Services: []string{"web", "db"}, DefaultService: "worker"}},
	}
	err := m.Validate()
	if err == nil || !strings.Contains(err.Error(), "apps.api.defaultService") {
		t.Fatalf("err = %v", err)
	}
	app := m.Apps["api"]
	app.DefaultService = "web"
	m.Apps["api"] = app
	if err := m.Validate(); err != nil && strings.Contains(err.Error(), "defaultService") {
		t.Fatalf("a listed service was refused: %v", err)
	}
}
