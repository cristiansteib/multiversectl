package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"text/template"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// excludeMu serialises edits of git's exclude file. For a linked worktree git
// keeps one for the whole clone, so universes started side by side by this
// process all write the same file.
var excludeMu sync.Mutex

// renderData is what a template can read as {{ .Name }}. The addresses are the
// same ones the compose variables and the env labels give, so a file never
// disagrees with the environment of the service that reads it.
type renderData struct {
	Universe string // the universe's name, as `universe ls` shows it
	Slug     string // the part a hostname carries
	Number   int    // 0 for a universe with a name instead of a number
	Domain   string
	App      string
	Dir      string // the universe's directory
	RepoDir  string // the app's checkout, where the file is written
	// Hosts and Origins are every hostname of the universe and every one as an
	// origin, in a stable order; origins and hosts join them.
	Hosts   []string
	Origins []string
	// Env is the environment a hook receives: <HOST>_URL, <HOST>_FORWARD_URL,
	// UNIVERSE, the manifest's own env, and the rest of runtimeEnv.
	Env map[string]string
}

// renderOutcome says what rendering one file did.
type renderOutcome int

const (
	renderUnchanged renderOutcome = iota
	renderWritten
)

// RenderInput names the universe and the apps whose files to render (every app
// with some when empty). With Print nothing is written: the result is shown.
type RenderInput struct {
	Universe string
	Apps     []string
	Print    bool
}

// Render renders the files an app's manifest asks for, now, as `universe up`
// does before it starts the app. It uses the addresses the universe last
// started with, so a forwarded address exists once it has been started.
func (s *Service) Render(ctx context.Context, in RenderInput) error {
	st, err := s.Resolve(in.Universe)
	if err != nil {
		return err
	}
	if !in.Print {
		unlock, err := s.lockUniverse(ctx, st.ID, st.Name)
		if err != nil {
			return err
		}
		defer unlock()
		if st, err = s.Resolve(in.Universe); err != nil {
			return err
		}
	}
	s = s.forUniverseOrSelf(st.Name)
	apps, err := s.appsToAct(in.Apps)
	if err != nil {
		return err
	}
	u := s.UniverseOf(st)
	rendered := false
	for _, appKey := range apps {
		if len(s.Manifest.Apps[appKey].Render) == 0 {
			continue
		}
		rendered = true
		if err := s.renderApp(ctx, u, st, appKey, in.Print); err != nil {
			return err
		}
	}
	if !rendered {
		s.Out.Info("no render entries to run")
	}
	return nil
}

// renderApp writes every file the app asks for, in order, and stops at the
// first that cannot be: a service started without its config is the failure
// this exists to prevent. With print, it shows the output and writes nothing.
func (s *Service) renderApp(ctx context.Context, u domain.Universe, st domain.UniverseState, appKey string, print bool) error {
	app := s.Manifest.Apps[appKey]
	if len(app.Render) == 0 {
		return nil
	}
	repoDir := u.RepoDir(app.Repo)
	env := s.hookEnv(u, st, domain.PreUp, appKey)
	hosts, origins := s.universeAddresses(st)
	data := renderData{
		Universe: u.Name, Slug: u.Slug(), Number: st.Number, Domain: st.Domain, App: appKey,
		Dir: u.Dir(), RepoDir: repoDir, Hosts: hosts, Origins: origins, Env: env,
	}
	for _, r := range app.Render {
		out, err := s.renderTemplate(r, repoDir, st, data)
		if err != nil {
			return fmt.Errorf("%s: render %s: %w", appKey, r.To, err)
		}
		if print {
			s.Out.Plain("--- %s: %s", appKey, r.To)
			s.Out.Plain("%s", strings.TrimSuffix(string(out), "\n"))
			continue
		}
		outcome, err := s.writeRendered(ctx, repoDir, r, out)
		if err != nil {
			return fmt.Errorf("%s: render %s: %w", appKey, r.To, err)
		}
		if outcome == renderWritten {
			s.Out.OK("%s: rendered %s", appKey, r.To)
		} else {
			s.Out.Info("%s: %s is up to date", appKey, r.To)
		}
	}
	return nil
}

