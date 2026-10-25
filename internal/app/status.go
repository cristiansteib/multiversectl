package app

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// RootStatus is the machine's root catalog, as far as it can be told without
// the network.
type RootStatus struct {
	Source      string `json:"source,omitempty"`
	Local       bool   `json:"local,omitempty"`
	NoRemote    bool   `json:"noRemote,omitempty"`
	Unavailable string `json:"unavailable,omitempty"`
}

// WorkspaceStatus is one installed workspace and how far it has drifted from
// where it came from.
type WorkspaceStatus struct {
	Name    string   `json:"name"`
	Current bool     `json:"current,omitempty"`
	Root    string   `json:"root"`
	From    string   `json:"from,omitempty"`
	Source  string   `json:"source,omitempty"`
	Notes   []string `json:"notes,omitempty"`
	Stale   bool     `json:"stale,omitempty"`
}

// StatusReport is what `status` shows.
type StatusReport struct {
	Root       RootStatus        `json:"root"`
	Workspaces []WorkspaceStatus `json:"workspaces"`
	Next       string            `json:"next,omitempty"`
}

// manifestClone is where install keeps a git-hosted manifest.
func manifestClone(root string) string { return filepath.Join(root, ".multiverse", "workspace") }

// Status reads the root catalog and every installed workspace and says where
// each has drifted: a manifest behind its remote, repositories its manifest
// lists that are not cloned, an entry the catalog dropped or repointed. Without
// fetch it compares with what was last fetched, so it is quick and offline.
func (c *Catalogs) Status(ctx context.Context, fetch bool) (StatusReport, error) {
	cfg, err := c.Config.Load()
	if err != nil {
		return StatusReport{}, err
	}
	var rep StatusReport
	var tree *domain.Node
	if cfg.Catalog != "" {
		rep.Root.Source = cfg.Catalog
		t, terr := c.tree(ctx, "", fetch, !fetch)
		if terr != nil {
			rep.Root.Unavailable = reason(terr, cfg.Catalog)
		} else {
			tree = &t
		}
		if !isGitURL(cfg.Catalog) {
			rep.Root.Local = true
			dir := filepath.Dir(cfg.Catalog)
			if _, err := c.Git.Toplevel(ctx, dir); err == nil && !c.Git.HasRemote(ctx, dir) {
				rep.Root.NoRemote = true
			}
		}
	}

	names := make([]string, 0, len(cfg.Workspaces))
	for n := range cfg.Workspaces {
		names = append(names, n)
	}
	sort.Strings(names)
	var stale []string
	for _, name := range names {
		e := cfg.Workspaces[name]
		w := WorkspaceStatus{Name: name, Current: name == cfg.Current, Root: e.Root, From: e.From, Source: shortRepo(e.Source.Repo)}
		if e.Source.Repo == "" {
			w.Source = ""
		}
		c.drift(ctx, &w, e, tree, fetch)
		if w.Stale {
			stale = append(stale, name)
		}
		rep.Workspaces = append(rep.Workspaces, w)
	}

	switch {
	case len(stale) > 0:
		rep.Next = "multiversectl catalog update " + strings.Join(stale, " ")
	case len(rep.Workspaces) == 0 && cfg.Catalog == "":
		rep.Next = "multiversectl catalog install <owner/repo>  ·  multiversectl init"
	case len(rep.Workspaces) == 0:
		rep.Next = "multiversectl catalog  ·  multiversectl catalog install <path>"
	}
	return rep, nil
}

func (c *Catalogs) drift(ctx context.Context, w *WorkspaceStatus, e WorkspaceEntry, tree *domain.Node, fetch bool) {
	note := func(stale bool, format string, a ...any) {
		w.Notes = append(w.Notes, fmt.Sprintf(format, a...))
		w.Stale = w.Stale || stale
	}
	if dir := manifestClone(e.Root); e.Source.Repo != "" && c.Files.Exists(dir) {
		fetched := true
		if fetch {
			if err := c.Git.Fetch(ctx, dir); err != nil {
				note(false, "could not fetch %s: %s", shortRepo(e.Source.Repo), reason(err, e.Source.Repo))
				fetched = false
			}
		}
		if n, err := c.Git.Behind(ctx, dir, e.Source.Ref); err == nil && n > 0 {
			when := ""
			if !fetched || !fetch {
				when = " (as of the last fetch)"
			}
			note(true, "manifest behind %d%s", n, when)
		}
	}
	if c.Manifests != nil {
		if m, err := c.Manifests.Load(e.Manifest); err != nil {
			note(false, "manifest unreadable: %v", err)
		} else {
			for _, key := range missingRepos(c.Files, m, e.Root) {
				note(true, "%s not cloned", key)
			}
		}
	}
	if e.From != "" && tree != nil {
		switch n, ok := tree.Find(e.From); {
		case !ok:
			note(false, "no longer in your catalog")
		case n.Source.Repo != e.Source.Repo || (n.Source.Repo != "" && n.Source.Path != e.Source.Path):
			note(false, "your catalog now points at %s", shortRepo(n.Source.Repo))
		}
	}
}

// missingRepos are the repositories a manifest lists that are not on disk.
func missingRepos(files Files, m *domain.Manifest, root string) []string {
	var out []string
	for key := range m.Repos {
		if !files.Exists(m.CloneDir(root, key)) {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

// MissingRepos is what an update has to clone for a workspace.
func (c *Catalogs) MissingRepos(m *domain.Manifest, root string) []string {
	return missingRepos(c.Files, m, root)
}

// UpdateResult is what refreshing one workspace's manifest did.
type UpdateResult struct {
	Name    string
	Changed bool
	Detail  string
	Skipped string
}

// RefreshWorkspace brings a workspace's manifest up to date with where it was
// installed from. Cloning the repositories it newly lists is the caller's, which
// holds a Service for the workspace.
func (c *Catalogs) RefreshWorkspace(ctx context.Context, name string, e WorkspaceEntry) (UpdateResult, error) {
	res := UpdateResult{Name: name}
	dir := manifestClone(e.Root)
	if e.Source.Repo == "" || !c.Files.Exists(dir) {
		res.Skipped = "installed from a file; nothing to update from"
		return res, nil
	}
	if clean, err := c.Git.IsClean(ctx, dir); err != nil || !clean {
		return res, fmt.Errorf("%s: local changes in `%s`; commit or stash them, then `multiversectl catalog update %s`", name, dir, name)
	}
	if err := c.Git.Fetch(ctx, dir); err != nil {
		return res, fmt.Errorf("%s: fetch %s: %s", name, shortRepo(e.Source.Repo), reason(err, e.Source.Repo))
	}
	behind, err := c.Git.Behind(ctx, dir, e.Source.Ref)
	if err != nil {
		if e.Source.Ref != "" { // a tag or commit does not move
			res.Skipped = "pinned to " + e.Source.Ref
			return res, nil
		}
		return res, fmt.Errorf("%s: %w", name, err)
	}
	if behind == 0 {
		return res, nil
	}
	branch := e.Source.Ref
	if branch == "" {
		if branch, err = c.Git.DefaultBranch(ctx, dir); err != nil {
			return res, err
		}
	}
	if err := c.Git.FastForward(ctx, dir, branch); err != nil {
		return res, fmt.Errorf("%s: cannot fast-forward the manifest (%w); it has diverged from %s", name, err, shortRepo(e.Source.Repo))
	}
	res.Changed, res.Detail = true, fmt.Sprintf("manifest moved forward %d", behind)
	return res, nil
}
