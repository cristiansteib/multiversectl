package app

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// Disk space a start needs: checkouts, images and volumes. Below the first it
// is refused, below the second it is said.
const (
	minFreeDisk  uint64 = 2 << 30
	warnFreeDisk uint64 = 10 << 30
	gib                 = 1 << 30
)

// remoteTimeout bounds one `git ls-remote`: a dead network must not hold a
// start for the minutes git would wait on its own.
const remoteTimeout = 15 * time.Second

// PreflightInput says what a preflight looks at.
type PreflightInput struct {
	// Path is where the multiverses will be written, for the disk check; its
	// nearest existing parent is measured.
	Path string
	// Machine checks docker, compose, buildx and the disk.
	Machine bool
	// Repos checks that every repository of the manifest that is not cloned yet
	// can be read. It needs a manifest.
	Repos bool
	// Secrets checks that the secret files the manifest and the compose files of
	// the cloned repositories name are there. It needs a manifest.
	Secrets bool
}

// Preflight checks, before anything is cloned or started, what a start needs
// from the machine, and for each thing that is not right says what to run. It
// reports every problem rather than the first. A failing check is a finding, not
// an error: the caller decides what stops.
func (s *Service) Preflight(ctx context.Context, in PreflightInput) []Check {
	var out []Check
	add := func(name string, status Status, format string, a ...any) {
		out = append(out, Check{Name: name, Status: status, Detail: fmt.Sprintf(format, a...)})
	}
	if in.Machine {
		s.preflightDocker(ctx, add)
		s.preflightDisk(ctx, add, in.Path)
	}
	if in.Repos && s.Manifest != nil {
		s.preflightRepos(ctx, add)
	}
	if in.Secrets && s.Manifest != nil {
		s.preflightSecrets(add)
	}
	return out
}

// PreflightFailed says whether any check found something that stops a start.
func PreflightFailed(checks []Check) bool {
	return slices.ContainsFunc(checks, func(c Check) bool { return c.Status == StatusFail })
}

func (s *Service) preflightDocker(ctx context.Context, add addCheck) {
	version, err := s.Runtime.Version(ctx)
	switch {
	case err == nil:
		add("docker", StatusOK, "reachable; compose %s", version)
	case strings.Contains(err.Error(), "not found on PATH"):
		add("docker", StatusFail, "the docker command is not installed. Install Docker (https://docs.docker.com/get-docker/), then run: docker info")
		return
	case strings.Contains(err.Error(), "unreachable"):
		add("docker", StatusFail, "the docker daemon does not answer. Start it (Linux: sudo systemctl start docker; macOS: open Docker Desktop), then run: docker info")
		return
	default:
		add("docker", StatusFail, "docker compose is not usable: %s. Install the compose plugin (https://docs.docker.com/compose/install/), then run: docker compose version", firstLine(err.Error()))
	}
	if s.Commands == nil {
		return
	}
	if out, err := s.Commands.RunHost(ctx, HostCommand{Args: []string{"docker", "buildx", "version"}}); err != nil {
		add("buildx", StatusFail, "docker buildx is missing, and images that build need it. Install it (https://docs.docker.com/build/install-buildx/), then run: docker buildx version")
	} else {
		add("buildx", StatusOK, "%s", strings.TrimSpace(firstLine(out)))
	}
}

func (s *Service) preflightDisk(ctx context.Context, add addCheck, path string) {
	if path == "" {
		return
	}
	dir := path
	for dir != "/" && dir != "." && !s.Files.Exists(dir) {
		dir = filepath.Dir(dir)
	}
	free, err := s.System.FreeSpace(ctx, dir)
	switch {
	case err != nil:
		s.say(1, "disk space of %s: %v", dir, err)
	case free < minFreeDisk:
		add("disk", StatusFail, "%.1f GiB free where %s goes (%s) and a start needs at least %d. Free some space (docker system df shows what docker holds; `docker image prune` frees images you do not use), or put the multiverse elsewhere with --root", float64(free)/gib, path, dir, minFreeDisk/gib)
	case free < warnFreeDisk:
		add("disk", StatusWarn, "%.1f GiB free where %s goes; images and volumes can use more than that", float64(free)/gib, path)
	default:
		add("disk", StatusOK, "%.0f GiB free where %s goes", float64(free)/gib, path)
	}
}

