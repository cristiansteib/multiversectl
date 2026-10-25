// Package wire is the composition root both binaries share: it is the one
// place that names adapters, so the CLI and the server read the same machine
// the same way.
package wire

import (
	"context"
	"io"
	"os"
	"path/filepath"

	"github.com/cristiansteib/multiversectl/internal/adapters/catalog"
	"github.com/cristiansteib/multiversectl/internal/adapters/compose"
	"github.com/cristiansteib/multiversectl/internal/adapters/config"
	"github.com/cristiansteib/multiversectl/internal/adapters/fsx"
	"github.com/cristiansteib/multiversectl/internal/adapters/gitcli"
	"github.com/cristiansteib/multiversectl/internal/adapters/githost"
	"github.com/cristiansteib/multiversectl/internal/adapters/manifest"
	"github.com/cristiansteib/multiversectl/internal/adapters/prompt"
	"github.com/cristiansteib/multiversectl/internal/adapters/release"
	"github.com/cristiansteib/multiversectl/internal/adapters/serverproc"
	"github.com/cristiansteib/multiversectl/internal/adapters/statestore"
	"github.com/cristiansteib/multiversectl/internal/adapters/system"
	"github.com/cristiansteib/multiversectl/internal/adapters/traefik"
	"github.com/cristiansteib/multiversectl/internal/app"
	"github.com/cristiansteib/multiversectl/internal/domain"
)

// Machine wires services for one binary and one version.
type Machine struct {
	Version string
	// Verbose shows the stdout of the commands being run.
	Verbose bool
	// Ctx ends a question waiting on the terminal; nil means never.
	Ctx context.Context
}

func (m Machine) ctx() context.Context {
	if m.Ctx == nil {
		return context.Background()
	}
	return m.Ctx
}

// LoadConfig opens the per-machine config and reads it.
func LoadConfig() (app.ConfigStore, app.Config, error) {
	store, err := config.New()
	if err != nil {
		return nil, app.Config{}, err
	}
	cfg, err := store.Load()
	return store, cfg, err
}

// Deps wires every adapter. Child-process stdout is shown only when Verbose;
// stderr, where the runtime reports progress and failures, always is.
func (m Machine) Deps(out app.Output, root string, cfg app.ConfigStore) app.Deps {
	var childOut io.Writer = io.Discard
	if m.Verbose {
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
		Manifests: ManifestLoader{},
		Releases:  release.New(),
		Out:       out,
	}
}

// Service builds a service for the manifest at path.
func (m Machine) Service(out app.Output, store app.ConfigStore, root, path string) (*app.Service, error) {
	mf, err := manifest.Load(path)
	if err != nil {
		return nil, err
	}
	return app.New(m.Deps(out, root, store), root, mf, m.Version), nil
}

// At builds a service for an explicit root and manifest file.
func (m Machine) At(out app.Output, root, path string) (*app.Service, error) {
	store, _, err := LoadConfig()
	if err != nil {
		return nil, err
	}
	return m.Service(out, store, root, path)
}

// Bare builds a service that needs no manifest: it knows the machine, not a
// workspace.
func (m Machine) Bare(out app.Output, root string) (*app.Service, error) {
	store, _, err := LoadConfig()
	if err != nil {
		return nil, err
	}
	return app.New(m.Deps(out, root, store), root, nil, m.Version), nil
}

// Catalogs builds the catalog service, which never needs a workspace.
func (m Machine) Catalogs(out app.Output) (*app.Catalogs, error) {
	store, _, err := LoadConfig()
	if err != nil {
		return nil, err
	}
	d := m.Deps(out, "", store)
	return app.NewCatalogs(app.CatalogDeps{
		Git:       d.Git,
		Files:     d.Files,
		Decoder:   catalog.Decoder{},
		Editor:    catalog.Editor{},
		Prompt:    prompt.New(m.ctx()),
		Host:      githost.New(),
		Config:    store,
		Manifests: ManifestLoader{},
		Out:       out,
		CacheDir:  filepath.Join(filepath.Dir(store.Path()), "catalogs"),
	}), nil
}

// Server builds the port that runs multiversectl-server in the background.
func Server() (app.ServerProcess, error) { return serverproc.New() }

// Completions builds the use case that installs shell completions.
func (m Machine) Completions(out app.Output) *app.Completions {
	return app.NewCompletions(app.CompletionDeps{
		Files: fsx.New(), System: system.New(), Prompt: prompt.New(m.ctx()), Out: out, Version: m.Version,
	})
}

// ManifestLoader adapts manifest.Load to the app.ManifestLoader port.
type ManifestLoader struct{}

func (ManifestLoader) Load(path string) (*domain.Manifest, error) { return manifest.Load(path) }

// Locate finds the manifest at or above dir.
func Locate(dir string) (string, error) { return manifest.Locate(dir) }

// First returns the first non-empty value.
func First(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
