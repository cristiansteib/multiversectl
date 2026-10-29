package app

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// How long a command in a service is tried after the app is up, and how often.
// The services are up, but a database may still be migrating under them, and a
// command that needs its tables fails until it is done.
var (
	hookEvery  = 3 * time.Second
	hookGiveUp = 3 * time.Minute
)

// multiverseHooks is the name the hooks of the whole multiverse go by where an
// app's name would be.
const multiverseHooks = ""

// hookContext is what a command running on the machine can read from
// $MULTIVERSE_CONTEXT_FILE: the universe's identity, directories and every
// address, so a project can build whatever it needs (a redirect URI, an .env)
// without the tool knowing about it.
type hookContext struct {
	Event    string              `json:"event"`
	App      string              `json:"app,omitempty"`
	Universe string              `json:"universe"`
	Slug     string              `json:"slug"`
	Number   int                 `json:"number,omitempty"`
	Dir      string              `json:"dir"`
	Domain   string              `json:"domain"`
	Hosts    map[string]hookHost `json:"hosts"`
	Repos    map[string]string   `json:"repos"`
	Branches map[string]string   `json:"branches"`
}

type hookHost struct {
	Hostname   string `json:"hostname"`
	URL        string `json:"url"`
	ForwardURL string `json:"forwardUrl,omitempty"`
	App        string `json:"app,omitempty"`
	Service    string `json:"service,omitempty"`
	Port       int    `json:"port,omitempty"`
}

// hookEnv is what a hook is told about its universe: the variables a compose
// file already gets, and which event it is running for.
func (s *Service) hookEnv(u domain.Universe, st domain.UniverseState, ev domain.HookEvent, appKey string) map[string]string {
	env := s.runtimeEnv(u, st)
	if appKey != "" {
		env = s.appEnv(u, st, appKey)
	}
	env["MULTIVERSE_EVENT"] = string(ev)
	env["MULTIVERSE_APP"] = appKey
	return env
}

func (s *Service) hookContext(u domain.Universe, st domain.UniverseState, ev domain.HookEvent, appKey string) hookContext {
	c := hookContext{
		Event: string(ev), App: appKey, Universe: u.Name, Slug: u.Slug(), Number: st.Number,
		Dir: u.Dir(), Domain: st.Domain, Branches: st.Branches,
		Hosts: map[string]hookHost{}, Repos: map[string]string{},
	}
	for key, hostname := range st.Hosts {
		rule := st.Rules[key]
		c.Hosts[key] = hookHost{Hostname: hostname, URL: s.originOf(st, key, hostname), ForwardURL: rule.ForwardURL, App: rule.App, Service: rule.Service, Port: rule.Port}
	}
	for repoKey := range s.Manifest.Repos {
		c.Repos[repoKey] = u.RepoDir(repoKey)
	}
	return c
}

// fire runs the hooks tied to an event, for one app or (with multiverseHooks)
// for the multiverse, in order. A hook marked Once runs on a universe's first
// start only, and is remembered: it is what a repository needs before a universe
// is of any use (the data a person logs in with), and running it on every start
// would load it over whatever was changed since.
//
// What a hook that does not succeed means is its own to say (see Hook.Policy).
// By default it is said, with how to run it by hand, and is not an error: the
// universe is up, and refusing it for a seed that did not load would be the
// worse of the two. The hooks after it wait, since they may depend on it, and it
// is tried again the next time.
//
// req is the runtime request a command in a service goes through; nil builds
// the usual one. services are the ones the app defines, for choosing where a
// command with no service of its own runs.
func (s *Service) fire(ctx context.Context, ev domain.HookEvent, st domain.UniverseState, appKey string, req *RuntimeRequest, services []string) (domain.UniverseState, error) {
	return s.runHooks(ctx, ev, st, appKey, req, services, false)
}