// preflightRepos asks every remote that is not cloned yet whether a clone of it
// would work from here, all at once, and sorts the failures by what to do about
// each.
func (s *Service) preflightRepos(ctx context.Context, add addCheck) {
	type target struct {
		url  string
		keys []string
	}
	var targets []*target
	byURL := map[string]*target{}
	for _, key := range sortedRepoKeys(s.Manifest.Repos) {
		repo := s.Manifest.Repos[key]
		if repo.URL == "" || s.Files.Exists(s.Manifest.CloneDir(s.Multiverse, key)) {
			continue
		}
		if t, ok := byURL[repo.URL]; ok {
			t.keys = append(t.keys, key)
			continue
		}
		t := &target{url: repo.URL, keys: []string{key}}
		byURL[repo.URL] = t
		targets = append(targets, t)
	}
	errs := make([]error, len(targets))
	var wg sync.WaitGroup
	slots := make(chan struct{}, 8)
	for i, t := range targets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			tctx, cancel := context.WithTimeout(ctx, remoteTimeout)
			defer cancel()
			errs[i] = s.Git.LsRemote(tctx, t.url)
		}()
	}
	wg.Wait()
	for i, t := range targets {
		name := "repository " + strings.Join(t.keys, ", ")
		if errs[i] == nil {
			add(name, StatusOK, "%s is readable", redactURL(t.url))
			continue
		}
		add(name, StatusFail, "%s", remoteProblem(t.url, errs[i]))
	}
}

// Why a remote cannot be read, as far as git's words say.
const (
	remoteNoNetwork = "no network"
	remoteNoAccess  = "no access"
	remoteHostKey   = "host key"
	remoteOther     = "other"
)

var (
	noNetworkRE = regexp.MustCompile(`(?i)could not resolve host|temporary failure in name resolution|network is unreachable|no route to host|connection timed out|operation timed out|timed out|failed to connect|connection refused|could not connect|deadline exceeded|name or service not known|nodename nor servname`)
	hostKeyRE   = regexp.MustCompile(`(?i)host key verification failed|remote host identification has changed|no matching host key`)
	noAccessRE  = regexp.MustCompile(`(?i)permission denied|authentication failed|could not read username|could not read password|repository not found|access denied|not found|invalid username or password|returned error: 40[0-9]|the requested url returned error: 40`)
)

// classifyRemote sorts a failed ls-remote by what the person has to do: the host
// key is checked first because ssh follows it with "could not read from remote
// repository", which would read as no access.
func classifyRemote(err error) string {
	msg := err.Error()
	switch {
	case hostKeyRE.MatchString(msg):
		return remoteHostKey
	case noNetworkRE.MatchString(msg):
		return remoteNoNetwork
	case noAccessRE.MatchString(msg):
		return remoteNoAccess
	}
	return remoteOther
}

func remoteProblem(rawURL string, err error) string {
	host := remoteHost(rawURL)
	shown := redactURL(rawURL)
	switch classifyRemote(err) {
	case remoteNoNetwork:
		return fmt.Sprintf("%s cannot be reached: no network, or %s is down. Check your connection, VPN or proxy, then run: git ls-remote %s", shown, host, shown)
	case remoteHostKey:
		return fmt.Sprintf("%s: ssh does not trust %s's host key. Check it is the right host, then run: ssh-keyscan %s >> ~/.ssh/known_hosts (if it changed: ssh-keygen -R %s)", shown, host, host, host)
	case remoteNoAccess:
		return fmt.Sprintf("%s: no access. Check that your ssh key or token can read it (ssh -T git@%s, or gh auth status), then run: git ls-remote %s", shown, host, shown)
	}
	return fmt.Sprintf("%s cannot be read: %s. Run: git ls-remote %s", shown, firstLine(err.Error()), shown)
}

