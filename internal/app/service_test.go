package app

import (
	"context"
	"reflect"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

func TestIsNewer(t *testing.T) {
	tests := []struct {
		name            string
		latest, current string
		want            bool
	}{
		{"patch bump", "1.2.4", "1.2.3", true},
		{"minor beats patch", "1.3.0", "1.2.9", true},
		{"numeric not lexical", "1.10.0", "1.9.0", true},
		{"leading v on both", "v2.0.0", "v1.9.9", true},
		{"equal", "1.2.3", "v1.2.3", false},
		{"older release", "1.2.2", "1.2.3", false},
		{"dev is always older", "0.0.1", "dev", true},
		{"pre-release suffix ignored", "1.2.3", "1.2.3-rc1", false},
		{"unparsable latest", "nightly", "1.0.0", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isNewer(tt.latest, tt.current); got != tt.want {
				t.Errorf("isNewer(%q, %q) = %v, want %v", tt.latest, tt.current, got, tt.want)
			}
		})
	}
}

type fakeSystem struct {
	System
	env map[string]string
}

func (fakeSystem) UID() int { return 1000 }
func (fakeSystem) GID() int { return 1001 }
func (f fakeSystem) Getenv(key string) string {
	return f.env[key]
}

type fakeStore struct {
	Store
	st domain.UniverseState
}

func (f fakeStore) Load(string) (domain.UniverseState, error) { return f.st, nil }

func TestEnv(t *testing.T) {
	manifest := &domain.Manifest{
		Name:  "acme",
		Repos: map[string]domain.Repo{"api": {URL: "git@example.test:acme/api.git"}},
		Env:   map[string]string{"HOST_UID": "{{uid}}", "TOKEN": "${ACME_TOKEN}"},
	}
	st := domain.UniverseState{
		Name:    "3",
		ID:      "3",
		Slug:    "u3",
		Number:  3,
		Domain:  "example.test",
		Project: "acme-u3",
		Hosts:   map[string]string{"api": "api-u3.example.test", "admin-ui": "admin-ui-u3.example.test"},
	}
	svc := New(Deps{
		System: fakeSystem{env: map[string]string{"ACME_TOKEN": "s3cret"}},
		Store:  fakeStore{st: st},
	}, "/ws", manifest, "test")

	got, err := svc.Env(context.Background(), "3")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"COMPOSE_PROJECT_NAME": "acme-u3",
		"UNIVERSE":             "3",
		"UNIVERSE_SLUG":        "u3",
		"UNIVERSE_DIR":         "/ws/universes/3",
		"MULTIVERSE_DOMAIN":    "example.test",
		"API_HOST":             "api-u3.example.test",
		"API_URL":              "http://api-u3.example.test",
		"ADMIN_UI_HOST":        "admin-ui-u3.example.test",
		"ADMIN_UI_URL":         "http://admin-ui-u3.example.test",
		"MULTIVERSE_HOSTS":     "admin-ui-u3.example.test,api-u3.example.test",
		"MULTIVERSE_ORIGINS":   "http://admin-ui-u3.example.test,http://api-u3.example.test",
		"MULTIVERSE_PROJECT":   "acme",
		"MULTIVERSE_ROOT":      "/ws",
		"API_DIR":              "/ws/universes/3/api",
		"HOST_UID":             "1000",
		"HOST_GID":             "1001",
		"TOKEN":                "s3cret",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Env =\n%v\nwant\n%v", got, want)
	}
}

// A universe named after a branch is reached by that name, by the directory
// it lives in and by the slug in its hostnames, because the user sees all
// three at different moments.
func TestEnvOfANamedUniverse(t *testing.T) {
	manifest := &domain.Manifest{
		Name:  "acme",
		Repos: map[string]domain.Repo{"api": {URL: "git@example.test:acme/api.git"}},
	}
	st := domain.UniverseState{
		Name: "fix/login", ID: "fix-login", Slug: "fix-login",
		Domain:  "example.test",
		Project: "acme-fix-login",
		Hosts:   map[string]string{"api": "api-fix-login.example.test"},
	}
	svc := New(Deps{System: fakeSystem{}, Store: fakeStore{st: st}}, "/ws", manifest, "test")

	got, err := svc.Env(context.Background(), "fix/login")
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"UNIVERSE":             "fix/login",
		"UNIVERSE_SLUG":        "fix-login",
		"UNIVERSE_DIR":         "/ws/universes/fix-login",
		"COMPOSE_PROJECT_NAME": "acme-fix-login",
		"API_DIR":              "/ws/universes/fix-login/api",
		"API_HOST":             "api-fix-login.example.test",
	} {
		if got[key] != want {
			t.Errorf("%s = %q, want %q", key, got[key], want)
		}
	}
}