// runHooks is fire, and with manual it is what a person asked for: a hook
// marked Once runs again and is not marked, and one that does not succeed is an
// error whatever it says it means, since there is no start to protect.
func (s *Service) runHooks(ctx context.Context, ev domain.HookEvent, st domain.UniverseState, appKey string, req *RuntimeRequest, services []string, manual bool) (domain.UniverseState, error) {
	hooks := s.Manifest.Hooks[ev]
	if appKey != multiverseHooks {
		hooks = s.Manifest.Apps[appKey].Hooks[ev]
	}
	if len(hooks) == 0 {
		return st, nil
	}
	u := s.UniverseOf(st)
	env := s.hookEnv(u, st, ev, appKey)
	owner := cmp.Or(appKey, "multiverse")

	// Written once, and only if a command on the machine is going to read it.
	var contextFile string
	defer func() {
		if contextFile != "" {
			_ = s.Files.RemoveAll(contextFile)
		}
	}()

	for _, hook := range hooks {
		key := hook.Key(appKey, ev)
		done := slices.Contains(st.HooksDone, key)
		s.say(3, "%s: %s hook %s, once=%v, already done=%v", owner, ev, key, hook.Once, done)
		if hook.Once && done && !manual && len(hook.SkipIf) == 0 {
			continue
		}
		line := shellLine(hook.Command)
		what := cmp.Or(hook.Name, "`"+line+"`")

		// runCmd runs a command where this hook runs, and what it printed comes back.
		var runCmd func(context.Context, []string) (string, error)
		var where, byHand string
		if hook.Host {
			dir := filepath.Join(u.Dir(), hook.Dir)
			if contextFile == "" {
				contextFile = s.writeHookContext(u, st, ev, appKey)
			}
			hostEnv := cloneEnv(env)
			if contextFile != "" {
				hostEnv["MULTIVERSE_CONTEXT_FILE"] = contextFile
			}
			if s.Commands == nil {
				return st, fmt.Errorf("%s: %s: this build cannot run a command on the machine", owner, what)
			}
			runCmd = func(ctx context.Context, cmd []string) (string, error) {
				return s.Commands.RunHost(ctx, HostCommand{Args: cmd, Dir: dir, Env: hostEnv})
			}
			byHand = fmt.Sprintf("cd %s && %s", dir, line)
			where = "on this machine"
		} else {
			service := cmp.Or(hook.Service, s.Manifest.Apps[appKey].DefaultService)
			if service == "" && len(services) == 1 {
				service = services[0]
			}
			if service == "" {
				s.Out.Warn("%s: %s names no service, and the app has %d: set `container.service` on it", owner, what, len(services))
				return st, nil
			}
			r := s.hookRequest(u, st, appKey, req)
			r.ExecEnv = env
			runCmd = func(ctx context.Context, cmd []string) (string, error) {
				return s.Runtime.ExecQuiet(ctx, r, service, cmd)
			}
			byHand = fmt.Sprintf("multiversectl universe exec %s %s --service %s -- %s", st.Name, appKey, service, line)
			where = "in " + service
		}
		// A failing skipIf says the work is not known to be done, which is also
		// what it says when it could not ask yet (the service is still
		// starting). So it is asked again before every retry: the retry that
		// finds the service answering then sees the work done, instead of
		// doing it a second time.
		askSkip := len(hook.SkipIf) > 0 && !manual
		isDone := func(ctx context.Context) bool {
			if !askSkip {
				return false
			}
			_, err := runCmd(ctx, hook.SkipIf)
			return err == nil
		}
		skipped := func() {
			s.Out.Info("%s: %s is already done (`%s` succeeded), skipped", owner, what, shellLine(hook.SkipIf))
		}
		if isDone(ctx) {
			skipped()
			continue
		}
		s.Out.Info("%s: running %s %s", owner, what, where)
		tries, wasDone := 0, false
		try := func(ctx context.Context) (string, error) {
			if tries++; tries > 1 && isDone(ctx) {
				wasDone = true
				return "", nil
			}
			return runCmd(ctx, hook.Command)
		}

		attempt := func() (string, error) {
			if hook.Timeout > 0 {
				tryCtx, cancel := context.WithTimeout(ctx, hook.Timeout)
				defer cancel()
				return try(tryCtx)
			}
			return try(ctx)
		}
		out, err := s.tryFor(ctx, hook.Retry(ev, hookGiveUp), attempt)
		if err != nil {
			policy := hook.Policy(ev)
			if manual {
				policy = domain.FailStop
			}
			switch policy {
			case domain.FailIgnore:
				s.say(1, "%s: %s did not succeed (%v); ignored", owner, what, err)
				continue
			case domain.FailStop:
				return st, fmt.Errorf("%s: %s did not succeed: %w%s\nrun it yourself with `%s`", owner, what, err, indented(out), byHand)
			}
			s.Out.Warn("%s: %s did not succeed (%v). Run it yourself with `%s`, and it is tried again the next time", owner, what, err, byHand)
			if tail := lastLines(out, 6); tail != "" {
				s.Out.Plain("%s", tail)
			}
			return st, nil
		}
		if wasDone {
			skipped()
			continue
		}
		s.Out.OK("%s: ran %s", owner, what)
		if tail := lastLines(out, 20); tail != "" {
			s.say(2, "%s", strings.ReplaceAll(tail, "\n", "\n    "))
		}
		if !hook.Once || manual || len(hook.SkipIf) > 0 {
			continue
		}
		st.HooksDone = append(slices.Clone(st.HooksDone), key)
		if err := s.Store.Save(st); err != nil {
			s.Out.Warn("%s: %s ran, but that could not be kept, so it runs again the next time: %v", owner, what, err)
			return st, nil
		}
	}
	return st, nil
}

