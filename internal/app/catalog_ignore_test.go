package app_test

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cristiansteib/multiversectl/internal/adapters/catalog"
	"github.com/cristiansteib/multiversectl/internal/adapters/fsx"
	"github.com/cristiansteib/multiversectl/internal/app"
)

func ignoringCatalogs(t *testing.T, ignored ...string) (*app.Catalogs, *memConfig, *fakeRepos, string) {
	t.Helper()
	local, remote := t.TempDir(), t.TempDir()
	write(t, local, "catalog.yaml", cat("me", `  catalogs:
    - name: vpn
      source: {repo: git@vpn.example.test:acme/index.git}
    - name: open
      source: {repo: git@example.test:acme/open.git}
`))
	write(t, remote, "catalog.yaml", cat("open", `  multiverses:
    - name: billing
      source: {repo: git@example.test:me/billing.git}
    - name: search
      source: {repo: git@example.test:me/search.git}
`))
	git := &fakeRepos{repos: map[string]string{"git@example.test:acme/open.git": remote}, hang: map[string]bool{"git@vpn.example.test:acme/index.git": true}}
	cfg := &memConfig{c: app.Config{Catalog: filepath.Join(local, "catalog.yaml"), Ignored: ignored}}
	c := app.NewCatalogs(app.CatalogDeps{Git: git, Files: fsx.New(), Decoder: catalog.Decoder{}, CacheDir: t.TempDir(), Config: cfg, Timeout: 5 * time.Second, Out: &quiet{}})
	return c, cfg, git, local
}

// An ignored catalog is not read at all: an unreachable one costs no wait and is
// not tried.
func TestIgnoredCatalogIsNotReadOrFetched(t *testing.T) {
	c, _, git, _ := ignoringCatalogs(t, "vpn")
	start := time.Now()
	root, err := c.Tree(context.Background(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > time.Second {
		t.Errorf("took %s", time.Since(start))
	}
	n, ok := root.Find("vpn")
	if !ok || !n.Ignored || len(n.Children) != 0 || n.Unavailable != "" {
		t.Fatalf("vpn = %+v", n)
	}
	if slices.Contains(git.tried, "git@vpn.example.test:acme/index.git") {
		t.Error("an ignored catalog was fetched")
	}
	if _, ok := root.Find("open/billing"); !ok {
		t.Error("the rest of the tree is not read")
	}
}

func TestIgnoreSavesAndUnignoreRestores(t *testing.T) {
	c, cfg, _, _ := ignoringCatalogs(t)
	ctx := context.Background()
	if err := c.Ignore(ctx, []string{"open/billing", "vpn"}, true); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cfg.c.Ignored, []string{"open/billing", "vpn"}) {
		t.Fatalf("saved %v", cfg.c.Ignored)
	}
	// Ignoring a parent covers what is under it.
	if err := c.Ignore(ctx, []string{"open"}, true); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cfg.c.Ignored, []string{"open", "vpn"}) {
		t.Fatalf("a parent should replace its children: %v", cfg.c.Ignored)
	}
	if err := c.Ignore(ctx, []string{"vpn"}, false); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cfg.c.Ignored, []string{"open"}) {
		t.Fatalf("after unignore: %v", cfg.c.Ignored)
	}
	if err := c.Ignore(ctx, []string{"nope"}, true); err == nil || !strings.Contains(err.Error(), "no \"nope\"") {
		t.Errorf("a path that is not in the catalog: %v", err)
	}
	if err := c.Ignore(ctx, []string{"/"}, true); err == nil {
		t.Error("the root cannot be ignored")
	}
	if err := c.Ignore(ctx, []string{"vpn"}, false); err == nil {
		t.Error("unignoring what is not ignored should say so")
	}
}

func TestAnIgnoredMultiverseIsNotInstalledWithItsParent(t *testing.T) {
	c, _, _, _ := ignoringCatalogs(t, "open/billing")
	root, err := c.Tree(context.Background(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := root.Find("open/billing")
	if !n.Ignored {
		t.Fatal("billing should be ignored")
	}
	if s, _ := root.Find("open/search"); s.Ignored {
		t.Error("search should not be")
	}
}
