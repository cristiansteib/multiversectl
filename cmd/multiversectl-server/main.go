// Command multiversectl-server serves a read-only web view of a machine's
// setup and fleet. It is a separate binary from multiversectl so the command
// line carries no HTTP server, and a server can be installed — or left off —
// without touching the tool that runs universes.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/cristiansteib/multiversectl/internal/adapters/server"
	"github.com/cristiansteib/multiversectl/internal/app"
	"github.com/cristiansteib/multiversectl/internal/wire"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

const usage = `multiversectl-server serves a read-only web view of this machine's setup and fleet.

It serves one page and a small JSON API: the root catalog as a tree, the
workspaces installed here and how they have drifted, and every universe of every
workspace with what is running in it. It changes nothing; changes are made with
multiversectl, and the page shows the command for each.

It listens on the loopback address only, because it has no login: --expose is
the decision to reach it from the network.

Usage:
  multiversectl-server [flags]

Flags:
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error: "+err.Error())
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("multiversectl-server", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), usage)
		fs.PrintDefaults()
	}
	addr := fs.String("addr", "127.0.0.1:7777", "address to listen on (a port of 0 picks a free one)")
	expose := fs.Bool("expose", false, "allow a non-loopback address and any host name; there is no login")
	open := fs.Bool("open", false, "open the page in a browser")
	showVersion := fs.Bool("version", false, "print the version and exit")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	if *showVersion {
		fmt.Printf("multiversectl-server %s (%s, %s)\n", version, commit, date)
		return nil
	}

	// The services are built with a quiet output: a server has no terminal,
	// and a request that wants a service's warnings collects them itself.
	quiet := server.Quiet()
	m := wire.Machine{Version: version}
	cats, err := m.Catalogs(quiet)
	if err != nil {
		return err
	}
	machine, err := m.Bare(quiet, os.Getenv("MULTIVERSECTL_ROOT"))
	if err != nil {
		return err
	}
	backend := server.NewServices(cats, machine, func(out app.Output, root, manifestPath string) (*app.Service, error) {
		return m.At(out, root, manifestPath)
	})
	return server.Serve(ctx, backend, server.Options{Addr: *addr, Version: version, Expose: *expose}, func(url string) {
		fmt.Printf("serving %s (read-only; Ctrl-C stops it)\n", url)
		if *expose {
			fmt.Fprintln(os.Stderr, "warning: --expose: anyone who can reach this address can read your setup")
		}
		if *open {
			openBrowser(ctx, url)
		}
	})
}

// openBrowser is best effort: the URL is printed either way.
func openBrowser(ctx context.Context, url string) {
	switch runtime.GOOS {
	case "darwin":
		_ = exec.CommandContext(ctx, "open", url).Start()
	case "windows":
		_ = exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		_ = exec.CommandContext(ctx, "xdg-open", url).Start()
	}
}
