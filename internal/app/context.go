package app

import (
	"path/filepath"
	"sort"
)

// ProjectContext is what a person or an agent needs to find its way around the
// project a command is acting on: where its definition is, where everything
// lives on this machine, and where it came from. It is gathered from the
// configuration and the manifest, so it costs nothing to show on every listing.
type ProjectContext struct {
	// Name is what the project is installed under on this machine (what -p takes).
	Name string `json:"name"`
	// Title is metadata.name of the manifest, which prefixes every compose project.
	Title string `json:"title,omitempty"`
	// Current marks the project commands act on when no -p says otherwise.
	Current bool `json:"current,omitempty"`
	// Root is the multiverse root: the clones and the universes are under it.
	Root string `json:"root"`
	// Manifest is the project definition, the multiverse.yaml, when it is known.
	Manifest string `json:"manifest,omitempty"`
	// From is the catalog node it was installed from; Source is the repository
	// of the manifest ("github:acme/multiverse@main"), empty for a local file.
	From   string `json:"from,omitempty"`
	Source string `json:"source,omitempty"`
	// UniversesDir is where every universe's worktrees are.
	UniversesDir string        `json:"universesDir"`
	Repos        []RepoContext `json:"repos,omitempty"`
	// Config is the machine's configuration file, which records what is installed.
	Config string `json:"config,omitempty"`
}

// RepoContext is one repository of the project: where its shared clone is, which
// is where secrets are put and from which every universe branches.
type RepoContext struct {
	Key  string `json:"key"`
	URL  string `json:"url,omitempty"`
	Path string `json:"path"`
	// Runtime is "compose", or "none" for one that is only checked out.
	Runtime string `json:"runtime"`
}

// Context describes the project this service acts on. A project that is not
// installed here (it was reached with a flag or the environment) has no name
// or origin to give, and still has its root and clones.
func (s *Service) Context() ProjectContext {
	c := ProjectContext{
		Root:         s.Workspace,
		UniversesDir: filepath.Join(s.Workspace, "universes"),
	}
	if s.Manifest != nil {
		c.Title = s.Manifest.Name
		keys := make([]string, 0, len(s.Manifest.Repos))
		for key := range s.Manifest.Repos {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			r := s.Manifest.Repos[key]
			runtime := string(r.Runtime)
			if runtime == "" {
				runtime = "compose"
			}
			c.Repos = append(c.Repos, RepoContext{Key: key, URL: r.URL, Path: s.Manifest.CloneDir(s.Workspace, key), Runtime: runtime})
		}
	}
	if s.Config != nil {
		c.Config = s.Config.Path()
		if cfg, err := s.Config.Load(); err == nil {
			for name, e := range cfg.Workspaces {
				if e.Root != s.Workspace {
					continue
				}
				c.Name, c.Current = name, name == cfg.Current
				c.Manifest, c.From = e.Manifest, e.From
				if e.Source.Repo != "" {
					c.Source = shortRepo(e.Source.Repo)
					if e.Source.Ref != "" {
						c.Source += "@" + e.Source.Ref
					}
				}
				break
			}
		}
	}
	if c.Name == "" {
		c.Name = c.Title
	}
	return c
}