// renderTemplate evaluates one entry. A value it cannot find is an error that
// names the file and the key, never an empty string in a config file.
func (s *Service) renderTemplate(r domain.Render, repoDir string, st domain.UniverseState, data renderData) ([]byte, error) {
	text := r.Template
	if r.From != "" {
		path := filepath.Join(repoDir, r.From)
		if rest, manifestRelative := strings.CutPrefix(r.From, "@"); manifestRelative {
			path = filepath.Join(s.Manifest.Dir, rest)
		}
		if !filepath.IsLocal(strings.TrimPrefix(r.From, "@")) {
			return nil, fmt.Errorf("from %q leaves the directory it is relative to", r.From)
		}
		b, err := s.Files.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading the template: %w", err)
		}
		text = string(b)
	}
	tpl, err := template.New(r.To).Option("missingkey=error").Funcs(s.renderFuncs(st, data.Env)).Parse(text)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// renderFuncs is the whole vocabulary of a template beyond text/template's own.
// host, url, forward, origins and hosts are the sources of the env labels under
// the same names, so what a label would give a service a template gives a file.
func (s *Service) renderFuncs(st domain.UniverseState, env map[string]string) template.FuncMap {
	known := func() string { return strings.Join(slices.Sorted(maps.Keys(st.Hosts)), ", ") }
	hostname := func(key string) (string, error) {
		name, ok := st.Hosts[key]
		if !ok {
			return "", fmt.Errorf("no host %q in this multiverse (have: %s)", key, known())
		}
		return name, nil
	}
	list := func(items func() []string, sep []string) (string, error) {
		name := "comma"
		if len(sep) > 0 {
			name = sep[0]
		}
		joiner, ok := separators[name]
		if !ok {
			return "", fmt.Errorf("unknown separator %q: want %s", name, strings.Join(slices.Sorted(maps.Keys(separators)), ", "))
		}
		return strings.Join(items(), joiner), nil
	}
	hosts, origins := s.universeAddresses(st)
	return template.FuncMap{
		"host": func(key string) (string, error) {
			name, err := hostname(key)
			return s.hostOf(st, key, name), err
		},
		"url": func(key string) (string, error) {
			name, err := hostname(key)
			return s.originOf(st, key, name), err
		},
		"forward": func(key string) (string, error) {
			if _, err := hostname(key); err != nil {
				return "", err
			}
			rule := st.Rules[key]
			if rule.ForwardURL == "" {
				return "", fmt.Errorf("host %q is not forwarded: set portForward: auto on it (the address exists once the universe has been started)", key)
			}
			return rule.ForwardURL, nil
		},
		// Any source an env label can name, including several joined with +.
		"source":  func(spec string) (string, error) { return s.resolveEnvSource(spec, st, hosts, origins) },
		"origins": func(sep ...string) (string, error) { return list(func() []string { return origins }, sep) },
		"hosts":   func(sep ...string) (string, error) { return list(func() []string { return hosts }, sep) },
		"env": func(key string) (string, error) {
			v, ok := env[key]
			if !ok {
				return "", fmt.Errorf("environment variable %q is not set (a hook would not receive it either)", key)
			}
			return v, nil
		},
		"default": func(fallback, v any) any {
			if emptyValue(v) {
				return fallback
			}
			return v
		},
		"required": func(msg string, v any) (any, error) {
			if emptyValue(v) {
				return nil, errors.New(msg)
			}
			return v, nil
		},
		"toJson": func(v any) (string, error) {
			var b bytes.Buffer
			enc := json.NewEncoder(&b)
			enc.SetEscapeHTML(false)
			if err := enc.Encode(v); err != nil {
				return "", err
			}
			return strings.TrimSuffix(b.String(), "\n"), nil
		},
	}
}

func emptyValue(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case []string:
		return len(x) == 0
	case map[string]string:
		return len(x) == 0
	case int:
		return x == 0
	case bool:
		return !x
	}
	return false
}

