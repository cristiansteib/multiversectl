package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

type preflightRuntime struct {
	Runtime
	err error
}

func (r preflightRuntime) Version(context.Context) (string, error) {
	if r.err != nil {
		return "", r.err
	}
	return "2.29.1", nil
}

// preflightGit is asked about every repository at once, so it keeps its
// record under a lock.
type preflightGit struct {
	Git
	errs  map[string]error
	mu    sync.Mutex
	asked []string
}

func (g *preflightGit) LsRemote(_ context.Context, url string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.asked = append(g.asked, url)
	return g.errs[url]
}

type preflightSystem struct {
	System
	free uint64
	home string
}

func (p preflightSystem) FreeSpace(context.Context, string) (uint64, error) { return p.free, nil }
func (p preflightSystem) Getenv(k string) string {
	if k == "HOME" {
		return p.home
	}
	return ""
}

type preflightCommands struct{ err error }

func (c preflightCommands) RunHost(_ context.Context, h HostCommand) (string, error) {
	if c.err != nil {
		return "", c.err
	}
	return "github.com/docker/buildx v0.17.1 abc\n", nil
}

type realFiles struct{ Files }

func (realFiles) Exists(p string) bool              { _, err := os.Stat(p); return err == nil }
func (realFiles) ReadFile(p string) ([]byte, error) { return os.ReadFile(p) }

func checksByName(checks []Check) map[string]Check {
	out := map[string]Check{}
	for _, c := range checks {
		out[c.Name] = c
	}
	return out
}

func TestPreflightSaysWhatToRunForEachThingThatIsMissing(t *testing.T) {
	for _, tc := range []struct {
		name    string
		err     error
		buildx  error
		free    uint64
		want    map[string]Status
		mention string
	}{
		{"all well", nil, nil, 50 * gib, map[string]Status{"docker": StatusOK, "buildx": StatusOK, "disk": StatusOK}, ""},
		{"no docker", errors.New("docker CLI not found on PATH"), nil, 50 * gib, map[string]Status{"docker": StatusFail, "disk": StatusOK}, "Install Docker"},
		{"daemon down", errors.New("docker daemon is unreachable: Cannot connect"), nil, 50 * gib, map[string]Status{"docker": StatusFail}, "systemctl start docker"},
		{"no buildx", nil, errors.New("exit 1"), 50 * gib, map[string]Status{"docker": StatusOK, "buildx": StatusFail}, "docker buildx version"},
		{"no space", nil, nil, 1 * gib, map[string]Status{"disk": StatusFail}, "docker system df"},
		{"little space", nil, nil, 5 * gib, map[string]Status{"disk": StatusWarn}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := New(Deps{Runtime: preflightRuntime{err: tc.err}, Commands: preflightCommands{tc.buildx},
				System: preflightSystem{free: tc.free}, Files: realFiles{}, Out: discardOut{}}, "/ws", nil, "test")
			checks := s.Preflight(context.Background(), PreflightInput{Machine: true, Path: t.TempDir() + "/not/yet"})
			got := checksByName(checks)
			for name, status := range tc.want {
				if got[name].Status != status {
					t.Errorf("%s = %+v, want %s", name, got[name], status)
				}
			}
			if tc.mention != "" {
				all := ""
				for _, c := range checks {
					all += c.Detail + "\n"
				}
				if !strings.Contains(all, tc.mention) {
					t.Errorf("no mention of %q in:\n%s", tc.mention, all)
				}
			}
		})
	}
}

