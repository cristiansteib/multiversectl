package app

import (
	"context"
	"reflect"
	"strings"
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
			"api":    {Entrypoint: "web", PortForward: "auto", HostPort: 41000, ForwardURL: "http://192.0.2.7:41000"},
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
		"dev.multiversectl.env.CORS_ALLOWED_ORIGINS":        "origins:space",
		"dev.multiversectl.env.ALLOWED_HOSTS_LINES":         "hosts:newline",
		"dev.multiversectl.env.PHONE_URL":                   "forward:api",
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
		// Every address, the forwarded one of a host included.
		"DJANGO_CSRF_TRUSTED_ORIGINS": "http://api-fix-login.example.test,http://192.0.2.7:41000,http://devapi-fix-login.example.test:8001",
		"DJANGO_ALLOWED_HOSTS":        "api-fix-login.example.test,devapi-fix-login.example.test",
		"API_BASE":                    "http://devapi-fix-login.example.test:8001",
		"OWN_HOST":                    "api-fix-login.example.test",
		"WHICH":                       "fix/login",
		// A framework decides the shape of its own list: Django's CORS setting wants
		// spaces where its CSRF one wants commas.
		"CORS_ALLOWED_ORIGINS": "http://api-fix-login.example.test http://192.0.2.7:41000 http://devapi-fix-login.example.test:8001",
		"ALLOWED_HOSTS_LINES":  "api-fix-login.example.test\ndevapi-fix-login.example.test",
		// A client that cannot use the hostname is given the address by IP and port.
		"PHONE_URL": "http://192.0.2.7:41000",
	} {
		if vars[name] != want {
			t.Errorf("%s = %q, want %q", name, vars[name], want)
		}
	}
	// A routing label must not become a variable.
	if _, leaked := vars["dev.multiversectl.host.api"]; leaked {
		t.Error("a host label leaked into the environment")
	}

	for _, bad := range []string{"nonsense", "host:nope", "url:nope", "forward:nope", "forward:devapi", "origins:tab"} {
		cfg.Services["web"] = ServiceConfig{Labels: map[string]string{"dev.multiversectl.env.X": bad}}
		if _, err := svc.serviceEnv(st, cfg); err == nil {
			t.Errorf("%q: want an error naming the problem", bad)
		}
	}
}

// A repository whose work starts on another branch says so once, and every
// universe branches from it and is measured against it.
func TestARepositorysBaseIsWhereItsUniversesStart(t *testing.T) {
	manifest := &domain.Manifest{Name: "acme", Repos: map[string]domain.Repo{"api": {Base: "develop"}, "ui": {}}}
	svc := New(Deps{Git: &codeGit{}, System: fakeSystem{}}, "/ws", manifest, "test")
	for repo, want := range map[string]string{"api": "develop", "ui": "main"} {
		if got, err := svc.baseBranch(context.Background(), repo, "/ws/"+repo); err != nil || got != want {
			t.Errorf("%s: base = %q, %v; want %q", repo, got, err, want)
		}
	}
	if got := svc.followed(context.Background(), "api", "/ws/api", ""); got != "develop" {
		t.Errorf("an unpushed universe follows %q, want its base", got)
	}
}

// An app's env is its own: it is over the multiverse's, and no other app sees it.
func TestAnAppsEnvIsOverTheMultiversesAndOnlyItsOwn(t *testing.T) {
	manifest := &domain.Manifest{Name: "acme", Env: map[string]string{"LOG_LEVEL": "info"},
		Repos: map[string]domain.Repo{"api": {}, "ui": {}},
		Apps:  map[string]domain.App{"api": {Repo: "api", Env: map[string]string{"LOG_LEVEL": "debug"}}, "ui": {Repo: "ui"}}}
	svc := New(Deps{System: fakeSystem{}}, "/ws", manifest, "test")
	u := svc.Universe("3")
	st := domain.UniverseState{Name: "3"}
	if got := svc.appEnv(u, st, "api")["LOG_LEVEL"]; got != "debug" {
		t.Errorf("api: LOG_LEVEL = %q", got)
	}
	if got := svc.appEnv(u, st, "ui")["LOG_LEVEL"]; got != "info" {
		t.Errorf("ui: LOG_LEVEL = %q", got)
	}
}

