package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/cristiansteib/multiversectl/internal/adapters/catalog"
	"github.com/cristiansteib/multiversectl/internal/adapters/cli"
	"github.com/cristiansteib/multiversectl/internal/adapters/compose"
	"github.com/cristiansteib/multiversectl/internal/adapters/config"
	"github.com/cristiansteib/multiversectl/internal/adapters/fsx"
	"github.com/cristiansteib/multiversectl/internal/adapters/gitcli"
	"github.com/cristiansteib/multiversectl/internal/adapters/githost"
	"github.com/cristiansteib/multiversectl/internal/adapters/manifest"
	"github.com/cristiansteib/multiversectl/internal/adapters/prompt"
	"github.com/cristiansteib/multiversectl/internal/adapters/release"
	"github.com/cristiansteib/multiversectl/internal/adapters/statestore"
	"github.com/cristiansteib/multiversectl/internal/adapters/system"
	"github.com/cristiansteib/multiversectl/internal/adapters/traefik"
	"github.com/cristiansteib/multiversectl/internal/app"
	"github.com/cristiansteib/multiversectl/internal/domain"
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

// deps wires every adapter. Child-process stdout is shown only with -v;
// stderr, where the runtime reports progress and failures, always is.
func deps(g cli.Globals, out app.Output, root string, cfg app.ConfigStore) app.Deps {
	var childOut io.Writer = io.Discard
	if g.Verbose {
		childOut = os.Stdout
	}
	proxy := traefik.New(childOut, os.Stderr)
	return app.Deps{
		Git:       gitcli.New(),
		Runtime:   compose.New(childOut, os.Stderr),
		Proxy:     proxy,
		Router:    proxy,
		Files:     fsx.New(),
		System:    system.New(),
		Store:     statestore.New(root),
		Config:    cfg,
		Manifests: manifestLoader{},
		Releases:  release.New(),
		Out:       out,
	}
}

func loadConfig() (app.ConfigStore, app.Config, error) {
	store, err := config.New()
	if err != nil {
		return nil, app.Config{}, err
	}
	cfg, err := store.Load()
	return store, cfg, err
}

// workspace resolves root and manifest: flag, $MULTIVERSECTL_ROOT, the
// manifest's own spec.root, the config file's workspace (-w, else the current
// one), then the manifest found upwards from the current directory.
func workspace(g cli.Globals, out app.Output) (*app.Service, error) {
	store, cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}
	saved, _, err := cfg.Select(g.Workspace)
	if err != nil {
		return nil, err
	}
	root := first(g.Root, os.Getenv("MULTIVERSECTL_ROOT"), saved.Root)
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
		if path, err = manifest.Locate(start); err != nil {
			return nil, fmt.Errorf("%w (run bootstrap, or pass --manifest)", err)
		}
	}
	m, err := manifest.Load(path)
	if err != nil {
		return nil, err
	}
	// spec.root is what lets one directory hold a manifest per project and
	// still be usable from anywhere: multiversectl -f acme.yaml up 3.
	if root == "" {
		root = first(m.Root, filepath.Dir(path))
	}
	return app.New(deps(g, out, root, store), root, m, version), nil
}

func at(g cli.Globals, out app.Output, root, manifestPath string) (*app.Service, error) {
	store, _, err := loadConfig()
	if err != nil {
		return nil, err
	}
	return build(g, out, store, root, manifestPath)
}

// bare is for commands that must work before any manifest exists.
func bare(g cli.Globals, out app.Output) (*app.Service, error) {
	store, _, err := loadConfig()
	if err != nil {
		return nil, err
	}
	root := first(g.Root, os.Getenv("MULTIVERSECTL_ROOT"))
	return app.New(deps(g, out, root, store), root, nil, version), nil
}

func catalogs(g cli.Globals, out app.Output) (*app.Catalogs, error) {
	store, _, err := loadConfig()
	if err != nil {
		return nil, err
	}
	d := deps(g, out, "", store)
	return app.NewCatalogs(app.CatalogDeps{
		Git:      d.Git,
		Files:    d.Files,
		Decoder:  catalog.Decoder{},
		Editor:   catalog.Editor{},
		Prompt:   prompt.New(),
		Host:     githost.New(),
		Config:   store,
		Out:      out,
		CacheDir: filepath.Join(filepath.Dir(store.Path()), "catalogs"),
	}), nil
}

func build(g cli.Globals, out app.Output, store app.ConfigStore, root, path string) (*app.Service, error) {
	m, err := manifest.Load(path)
	if err != nil {
		return nil, err
	}
	return app.New(deps(g, out, root, store), root, m, version), nil
}

func first(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// manifestLoader adapts manifest.Load to the app.ManifestLoader port.
type manifestLoader struct{}

func (manifestLoader) Load(path string) (*domain.Manifest, error) { return manifest.Load(path) }
