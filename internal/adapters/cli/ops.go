package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cristiansteib/multiversectl/internal/adapters/userdirs"
	"github.com/cristiansteib/multiversectl/internal/app"
	"github.com/cristiansteib/multiversectl/internal/domain"
)

// errChecksFailed makes status exit non-zero without a second message.
var errChecksFailed = errors.New("one or more checks failed")

// installPath is where multiverses will be written, for the disk check.
func (r runner) installPath(s *app.Service, base string) string {
	path := firstNonEmpty(r.g.Root, s.Multiverse, base, userdirs.RootEnv(), userdirs.DefaultRoot())
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home := s.System.Getenv("HOME"); home != "" {
			path = filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	return path
}

// reportPreflight prints what a preflight found and fails when anything stops
// a start. With all, what is fine is printed too.
func (r runner) reportPreflight(checks []app.Check, all bool) error {
	for _, c := range checks {
		line := c.Name + ": " + c.Detail
		switch c.Status {
		case app.StatusOK:
			if all || r.g.Verbose > 0 {
				r.out.OK("%s", line)
			}
		case app.StatusWarn:
			r.out.Warn("%s", line)
		default:
			r.out.Fail("%s", line)
		}
	}
	if app.PreflightFailed(checks) {
		if all {
			return errChecksFailed
		}
		return errors.New("preflight: this machine is not ready to start; fix the above, or pass --skip-preflight to start anyway")
	}
	return nil
}

// IsSilent reports errors main should turn into an exit code without printing.
func IsSilent(err error) bool { return errors.Is(err, errChecksFailed) }

func (r runner) envCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "env <universe>",
		Short:   "Print a universe's environment as export lines for your shell",
		Example: `  eval "$(multiversectl universe env 3)"`,
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return r.withService(func(ctx context.Context, s *app.Service) error {
				args, err := r.universeArgs(cmd, args, s)
				if err != nil {
					return err
				}
				env, err := s.Env(ctx, args[0])
				if err != nil {
					return err
				}
				keys := sortedKeys(env)
				for _, k := range keys {
					s.Out.Plain("%s", exportLine(k, env[k]))
				}
				return nil
			})(cmd, args)
		},
	}
}

// exportLine single-quotes the value so the shell takes it literally.
func exportLine(key, value string) string {
	return "export " + key + "=" + "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func (r runner) proxyCmd() *cobra.Command {
	proxy := &cobra.Command{Use: "proxy", Short: "Manage the shared ingress every universe is reached through"}
	var rebind bool
	up := &cobra.Command{
		Use: "up", Short: "Start the proxy and its network", Args: cobra.NoArgs,
		RunE: r.withService(func(ctx context.Context, s *app.Service) error {
			dom, err := s.ResolveDomain(ctx)
			if err != nil {
				return err
			}
			if err := s.EnsureProxy(ctx, dom, rebind); err != nil {
				return err
			}
			s.Out.OK("proxy up for *.%s", dom)
			return nil
		}),
	}
	up.Flags().BoolVar(&rebind, "rebind-proxy", false, "reconfigure a running proxy even though universes of other multiverses are served through it")
	proxy.AddCommand(
		up,
		&cobra.Command{
			Use: "down", Short: "Stop the proxy (universes keep their data)", Args: cobra.NoArgs,
			RunE: r.withService(func(ctx context.Context, s *app.Service) error {
				if err := s.Proxy.Down(ctx); err != nil {
					return err
				}
				s.Out.OK("proxy down")
				return nil
			}),
		},
		&cobra.Command{
			Use: "status", Short: "Show whether the proxy runs and on which ports", Args: cobra.NoArgs,
			RunE: r.withService(func(ctx context.Context, s *app.Service) error {
				st, err := s.Proxy.Status(ctx)
				if err != nil {
					return err
				}
				if !st.Running {
					s.Out.Warn("proxy is not running — start it with: `multiversectl system proxy up`")
					return nil
				}
				s.Out.OK("proxy is running")
				for _, name := range sortedKeys(st.Entrypoints) {
					s.Out.Plain("  %s  :%d", name, st.Entrypoints[name])
				}
				return nil
			}),
		},
	)
	return proxy
}

