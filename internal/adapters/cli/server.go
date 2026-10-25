package cli

import (
	"context"
	"errors"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/cristiansteib/multiversectl/internal/app"
)

// serverCmd starts and stops the web view. The view itself is the separate
// multiversectl-server program; this runs it in the background and remembers
// it, so a person does not keep a terminal open for it.
func (r runner) serverCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Start, stop and check the read-only web view of this machine's setup and fleet",
		Long: "The web view is a separate program, multiversectl-server, installed beside this\n" +
			"one. These commands run it in the background and remember it. It only reads,\n" +
			"listens on the loopback address, and has no login.",
		Example: "  multiversectl server start --open\n  multiversectl server status\n  multiversectl server stop",
		Args:    cobra.NoArgs,
		RunE:    func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(r.serverStartCmd(), r.serverStopCmd(), r.serverStatusCmd())
	return cmd
}

func (r runner) serverStartCmd() *cobra.Command {
	var addr string
	var expose, open bool
	cmd := &cobra.Command{
		Use:     "start",
		Short:   "Start the web view in the background",
		Example: "  multiversectl server start\n  multiversectl server start --open\n  multiversectl server start --addr 127.0.0.1:9000",
		Args:    cobra.NoArgs,
	}
	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:7777", "address to listen on (a port of 0 picks a free one)")
	cmd.Flags().BoolVar(&expose, "expose", false, "allow a non-loopback address and any host name; there is no login")
	cmd.Flags().BoolVar(&open, "open", false, "open the page in a browser")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		p, err := r.f.Server()
		if err != nil {
			return err
		}
		var info app.ServerInfo
		err = r.spin(true, "Starting the server", func() (err error) {
			info, err = p.Start(cmd.Context(), app.ServerStart{Addr: addr, Expose: expose})
			return err
		})
		switch {
		case errors.Is(err, app.ErrServerRunning):
			r.out.Info("already running at `%s` (pid %d); stop it with: `multiversectl server stop`", info.URL, info.PID)
		case err != nil:
			return err
		default:
			r.out.OK("serving `%s` in the background (pid %d, read-only); stop it with: `multiversectl server stop`", info.URL, info.PID)
			if expose {
				r.out.Warn("--expose: anyone who can reach this address can read your setup")
			}
		}
		if open {
			openBrowser(cmd.Context(), info.URL)
		}
		return nil
	}
	return cmd
}

func (r runner) serverStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the web view",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := r.f.Server()
			if err != nil {
				return err
			}
			info, err := p.Stop(cmd.Context())
			if errors.Is(err, app.ErrServerNotRunning) {
				r.out.Info("the server is not running: `multiversectl server start`")
				return nil
			}
			if err != nil {
				return err
			}
			r.out.OK("stopped the server at `%s` (pid %d)", info.URL, info.PID)
			return nil
		},
	}
}

func (r runner) serverStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Say whether the web view is running, and where",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := r.f.Server()
			if err != nil {
				return err
			}
			info, err := p.Status(cmd.Context())
			if errors.Is(err, app.ErrServerNotRunning) {
				r.out.Info("the server is not running: `multiversectl server start`")
				return nil
			}
			if err != nil {
				return err
			}
			r.out.OK("running at `%s` (pid %d, started %s); log: `%s`", info.URL, info.PID, age(info.Started), info.Log)
			return nil
		},
	}
}

// openBrowser is best effort: the URL is printed either way.
func openBrowser(ctx context.Context, url string) {
	switch runtime.GOOS {
	case "darwin":
		_ = exec.CommandContext(ctx, "open", url).Start()
	default:
		_ = exec.CommandContext(ctx, "xdg-open", url).Start()
	}
}
