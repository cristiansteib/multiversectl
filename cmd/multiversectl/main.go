package main

import (
	"context"
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cmd, globals := cli.New(cli.Factory{
		Version:   version,
		Commit:    commit,
		Date:      date,
		Workspace: workspace,
		Bare:      bare,
		At:        at,
		Catalogs:  catalogs,
	})
	if err := cmd.ExecuteContext(ctx); err != nil {
		if !cli.IsSilent(err) {
			fmt.Fprintln(os.Stderr, errorPrefix(globals.NoColor)+err.Error())
		}
		os.Exit(1)
	}
}

func errorPrefix(noColor bool) string {
	if cli.ColorEnabled(os.Stderr, noColor) {
		return "\x1b[31merror:\x1b[0m "
	}
	return "error: "
}

func machine(g cli.Globals) wire.Machine {
	return wire.Machine{Version: version, Verbose: g.Verbose}
}

// workspace resolves root and manifest: flag, $MULTIVERSECTL_ROOT, the
// manifest's own spec.root, the config file's workspace (-w, else the current
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
			return nil, fmt.Errorf("no workspace on this machine: multiversectl install <owner/repo>  (or -f <manifest>): %w", err)
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