// remoteHost is the host of a git URL in either of its forms.
func remoteHost(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Hostname()
	}
	rest := strings.TrimPrefix(raw, "ssh://")
	if _, after, ok := strings.Cut(rest, "@"); ok {
		rest = after
	}
	host, _, _ := strings.Cut(rest, ":")
	return host
}

// redactURL keeps a password or token out of what is printed.
func redactURL(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.User != nil {
		if _, has := u.User.Password(); has {
			u.User = url.User(u.User.Username())
			return u.String()
		}
	}
	return raw
}

// preflightSecrets names every secret file a start would stop on: the ones the
// manifest lists for an app, which are copied from its shared clone, and the
// files its compose files give a build or a service as `secrets: file:`. Only a
// repository that is cloned can be read.
func (s *Service) preflightSecrets(add addCheck) {
	u := domain.Universe{Name: "0", Multiverse: s.Multiverse, Manifest: s.Manifest}
	missing := 0
	for _, appKey := range s.Manifest.RunnableApps() {
		app := s.Manifest.Apps[appKey]
		clone := s.Manifest.CloneDir(s.Multiverse, app.Repo)
		if !s.Files.Exists(clone) {
			continue
		}
		for _, pair := range s.SecretPaths(u, appKey) {
			if s.Files.Exists(pair.Src) || (pair.Template != "" && s.Files.Exists(pair.Template)) {
				continue
			}
			missing++
			add("secret "+appKey, StatusFail, "%s needs %s, which git ignores and no repository has. Put your copy at: %s", appKey, pair.Name, pair.Src)
		}
		for _, file := range s.composeSecretFiles(clone, app) {
			if s.Files.Exists(file.path) {
				continue
			}
			missing++
			add("secret "+appKey, StatusFail, "%s: compose secret %q needs the file %s, which is not there. Create it, or point the secret at the file you have", appKey, file.name, file.path)
		}
	}
	if missing == 0 {
		add("secrets", StatusOK, "every secret file the manifest and the compose files name is there")
	}
}

type composeSecret struct{ name, path string }

// composeSecretFiles reads the top-level `secrets:` of an app's own compose
// files for the ones backed by a file. A path with ~ or ${HOME} is the user's.
func (s *Service) composeSecretFiles(repoDir string, app domain.App) []composeSecret {
	var out []composeSecret
	names := app.Compose
	if len(names) == 0 {
		names = domain.ComposeNames
	}
	seen := false
	for _, name := range names {
		path := name
		if !filepath.IsAbs(path) {
			path = filepath.Join(repoDir, name)
		}
		if len(app.Compose) == 0 && seen {
			break // the repository's own file: one is run, not each
		}
		data, err := s.Files.ReadFile(path)
		if err != nil {
			continue
		}
		seen = true
		var doc struct {
			Secrets map[string]struct {
				File string `yaml:"file"`
			} `yaml:"secrets"`
		}
		if yaml.Unmarshal(data, &doc) != nil {
			continue
		}
		for _, secretName := range slices.Sorted(mapKeysOf(doc.Secrets)) {
			file := doc.Secrets[secretName].File
			if file == "" {
				continue
			}
			out = append(out, composeSecret{name: secretName, path: s.secretFilePath(file, filepath.Dir(path))})
		}
	}
	return out
}

// secretFilePath resolves a compose secret's file the way compose does: ~ and
// ${HOME} are the home compose itself is given, which a state directory does
// not move, and a relative path is beside the compose file.
func (s *Service) secretFilePath(file, dir string) string {
	if home := s.System.ProcessEnv("HOME"); home != "" {
		file = strings.ReplaceAll(strings.ReplaceAll(file, "${HOME}", home), "$HOME", home)
		if file == "~" || strings.HasPrefix(file, "~/") {
			file = filepath.Join(home, strings.TrimPrefix(file, "~"))
		}
	}
	if !filepath.IsAbs(file) {
		file = filepath.Join(dir, file)
	}
	return file
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}