func TestPreflightTellsNoNetworkFromNoAccessFromAHostKey(t *testing.T) {
	m := &domain.Manifest{Name: "acme", Repos: map[string]domain.Repo{
		"api":    {URL: "git@git.example.test:acme/api.git"},
		"web":    {URL: "https://git.example.test/acme/web.git"},
		"worker": {URL: "git@keys.example.test:acme/worker.git"},
		"docs":   {URL: "https://token:s3cret@git.example.test/acme/docs.git"},
		"ok":     {URL: "https://git.example.test/acme/ok.git"},
		"twin":   {URL: "https://git.example.test/acme/ok.git"},
	}}
	git := &preflightGit{errs: map[string]error{
		"git@git.example.test:acme/api.git":                   errors.New("git ls-remote: ssh: Could not resolve hostname git.example.test: Temporary failure in name resolution\nfatal: Could not read from remote repository."),
		"https://git.example.test/acme/web.git":               errors.New("git ls-remote: remote: Repository not found.\nfatal: Authentication failed"),
		"git@keys.example.test:acme/worker.git":               errors.New("git ls-remote: Host key verification failed.\nfatal: Could not read from remote repository."),
		"https://token:s3cret@git.example.test/acme/docs.git": context.DeadlineExceeded,
	}}
	s := New(Deps{Git: git, Files: realFiles{}, Out: discardOut{}}, t.TempDir(), m, "test")
	got := checksByName(s.Preflight(context.Background(), PreflightInput{Repos: true}))

	for name, want := range map[string]string{
		"repository api":    "no network",
		"repository web":    "no access",
		"repository worker": "host key",
		"repository docs":   "no network",
	} {
		c := got[name]
		if c.Status != StatusFail || !strings.Contains(c.Detail, want) {
			t.Errorf("%s = %+v, want a failure saying %q", name, c, want)
		}
	}
	if !strings.Contains(got["repository worker"].Detail, "ssh-keyscan keys.example.test") {
		t.Errorf("host key advice: %s", got["repository worker"].Detail)
	}
	if strings.Contains(got["repository docs"].Detail, "s3cret") {
		t.Errorf("a password was printed: %s", got["repository docs"].Detail)
	}
	if c := got["repository ok, twin"]; c.Status != StatusOK {
		t.Errorf("two repositories at one address are asked once: %+v", got)
	}
	n := 0
	for _, u := range git.asked {
		if u == "https://git.example.test/acme/ok.git" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("asked %d times", n)
	}
}

func TestPreflightDoesNotAskAboutARepositoryThatIsAlreadyCloned(t *testing.T) {
	root := t.TempDir()
	m := &domain.Manifest{Name: "acme", Repos: map[string]domain.Repo{"api": {URL: "git@git.example.test:acme/api.git"}}}
	if err := os.MkdirAll(m.CloneDir(root, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	git := &preflightGit{}
	s := New(Deps{Git: git, Files: realFiles{}, Out: discardOut{}}, root, m, "test")
	if checks := s.Preflight(context.Background(), PreflightInput{Repos: true}); len(checks) != 0 || len(git.asked) != 0 {
		t.Errorf("checks %+v, asked %v", checks, git.asked)
	}
}

func TestPreflightNamesTheSecretFilesThatAreMissing(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	m := &domain.Manifest{Name: "acme", Repos: map[string]domain.Repo{"api": {URL: "x"}},
		Apps: map[string]domain.App{"api": {Repo: "api", Secrets: []string{".env"}}}}
	clone := m.CloneDir(root, "api")
	if err := os.MkdirAll(clone, 0o755); err != nil {
		t.Fatal(err)
	}
	compose := "services: {web: {image: x}}\nsecrets:\n  netrc: {file: ~/.netrc}\n  token: {file: ./token.txt}\n  inline: {environment: TOKEN}\n"
	if err := os.WriteFile(filepath.Join(clone, "compose.yaml"), []byte(compose), 0o644); err != nil {
		t.Fatal(err)
	}
	s := New(Deps{Files: realFiles{}, System: preflightSystem{home: home}, Out: discardOut{}}, root, m, "test")
	var details []string
	for _, c := range s.Preflight(context.Background(), PreflightInput{Secrets: true}) {
		if c.Status != StatusFail {
			t.Errorf("unexpected %+v", c)
		}
		details = append(details, c.Detail)
	}
	all := strings.Join(details, "\n")
	for _, want := range []string{filepath.Join(clone, ".env"), filepath.Join(home, ".netrc"), filepath.Join(clone, "token.txt")} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %s in:\n%s", want, all)
		}
	}
	if strings.Contains(all, "inline") {
		t.Errorf("a secret that is not a file was reported:\n%s", all)
	}

	// All of them there: nothing to say but that it is fine.
	for _, f := range []string{filepath.Join(clone, ".env"), filepath.Join(home, ".netrc"), filepath.Join(clone, "token.txt")} {
		if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	checks := s.Preflight(context.Background(), PreflightInput{Secrets: true})
	if len(checks) != 1 || checks[0].Status != StatusOK || PreflightFailed(checks) {
		t.Errorf("checks = %+v", checks)
	}
}