// A host answered on a second entrypoint is reached on that port, so its
// origin carries it: an app that trusts MULTIVERSE_ORIGINS would otherwise
// reject the very requests the proxy is routing to it.
func TestOriginsCarryTheEntrypointPort(t *testing.T) {
	manifest := &domain.Manifest{
		Name:  "acme",
		Proxy: domain.Proxy{Entrypoints: map[string]int{"web": 80, "devapi": 8001}},
		Repos: map[string]domain.Repo{"api": {}},
	}
	st := domain.UniverseState{
		Name: "3", ID: "3", Slug: "u3", Domain: "example.test", Project: "acme-u3",
		Hosts: map[string]string{
			"api":    "api-u3.example.test",
			"devapi": "devapi-u3.example.test",
		},
		Rules: map[string]domain.RouteRule{
			"api":    {Entrypoint: "web"},
			"devapi": {Entrypoint: "devapi"},
		},
	}
	svc := New(Deps{System: fakeSystem{}, Store: fakeStore{st: st}}, "/ws", manifest, "test")
	got, err := svc.Env(context.Background(), "3")
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"API_URL":    "http://api-u3.example.test",
		"DEVAPI_URL": "http://devapi-u3.example.test:8001",
		"MULTIVERSE_ORIGINS": "http://api-u3.example.test," +
			"http://devapi-u3.example.test:8001",
	} {
		if got[key] != want {
			t.Errorf("%s = %q, want %q", key, got[key], want)
		}
	}
}

// A service asks for what only the universe knows with a label, and gets it
// resolved; an unknown source is an error rather than an empty variable.
func TestServiceEnvFromLabels(t *testing.T) {
	manifest := &domain.Manifest{
		Name:  "acme",
		Proxy: domain.Proxy{Entrypoints: map[string]int{"web": 80, "devapi": 8001}},
		Repos: map[string]domain.Repo{"api": {}},
	}
	st := domain.UniverseState{
		Name: "fix/login", ID: "fix-login", Slug: "fix-login",
		Domain: "example.test",
		Hosts: map[string]string{
			"api":    "api-fix-login.example.test",
			"devapi": "devapi-fix-login.example.test",
		},
		Rules: map[string]domain.RouteRule{
			"api":    {Entrypoint: "web"},
			"devapi": {Entrypoint: "devapi"},
		},
	}
	svc := New(Deps{System: fakeSystem{}, Store: fakeStore{st: st}}, "/ws", manifest, "test")

	cfg := ProjectConfig{Services: map[string]ServiceConfig{"web": {Labels: map[string]string{
		"dev.multiversectl.env.DJANGO_CSRF_TRUSTED_ORIGINS": "origins",
		"dev.multiversectl.env.DJANGO_ALLOWED_HOSTS":        "hosts",
		"dev.multiversectl.env.API_BASE":                    "url:devapi",
		"dev.multiversectl.env.OWN_HOST":                    "host:api",
		"dev.multiversectl.env.WHICH":                       "universe",
		"dev.multiversectl.host.api":                        "8001",
	}}}}
	got, err := svc.serviceEnv(st, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Service != "web" {
		t.Fatalf("got %+v", got)
	}
	vars := map[string]string{}
	for _, v := range got[0].Vars {
		vars[v.Name] = v.Value
	}
	for name, want := range map[string]string{
		"DJANGO_CSRF_TRUSTED_ORIGINS": "http://api-fix-login.example.test,http://devapi-fix-login.example.test:8001",
		"DJANGO_ALLOWED_HOSTS":        "api-fix-login.example.test,devapi-fix-login.example.test",
		"API_BASE":                    "http://devapi-fix-login.example.test:8001",
		"OWN_HOST":                    "api-fix-login.example.test",
		"WHICH":                       "fix/login",
	} {
		if vars[name] != want {
			t.Errorf("%s = %q, want %q", name, vars[name], want)
		}
	}
	// A routing label must not become a variable.
	if _, leaked := vars["dev.multiversectl.host.api"]; leaked {
		t.Error("a host label leaked into the environment")
	}

	for _, bad := range []string{"nonsense", "host:nope", "url:nope"} {
		cfg.Services["web"] = ServiceConfig{Labels: map[string]string{"dev.multiversectl.env.X": bad}}
		if _, err := svc.serviceEnv(st, cfg); err == nil {
			t.Errorf("%q: want an error naming the problem", bad)
		}
	}
}