// Starting from a multiverse with other certificates, or another entrypoint's
// TLS, takes the proxy from whoever set it up: that is said, like a port is.
func TestAProxyChangeNamesCertificatesAndTLS(t *testing.T) {
	status := ProxyStatus{Bind: "0.0.0.0", Entrypoints: map[string]int{"web": 443}, TLS: map[string]bool{"web": true}, Certificates: []string{"/a.pem"}}
	want := ProxyRequest{Bind: "0.0.0.0", Entrypoints: map[string]int{"web": 443}}
	got := proxyChange(status, want)
	if !strings.Contains(got, "certificates none instead of /a.pem") || !strings.Contains(got, "web without TLS") {
		t.Errorf("change = %q", got)
	}
	want.TLS, want.Certificates = map[string]bool{"web": true}, []domain.Certificate{{CertFile: "/a.pem", KeyFile: "/k.pem"}}
	if got := proxyChange(status, want); got != "" {
		t.Errorf("no change, yet %q", got)
	}
}

// One label can name several sources, joined with +: each resolved, duplicates
// dropped, the order kept, joined with the separator at the end.
func TestEnvLabelSourcesCanBeCombined(t *testing.T) {
	manifest := &domain.Manifest{Name: "acme", Proxy: domain.Proxy{Entrypoints: map[string]int{"web": 80}}, Repos: map[string]domain.Repo{"api": {}}}
	st := domain.UniverseState{
		Name: "3", ID: "3", Slug: "u3", Domain: "example.test",
		Hosts: map[string]string{"web": "web-u3.example.test", "api": "api-u3.example.test"},
		Rules: map[string]domain.RouteRule{
			"web": {Entrypoint: "web", PortForward: "auto", HostPort: 41000, ForwardURL: "http://192.0.2.7:41000"},
			// Its forwarded port is not chosen yet.
			"api": {Entrypoint: "web", PortForward: "auto"},
		},
	}
	svc := New(Deps{System: fakeSystem{}, Store: fakeStore{st: st}}, "/ws", manifest, "test")
	hosts, origins := svc.universeAddresses(st)
	for source, want := range map[string]string{
		"forward:web+url:web":               "http://192.0.2.7:41000,http://web-u3.example.test",
		"url:web+forward:web":               "http://web-u3.example.test,http://192.0.2.7:41000",
		"forward:web+url:web:space":         "http://192.0.2.7:41000 http://web-u3.example.test",
		"forward:web+url:web:newline":       "http://192.0.2.7:41000\nhttp://web-u3.example.test",
		"url:web+url:web+host:web":          "http://web-u3.example.test,web-u3.example.test",
		"forward:api+url:api":               "http://api-u3.example.test", // not allocated: left out
		"universe+slug+domain:semicolon":    "3;u3;example.test",
		"origins+url:web:space":             "http://api-u3.example.test http://web-u3.example.test http://192.0.2.7:41000",
		"hosts+host:web+universe:semicolon": "api-u3.example.test;web-u3.example.test;3",
		"origins + url:web:space":           "http://api-u3.example.test http://web-u3.example.test http://192.0.2.7:41000",
	} {
		got, err := svc.resolveEnvSource(source, st, hosts, origins)
		if err != nil || got != want {
			t.Errorf("%s = %q, %v; want %q", source, got, err, want)
		}
	}
	// origins lists the forwarded address of every host that has one, and
	// hosts stays names.
	if got := strings.Join(origins, " "); got != "http://api-u3.example.test http://web-u3.example.test http://192.0.2.7:41000" {
		t.Errorf("origins = %v", origins)
	}
	if got := strings.Join(hosts, " "); got != "api-u3.example.test web-u3.example.test" {
		t.Errorf("hosts = %v", hosts)
	}
	for _, bad := range []string{"url:web+", "+url:web", "url:nope+url:web", "origins:space+url:web", "forward:nope+url:web"} {
		if _, err := svc.resolveEnvSource(bad, st, hosts, origins); err == nil {
			t.Errorf("%q: want an error", bad)
		}
	}
	// A host that is not forwarded at all has no forwarded address to give,
	// combined or not: that is a mistake to say, not one to leave out.
	st.Hosts["db"] = "db-u3.example.test"
	st.Rules["db"] = domain.RouteRule{Entrypoint: "web"}
	if _, err := svc.resolveEnvSource("forward:db+url:web", st, hosts, origins); err == nil {
		t.Error("forward of a host that is not forwarded: want an error")
	}
}
