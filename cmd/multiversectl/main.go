package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
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
		Version:    version,
		Commit:     commit,
		Date:       date,
		Multiverse: multiverse,
		Bare:       bare,
		At:         at,
		Catalogs:   catalogs,
		Server:     wire.Server,
		Detach:     detach,
		Progress:   func() app.Progress { return wire.Machine{Ctx: interrupted}.Progress() },
		Prompter:   func() app.Prompter { return wire.Machine{Ctx: interrupted}.Prompter() },
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

// detach starts this program again with args and does not wait for it: its
// own session, no terminal, so it neither holds the shell open nor is stopped
// with it. Best effort, like what it is used for.
func detach(args ...string) {
	self, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(self, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if cmd.Start() == nil {
		_ = cmd.Process.Release()
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

// multiverse resolves root and manifest: flag, $MULTIVERSECTL_ROOT, the
// manifest's own spec.root, then the installed multiverse (-m, else the one
// whose folder this is, else the only one), then the manifest found upwards
// from the current directory.
func multiverse(g cli.Globals, out app.Output) (*app.Service, error) {
	store, cfg, err := wire.LoadConfig()
	if err != nil {
		return nil, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	var saved app.MultiverseEntry
	// A manifest named with -f says which multiverse this is by itself, so
	// the choice among installed ones is not made for it.
	if g.Manifest == "" || g.Multiverse != "" {
		if saved, _, err = cfg.Select(g.Multiverse, cwd); err != nil {
			return nil, err
		}
	}
	root := wire.First(g.Root, os.Getenv("MULTIVERSECTL_ROOT"), saved.Root)
	path := g.Manifest
	if path == "" && saved.Manifest != "" && (root == "" || root == saved.Root) {
		path = saved.Manifest
	}
	if path == "" {
		start := root
		if start == "" {
			start = cwd
		}
		if path, err = wire.Locate(start); err != nil {
			return nil, fmt.Errorf("no multiverse on this machine: `multiversectl start <owner/repo>`  (or -f <manifest>): %w", err)
		}
	}
	m, err := manifest.Load(path)
	if err != nil {
		return nil, err
	}
	// spec.root is what lets one directory hold a manifest per multiverse and
	// still be usable from anywhere: multiversectl -f acme.yaml universe up 3.
	if root == "" {
		root = wire.First(m.Root, filepath.Dir(path))
	}
	if err := manifest.MergeParts(m, root); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
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