// forgetOnce drops the record of the once hooks of an app that have run, for
// when what they produced has been removed with the app's volumes.
func forgetOnce(st domain.UniverseState, appKey string) domain.UniverseState {
	prefix := appKey + "|"
	st.HooksDone = slices.DeleteFunc(slices.Clone(st.HooksDone), func(key string) bool {
		return strings.HasPrefix(key, prefix)
	})
	return st
}

// hookRequest is the request a command in a service goes through: the one the
// caller has, or the usual one for the app.
func (s *Service) hookRequest(u domain.Universe, st domain.UniverseState, appKey string, req *RuntimeRequest) RuntimeRequest {
	if req != nil {
		return *req
	}
	return s.request(u, st, appKey, nil)
}

// writeHookContext leaves the context file for a command on the machine, and
// says "" when it could not be: the variables are still given, and a hook that
// needs the file fails on its own.
func (s *Service) writeHookContext(u domain.Universe, st domain.UniverseState, ev domain.HookEvent, appKey string) string {
	data, err := json.MarshalIndent(s.hookContext(u, st, ev, appKey), "", "  ")
	if err != nil {
		return ""
	}
	name, err := s.Files.TempFile("multiverse-context-*.json")
	if err != nil {
		s.say(1, "no context file for the hooks: %v", err)
		return ""
	}
	if err := s.Files.WriteFile(name, data, 0o600); err != nil {
		_ = s.Files.RemoveAll(name)
		s.say(1, "no context file for the hooks: %v", err)
		return ""
	}
	return name
}

// tryFor runs f until it succeeds, for up to window after the first try, and
// returns what the last try printed with its error. A window of nothing is one
// try.
func (s *Service) tryFor(ctx context.Context, window time.Duration, f func() (string, error)) (string, error) {
	deadline := time.Now().Add(window)
	for {
		out, err := f()
		if err == nil {
			return out, nil
		}
		s.say(3, "not yet (%v); trying again in %s", err, hookEvery)
		if ctx.Err() != nil || time.Now().Add(hookEvery).After(deadline) {
			return out, err
		}
		select {
		case <-ctx.Done():
			return out, ctx.Err()
		case <-time.After(hookEvery):
		}
	}
}

func cloneEnv(env map[string]string) map[string]string {
	out := make(map[string]string, len(env)+1)
	for k, v := range env {
		out[k] = v
	}
	return out
}

// indented puts what a command printed under the error that quotes it.
func indented(out string) string {
	tail := lastLines(out, 6)
	if tail == "" {
		return ""
	}
	return "\n    " + strings.ReplaceAll(tail, "\n", "\n    ")
}

