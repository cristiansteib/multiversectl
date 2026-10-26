package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/cristiansteib/multiversectl/internal/adapters/cli"
	"github.com/cristiansteib/multiversectl/internal/adapters/manifest"
	"github.com/cristiansteib/multiversectl/internal/app"
	"github.com/cristiansteib/multiversectl/internal/wire"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	ctx := interruptible()

	cmd, globals := cli.New(cli.Factory{
		Version:   version,
		Commit:    commit,
		Date:      date,
		Workspace: workspace,
		Bare:      bare,
		At:        at,
		Catalogs:  catalogs,
		Server:    wire.Server,
		Progress:  func() app.Progress { return wire.Machine{Ctx: interrupted}.Progress() },
		Prompter:  func() app.Prompter { return wire.Machine{Ctx: interrupted}.Prompter() },
		Completions: func(out app.Output) *app.Completions {
			return wire.Machine{Version: version, Ctx: interrupted}.Completions(out)
		},
		Skills: func(out app.Output) *app.Skills {
			return wire.Machine{Version: version, Ctx: interrupted}.Skills(out)
		},
	})
	if err := cmd.ExecuteContext(ctx); err != nil {
		if ctx.Err() != nil || errors.Is(err, context.Canceled) {
			// Whatever the operation reported on its way out is a symptom of
			// being stopped; saying so is what the person needs.
			fmt.Fprintln(os.Stderr, "interrupted")
			os.Exit(130)
		}
		if !cli.IsSilent(err) && globals.JSON {
			b, _ := json.Marshal(map[string]string{"level": "error", "message": cli.Markup(err.Error(), false)})
			fmt.Fprintln(os.Stderr, string(b))
		} else if !cli.IsSilent(err) {
			fmt.Fprintln(os.Stderr, errorPrefix(globals.NoColor)+cli.Markup(err.Error(), cli.ColorEnabled(os.Stderr, globals.NoColor)))
		}
		os.Exit(1)
	}
}

// interrupted ends the operations and the questions that are waiting. It is a
// package variable because the factory functions below are called by the
// command line without a context of their own.
var interrupted = context.Background()

// interruptible cancels the returned context on the first Ctrl+C, which lets
// the operation stop what it started, and exits on the second, for the one that
// does not.
func interruptible() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	sig := make(chan os.Signal, 2)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		cancel()
		<-sig
		fmt.Fprintln(os.Stderr, "\nforced exit")
		os.Exit(130)
	}()
	interrupted = ctx
	return ctx
}

func errorPrefix(noColor bool) string {
	if cli.ColorEnabled(os.Stderr, noColor) {
		return "\x1b[31merror:\x1b[0m "
	}
	return "error: "
}

func machine(g cli.Globals) wire.Machine {
	return wire.Machine{Version: version, Verbose: g.Verbose, Ctx: interrupted}
}

// project resolves root and manifest: flag, $MULTIVERSECTL_ROOT, the
// manifest's own spec.root, the config file's project (-w, else the current
// one), then the manifest found upwards from the current directory.
func workspace(g cli.Globals, out app.Output) (*app.Service, error) {
	store, cfg, err := wire.LoadConfig()
	if err != nil {
		return nil, err
	}
	saved, _, err := cfg.Select(g.Workspace)
	if err != nil {
		return nil, err
	}
	root := wire.First(g.Root, os.Getenv("MULTIVERSECTL_ROOT"), saved.Root)
	path := g.Manifest
	if path == "" && saved.Manifest != "" && (root == "" || root == saved.Root) {
		path = saved.Manifest
	}
	if path == "" {
		start := root
		if start == "" {
			if start, err = os.Getwd(); err != nil {
				return nil, err
			}
		}
		if path, err = wire.Locate(start); err != nil {
			return nil, fmt.Errorf("no project on this machine: `multiversectl catalog install <owner/repo>`  (or -f <manifest>): %w", err)
		}
	}
	m, err := manifest.Load(path)
	if err != nil {
		return nil, err
	}
	// spec.root is what lets one directory hold a manifest per project and
	// still be usable from anywhere: multiversectl -f acme.yaml universe up 3.
	if root == "" {
		root = wire.First(m.Root, filepath.Dir(path))
	}
	return app.New(machine(g).Deps(out, root, store), root, m, version), nil
}

func at(g cli.Globals, out app.Output, root, manifestPath string) (*app.Service, error) {
	return machine(g).At(out, root, manifestPath)
}

// bare is for commands that must work before any manifest exists.
func bare(g cli.Globals, out app.Output) (*app.Service, error) {
	return machine(g).Bare(out, wire.First(g.Root, os.Getenv("MULTIVERSECTL_ROOT")))
}

func catalogs(g cli.Globals, out app.Output) (*app.Catalogs, error) {
	return machine(g).Catalogs(out)
}
