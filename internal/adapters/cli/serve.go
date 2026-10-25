package cli

import (
	"context"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"
)

// ServeOptions say where the web view listens.
type ServeOptions struct {
	Addr   string
	Expose bool
}

func (r runner) serverCmd() *cobra.Command {
	var o ServeOptions
	var open bool
	cmd := &cobra.Command{
		Use:     "server",
		Aliases: []string{"serve"},
		Short:   "Serve a read-only web view of this machine's setup and fleet",
		Long: "Serves one page and a small JSON API: the root catalog as a tree, the workspaces\n" +
			"installed here and how they have drifted, and every universe of every workspace\n" +
			"with what is running in it. It changes nothing; changes are made from the\n" +
			"command line, and the page shows the command for each.\n\n" +
			"It listens on the loopback address only, because it has no login: --expose is\n" +
			"the decision to reach it from the network.",
		Example: "  multiversectl server\n  multiversectl server --open\n  multiversectl server --addr 127.0.0.1:9000",
		Args:    cobra.NoArgs,
	}
	cmd.Flags().StringVar(&o.Addr, "addr", "127.0.0.1:7777", "address to listen on (a port of 0 picks a free one)")
	cmd.Flags().BoolVar(&o.Expose, "expose", false, "allow a non-loopback address and any host name; there is no login")
	cmd.Flags().BoolVar(&open, "open", false, "open the page in a browser")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return r.f.Serve(cmd.Context(), *r.g, r.out, o, func(url string) {
			r.out.OK("serving %s (read-only; Ctrl-C stops it)", url)
			if o.Expose {
				r.out.Warn("--expose: anyone who can reach this address can read your setup")
			}
			if open {
				openBrowser(cmd.Context(), url)
			}
		})
	}
	return cmd
}

// openBrowser is best effort: the URL is printed either way.
func openBrowser(ctx context.Context, url string) {
	name := "xdg-open"
	switch runtime.GOOS {
	case "darwin":
		name = "open"
	case "windows":
		name = "rundll32"
		_ = exec.CommandContext(ctx, name, "url.dll,FileProtocolHandler", url).Start()
		return
	}
	_ = exec.CommandContext(ctx, name, url).Start()
}
