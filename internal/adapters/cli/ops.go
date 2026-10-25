package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cristiansteib/multiversectl/internal/app"
)

// errChecksFailed makes doctor exit non-zero without a second message.
var errChecksFailed = errors.New("one or more checks failed")

func (r runner) doctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "doctor [universe]",
		Short:   "Check the host, the workspace and optionally one universe; exits 1 on any failure",
		Example: "  multiversectl doctor\n  multiversectl doctor 3\n  multiversectl doctor fix/login",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			universe := ""
			if len(args) == 1 {
				universe = args[0]
			}
			return r.withService(func(ctx context.Context, s *app.Service) error {
				return r.runDoctor(ctx, s, universe)
			})(cmd, args)
		},
	}
}

func (r runner) runDoctor(ctx context.Context, s *app.Service, universe string) error {
	report, err := s.Doctor(ctx, app.DoctorInput{Universe: universe})
	if err != nil {
		return err
	}
	failed := false
	for _, c := range report.Checks {
		line := c.Name
		if c.Detail != "" {
			line += ": " + c.Detail
		}
		switch c.Status {
		case app.StatusOK:
			r.out.OK("%s", line)
		case app.StatusWarn:
			r.out.Warn("%s", line)
		default:
			failed = true
			r.out.Fail("%s", line)
		}
	}
	if failed {
		return errChecksFailed
	}
	return nil
}

// IsSilent reports errors main should turn into an exit code without printing.
func IsSilent(err error) bool { return errors.Is(err, errChecksFailed) }

func (r runner) envCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "env <universe>",
		Short:   "Print a universe's environment as export lines for your shell",
		Example: `  eval "$(multiversectl env 3)"`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return r.withService(func(ctx context.Context, s *app.Service) error {
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

func (r runner) bootstrapCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "bootstrap <manifest-url-or-path>",
		Short:   "Set up a workspace: record it, clone its repos, run doctor",
		Example: "  multiversectl bootstrap git@github.com:acme/workspace.git --root ~/acme\n  multiversectl bootstrap ./multiverse.yaml --root ~/acme",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			bare, err := r.f.Bare(*r.g, r.out)
			if err != nil {
				return err
			}
			source := args[0]
			if abs, err := filepath.Abs(source); err == nil {
				if _, err := os.Stat(abs); err == nil {
					source = abs
				}
			}
			root := firstNonEmpty(r.g.Root, os.Getenv("MULTIVERSECTL_ROOT"), "~/multiverse")
			cfg, err := bare.Bootstrap(ctx, app.BootstrapInput{Root: root, ManifestSource: source})
			if err != nil {
				return err
			}
			s, err := r.f.At(*r.g, r.out, cfg.Root, cfg.Manifest)
			if err != nil {
				return err
			}
			if err := s.CloneRepos(ctx); err != nil {
				return err
			}
			return r.runDoctor(ctx, s, "")
		},
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func (r runner) proxyCmd() *cobra.Command {
	proxy := &cobra.Command{Use: "proxy", Short: "Manage the shared ingress every universe is reached through"}
	proxy.AddCommand(
		&cobra.Command{
			Use: "up", Short: "Start the proxy and its network", Args: cobra.NoArgs,
			RunE: r.withService(func(ctx context.Context, s *app.Service) error {
				dom, err := s.ResolveDomain(ctx)
				if err != nil {
					return err
				}
				if err := s.EnsureProxy(ctx, dom); err != nil {
					return err
				}
				s.Out.OK("proxy up for *.%s", dom)
				return nil
			}),
		},
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
					s.Out.Warn("proxy is not running — start it with: multiversectl proxy up")
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

func (r runner) updateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "update",
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
			})
			if err != nil {
				return err
			}
			if res.Updated {
				s.Out.OK("updated %s -> %s", res.From, res.To)
			} else {
				s.Out.OK("already up to date (%s)", res.From)
			}
			return nil
		},
	}
}

func (r runner) versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version, commit, build date and Go version",
		Args:  cobra.NoArgs,
		Run: func(*cobra.Command, []string) {
			fmt.Fprintf(os.Stdout, "multiversectl %s (commit %s, built %s, %s)\n",
				r.f.Version, r.f.Commit, r.f.Date, runtime.Version())
		},
	}
}

func (r runner) execCmd() *cobra.Command {
	return r.commandIn("exec", "Run a command in a universe's running container", true,
		"  multiversectl exec 3 api --service db -- psql -U postgres\n"+
			"  multiversectl exec 3 api -- bash")
}

func (r runner) runCmd() *cobra.Command {
	return r.commandIn("run", "Run a one-off command in a fresh container", false,
		"  multiversectl run 3 api -- python manage.py migrate\n"+
			"  multiversectl run 3 api --service db -- psql -U postgres")
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
	cmd.Flags().StringVar(&service, "service", "", "service to run in (default: the app's only service)")
	return cmd
}