// writeRendered puts content where r says, and only when that changes
// something, so a file watcher does not reload an app for nothing.
func (s *Service) writeRendered(ctx context.Context, repoDir string, r domain.Render, content []byte) (renderOutcome, error) {
	if !s.Files.Exists(repoDir) {
		return renderUnchanged, fmt.Errorf("the checkout %s does not exist", repoDir)
	}
	target := filepath.Join(repoDir, r.To)
	if err := s.confine(repoDir, target); err != nil {
		return renderUnchanged, err
	}
	tracked, err := s.Git.IsTracked(ctx, repoDir, filepath.ToSlash(filepath.Clean(r.To)))
	if err != nil {
		return renderUnchanged, fmt.Errorf("asking git whether it is tracked: %w", err)
	}
	if tracked && !r.Overwrite {
		return renderUnchanged, fmt.Errorf("git tracks it, and a render would overwrite source: render to another path, or set overwrite: true on the entry")
	}
	if !tracked {
		if err := s.excludeFromGit(ctx, repoDir, r.To); err != nil {
			return renderUnchanged, err
		}
	}
	mode := r.Mode
	if mode == 0 {
		mode = domain.DefaultRenderMode
	}
	if have, err := s.Files.Mode(target); err == nil {
		if old, err := s.Files.ReadFile(target); err == nil && bytes.Equal(old, content) {
			if have.Perm() != mode {
				return renderWritten, s.Files.Chmod(target, mode)
			}
			return renderUnchanged, nil
		}
	}
	if err := s.Files.MkdirAll(filepath.Dir(target)); err != nil {
		return renderUnchanged, err
	}
	return renderWritten, s.writeAtomically(target, content, mode)
}

// writeAtomically leaves either the old file or the whole new one: a service
// booting during the write must not read half a config.
func (s *Service) writeAtomically(path string, content []byte, mode fs.FileMode) error {
	tmp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".multiverse-tmp")
	if err := s.Files.WriteFile(tmp, content, mode); err != nil {
		return err
	}
	// The umask trims what WriteFile asks for; the entry says what it wants.
	if err := s.Files.Chmod(tmp, mode); err != nil {
		_ = s.Files.RemoveAll(tmp)
		return err
	}
	if err := s.Files.Rename(tmp, path); err != nil {
		_ = s.Files.RemoveAll(tmp)
		return err
	}
	return nil
}

// confine refuses a target that, through a link, would be written outside the
// app's checkout. The manifest's path is already checked to be local; this is
// about what is on disk.
func (s *Service) confine(repoDir, target string) error {
	root, err := s.Files.RealPath(repoDir)
	if err != nil {
		return err
	}
	// The nearest directory that exists is the one a link could redirect.
	dir := filepath.Dir(target)
	for !s.Files.Exists(dir) && dir != repoDir {
		dir = filepath.Dir(dir)
	}
	real, err := s.Files.RealPath(dir)
	if err != nil {
		return err
	}
	if rel, err := filepath.Rel(root, real); err != nil || !filepath.IsLocal(rel) {
		return fmt.Errorf("%s resolves outside the checkout (%s): refusing to write there", dir, real)
	}
	if mode, err := s.Files.Mode(target); err == nil && !mode.IsRegular() {
		return fmt.Errorf("%s is not a regular file (a link or a directory): refusing to replace it", target)
	}
	return nil
}

// excludeFromGit adds the path to the checkout's exclude file once, so a
// rendered file does not appear as an untracked change and the repository's
// tracked .gitignore is left alone.
func (s *Service) excludeFromGit(ctx context.Context, repoDir, to string) error {
	file, err := s.Git.ExcludeFile(ctx, repoDir)
	if err != nil {
		return fmt.Errorf("finding git's exclude file: %w", err)
	}
	line := excludePattern(to)
	excludeMu.Lock()
	defer excludeMu.Unlock()
	var have []byte
	if s.Files.Exists(file) {
		if have, err = s.Files.ReadFile(file); err != nil {
			return err
		}
		for _, l := range strings.Split(string(have), "\n") {
			if strings.TrimRight(l, "\r") == line {
				return nil
			}
		}
	}
	next := string(have)
	if next != "" && !strings.HasSuffix(next, "\n") {
		next += "\n"
	}
	if next == "" {
		next = "# written by multiversectl: files rendered from a template\n"
	}
	next += line + "\n"
	if err := s.Files.MkdirAll(filepath.Dir(file)); err != nil {
		return err
	}
	return s.writeAtomically(file, []byte(next), 0o644)
}

// excludePattern is a path as an exclude line that matches that file and
// nothing else: anchored at the checkout's root, with the characters git reads
// as a pattern escaped.
func excludePattern(to string) string {
	p := filepath.ToSlash(filepath.Clean(to))
	var b strings.Builder
	b.WriteByte('/')
	for _, c := range p {
		if strings.ContainsRune(`*?[\`, c) {
			b.WriteByte('\\')
		}
		b.WriteRune(c)
	}
	return strings.TrimRight(b.String(), " ") + strings.Repeat(`\ `, len(p)-len(strings.TrimRight(p, " ")))
}