// shellLine writes a command as a person could paste it: an argument with
// anything a shell would split or expand is quoted.
func shellLine(args []string) string {
	words := make([]string, len(args))
	for i, a := range args {
		words[i] = a
		if a == "" || strings.ContainsAny(a, " \t\n\"'$`\\;&|<>()*?[]{}!#~") {
			words[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
	}
	return strings.Join(words, " ")
}

// HookInfo is one hook of a universe as `universe hooks` lists it.
type HookInfo struct {
	Event     domain.HookEvent
	App       string // "" for the multiverse's own
	Name      string
	Command   string
	Where     string // "host", "host in api", or "service web"
	Once      bool
	Done      bool // a Once hook that has run in this universe
	OnFailure domain.Failure
}

// Hooks lists the hooks a universe runs, in the order the events happen.
func (s *Service) Hooks(universe string) ([]HookInfo, error) {
	st, err := s.Resolve(universe)
	if err != nil {
		return nil, err
	}
	s = s.forUniverseOrSelf(st.Name)
	var out []HookInfo
	list := func(app string, hooks map[domain.HookEvent][]domain.Hook) {
		for _, ev := range domain.HookEvents {
			for _, h := range hooks[ev] {
				where := "service " + cmp.Or(h.Service, s.Manifest.Apps[app].DefaultService, "(its only one)")
				if h.Host {
					where = "host"
					if h.Dir != "" {
						where += " in " + h.Dir
					}
				}
				out = append(out, HookInfo{
					Event: ev, App: app, Name: h.Name, Command: shellLine(h.Command), Where: where,
					Once: h.Once, Done: h.Once && slices.Contains(st.HooksDone, h.Key(app, ev)), OnFailure: h.Policy(ev),
				})
			}
		}
	}
	list(multiverseHooks, s.Manifest.Hooks)
	for _, appKey := range s.Manifest.RunnableApps() {
		list(appKey, s.Manifest.Apps[appKey].Hooks)
	}
	slices.SortStableFunc(out, func(a, b HookInfo) int {
		return slices.Index(domain.HookEvents, a.Event) - slices.Index(domain.HookEvents, b.Event)
	})
	return out, nil
}

// RunHooks runs the hooks of one event on a universe now, as a person asking:
// for one app, or with none named for the multiverse's own and then every app's.
// It is how a hook that was warned about is run again, and how one is tried
// before it is trusted to a start.
func (s *Service) RunHooks(ctx context.Context, universe string, ev domain.HookEvent, apps []string) error {
	if !ev.Valid() {
		return fmt.Errorf("%q is not an event: want one of %s", ev, domain.JoinHookEvents())
	}
	st, err := s.Resolve(universe)
	if err != nil {
		return err
	}
	unlock, err := s.lockUniverse(ctx, st.ID, st.Name)
	if err != nil {
		return err
	}
	defer unlock()
	if st, err = s.Resolve(universe); err != nil {
		return err
	}
	s = s.forUniverseOrSelf(st.Name)
	targets := apps
	if len(targets) == 0 {
		targets = append([]string{multiverseHooks}, s.withoutIdleOnDemand(ctx, st, s.Manifest.RunnableApps())...)
	}
	ran := false
	for _, appKey := range targets {
		hooks := s.Manifest.Hooks[ev]
		if appKey != multiverseHooks {
			if _, ok := s.Manifest.Apps[appKey]; !ok {
				return fmt.Errorf("no app %q in this multiverse", appKey)
			}
			hooks = s.Manifest.Apps[appKey].Hooks[ev]
		}
		if len(hooks) == 0 {
			continue
		}
		ran = true
		var services []string
		// Asked of the compose files only when a command in a service does not name
		// one, since it is what lets an app with a single service leave it out.
		if appKey != multiverseHooks && needsService(hooks, s.Manifest.Apps[appKey]) {
			if cfg, err := s.inspect(ctx, s.UniverseOf(st), appKey); err == nil {
				services = cfg.Names()
			}
		}
		if st, err = s.runHooks(ctx, ev, st, appKey, nil, services, true); err != nil {
			return err
		}
	}
	if !ran {
		s.Out.Info("no %s hooks to run", ev)
	}
	return nil
}

func needsService(hooks []domain.Hook, app domain.App) bool {
	return app.DefaultService == "" && slices.ContainsFunc(hooks, func(h domain.Hook) bool { return !h.Host && h.Service == "" })
}
