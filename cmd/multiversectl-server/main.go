// Command multiversectl-server serves a read-only web view of a machine's
// setup and fleet. It is a separate binary from multiversectl so the command
// line carries no HTTP server, and a server can be installed — or left off —
// without touching the tool that runs universes.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/cristiansteib/multiversectl/internal/adapters/server"
	"github.com/cristiansteib/multiversectl/internal/adapters/userdirs"
	"github.com/cristiansteib/multiversectl/internal/app"
	"github.com/cristiansteib/multiversectl/internal/wire"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

const usage = `multiversectl-server serves a read-only web view of this machine's setup and fleet.

It serves one page, a JSON API and a websocket: the root catalog as a tree, the
projects installed here and how they have drifted, every universe of every
project with what is running in it, the versions of what it all runs on, and
buttons to start, stop, create and remove universes and to update a project.
What changes is pushed to the page as it happens.

Because the page can act, it is opened with a link that carries a token, kept in
server.token beside the configuration so it survives a restart (delete the file
to change it); without it nothing is served. It listens on the
loopback address only: --expose is the decision to reach it from the network,
and the token still applies. --read-only removes every action.

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
	readOnly := fs.Bool("read-only", false, "offer no action: the page only shows")
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
	// --expose is the decision to be reachable from the network, and a server
	// still bound to the loopback address is reachable from nothing. Asking
	// for an address as well says which one, and is left alone.
	chosen := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "addr" {
			chosen = true
		}
	})
	if *expose && !chosen {
		*addr = "0.0.0.0:7777"
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
	machine, err := m.Bare(quiet, userdirs.RootEnv())
	if err != nil {
		return err
	}
	backend := server.NewServices(cats, machine, func(out app.Output, stdout, stderr io.Writer, root, manifestPath string) (*app.Service, error) {
		// What an action's commands print goes to the page, not to this process.
		return wire.Machine{Version: version, Stdout: stdout, Stderr: stderr}.At(out, root, manifestPath)
	})
	// The same token across restarts, so the link and any tool configured with it
	// survive an update.
	tokenPath, err := server.TokenPath()
	if err != nil {
		return err
	}
	token, err := server.PersistentToken(tokenPath)
	if err != nil {
		return fmt.Errorf("keeping the token: %w", err)
	}
	opts := server.Options{Addr: *addr, Version: version, Commit: commit, Date: date, Expose: *expose, ReadOnly: *readOnly, Token: token}
	return server.Serve(ctx, backend, opts, func(url string) {
		mode := "it can start, stop, create and remove universes"
		if *readOnly {
			mode = "read-only"
		}
		fmt.Printf("serving %s (%s; Ctrl-C stops it)\n", url, mode)
		if *expose {
			for _, at := range server.ReachableAt(*addr) {
				fmt.Printf("from this network at %s — multiversectl server pair gives a code to open it with\n", at)
			}
			fmt.Fprintln(os.Stderr, "warning: --expose: anyone who can reach this address and has the link or a code can act on your setup")
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