func (r runner) selfUpdateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "self-update",
		Short: "Replace this binary with the latest release",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := r.f.Bare(*r.g, r.out)
			if err != nil {
				return err
			}
			path, err := os.Executable()
			if err != nil {
				return err
			}
			if resolved, err := filepath.EvalSymlinks(path); err == nil {
				path = resolved
			}
			res, err := s.SelfUpdate(cmd.Context(), app.SelfUpdateInput{
				CurrentVersion: r.f.Version, OS: runtime.GOOS, Arch: runtime.GOARCH, BinaryPath: path,
				ServerPath: filepath.Join(filepath.Dir(path), "multiversectl-server"),
			})
			if err != nil {
				return err
			}
			if res.Updated {
				s.Out.OK("updated %s -> %s%s", res.From, res.To, map[bool]string{true: ", with the web view", false: ""}[res.Server])
				r.restartServerAfterUpdate(cmd.Context())
			} else if res.Pending != "" {
				s.Out.Warn("version %s has no binaries attached yet: it is still being published, or its release failed; try again shortly (this is %s)", res.Pending, res.From)
			} else {
				s.Out.OK("already up to date (%s)", res.From)
			}
			return nil
		},
	}
}

// restartServerAfterUpdate makes a running web view take the new build: the file
// on disk is new, but the process that is running still holds the old one. A
// server that is not running is left alone; nothing here ever starts one.
func (r runner) restartServerAfterUpdate(ctx context.Context) {
	p, err := r.f.Server()
	if err != nil {
		return
	}
	if _, err := p.Status(ctx); err != nil {
		return
	}
	info, err := p.Restart(ctx)
	if err != nil {
		r.out.Warn("the web view is still on the old build (%v): `multiversectl server restart`", err)
		return
	}
	r.out.OK("restarted the web view on the new build: `%s`", info.URL)
}

func (r runner) versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version, commit, build date and Go version",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			if wantsJSON(cmd) {
				b, _ := json.Marshal(map[string]string{"version": r.f.Version, "commit": r.f.Commit, "built": r.f.Date, "go": runtime.Version()})
				fmt.Fprintln(os.Stdout, string(b))
				return
			}
			fmt.Fprintf(os.Stdout, "multiversectl %s (commit %s, built %s, %s)\n",
				r.f.Version, r.f.Commit, r.f.Date, runtime.Version())
		},
	}
}

func (r runner) execCmd() *cobra.Command {
	return r.commandIn("exec", "Run a command in a universe's running container", true,
		"  multiversectl universe exec 3 api --service db -- psql -U postgres\n"+
			"  multiversectl universe exec 3 api -- bash")
}

func (r runner) runCmd() *cobra.Command {
	cmd := r.commandIn("run", "Run a one-off command in a fresh container, or the hooks of an event", false,
		"  multiversectl universe run 3 api -- python manage.py migrate\n"+
			"  multiversectl universe run 3 api --service db -- psql -U postgres\n"+
			"  multiversectl universe run 3 --hook postUp           # every app's postUp hooks, now\n"+
			"  multiversectl universe run 3 --hook preUp api")
	var hook string
	cmd.Flags().StringVar(&hook, "hook", "", "run the hooks the manifest ties to this event instead of a command (postCreate, preUp, postUp, preDown, postDown, preRemove), for the apps named or all; a once-only hook runs again and is not marked")
	_ = cmd.RegisterFlagCompletionFunc("hook", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		events := make([]string, len(domain.HookEvents))
		for i, e := range domain.HookEvents {
			events[i] = string(e)
		}
		return events, cobra.ShellCompDirectiveNoFileComp
	})
	// A hook is run again to retry one that was warned about, or to try one
	// before a start depends on it; it has no command of its own to take.
	command, args := cmd.RunE, cmd.Args
	cmd.Args = func(c *cobra.Command, a []string) error {
		if hook == "" {
			return args(c, a)
		}
		return cobra.MinimumNArgs(1)(c, a)
	}
	cmd.RunE = func(c *cobra.Command, a []string) error {
		if hook == "" {
			return command(c, a)
		}
		// Said here, not by Args, whose errors are all reworded as a usage.
		if c.ArgsLenAtDash() >= 0 {
			return errors.New("--hook runs the manifest's commands for that event; it takes no command after --")
		}
		return r.withService(func(ctx context.Context, s *app.Service) error {
			return s.RunHooks(ctx, a[0], domain.HookEvent(hook), a[1:])
		})(c, a)
	}
	return cmd
}

// commandIn builds run and exec, which differ only in where they land.
func (r runner) commandIn(name, short string, inRunning bool, example string) *cobra.Command {
	var service string
	cmd := &cobra.Command{
		Use:     name + " <universe> <app> -- <command>...",
		Short:   short,
		Example: example,
		Args:    cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return r.withService(func(ctx context.Context, s *app.Service) error {
				return s.Run(ctx, app.RunInput{
					Universe: args[0], App: args[1], Service: service, Args: args[2:], InRunning: inRunning,
				})
			})(cmd, args)
		},
	}
	cmd.Flags().StringVar(&service, "service", "", "service to run in (default: the app's defaultService, or its only service)")
	return cmd
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
