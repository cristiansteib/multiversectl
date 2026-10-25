package app

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	workspaceNameRE = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	nonNameRE       = regexp.MustCompile(`[^a-z0-9]+`)
)

// Normalize folds a config written before multiple workspaces existed into
// the current shape, so callers only ever see Workspaces.
func (c *Config) Normalize() {
	if c.Root != "" && c.Manifest != "" && len(c.Workspaces) == 0 {
		name := WorkspaceNameFor(c.Root)
		c.Workspaces = map[string]WorkspaceEntry{name: {Root: c.Root, Manifest: c.Manifest}}
		if c.Current == "" {
			c.Current = name
		}
	}
	c.Root, c.Manifest = "", ""
}

// Resolve finds a workspace by the name it was recorded under or by the
// catalog path it was installed from, and returns its name either way.
func (c Config) Resolve(key string) (string, WorkspaceEntry, error) {
	if e, ok := c.Workspaces[key]; ok {
		return key, e, nil
	}
	for name, e := range c.Workspaces {
		if e.From != "" && e.From == key {
			return name, e, nil
		}
	}
	return "", WorkspaceEntry{}, fmt.Errorf("no workspace %q; installed: %s (`multiversectl catalog install %s` adds it)", key, c.names(), key)
}

// Select returns the workspace called name, or the current one when name is
// empty. The boolean is false when nothing is recorded yet, which is the
// normal state before the first install.
func (c Config) Select(name string) (WorkspaceEntry, bool, error) {
	if name == "" {
		name = c.Current
	}
	if name == "" {
		return WorkspaceEntry{}, false, nil
	}
	_, e, err := c.Resolve(name)
	return e, err == nil, err
}

func (c Config) names() string {
	if len(c.Workspaces) == 0 {
		return "none"
	}
	names := make([]string, 0, len(c.Workspaces))
	for n := range c.Workspaces {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// WorkspaceNameFor derives a name from a root directory when the user gave
// none, squashing whatever the directory is called into a valid name.
func WorkspaceNameFor(root string) string {
	n := strings.Trim(nonNameRE.ReplaceAllString(strings.ToLower(filepath.Base(root)), "-"), "-")
	if n == "" || n[0] < 'a' || n[0] > 'z' {
		n = "ws-" + n
	}
	return strings.TrimSuffix(n, "-")
}

// WorkspaceInfo is one row of the workspaces listing.
type WorkspaceInfo struct {
	Name    string
	Current bool
	WorkspaceEntry
}

// Workspaces lists what is installed on this machine, by name.
func (s *Service) Workspaces() ([]WorkspaceInfo, error) {
	cfg, err := s.Config.Load()
	if err != nil {
		return nil, err
	}
	out := make([]WorkspaceInfo, 0, len(cfg.Workspaces))
	for name, e := range cfg.Workspaces {
		out = append(out, WorkspaceInfo{Name: name, Current: name == cfg.Current, WorkspaceEntry: e})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Use makes a workspace the one commands act on by default.
func (s *Service) Use(key string) error {
	cfg, err := s.Config.Load()
	if err != nil {
		return err
	}
	if key == "" {
		return fmt.Errorf("use: a workspace name is required")
	}
	name, _, err := cfg.Resolve(key)
	if err != nil {
		return err
	}
	cfg.Current = name
	if err := s.Config.Save(cfg); err != nil {
		return fmt.Errorf("saving the configuration: %w", err)
	}
	s.Out.OK("now using %s", name)
	return nil
}

// Forget removes a workspace from this machine's list. Its files stay where
// they are; only the tool stops knowing about it.
func (s *Service) Forget(key string) error {
	cfg, err := s.Config.Load()
	if err != nil {
		return err
	}
	if key == "" {
		return fmt.Errorf("uninstall: a workspace name is required")
	}
	name, entry, err := cfg.Resolve(key)
	if err != nil {
		return err
	}
	delete(cfg.Workspaces, name)
	wasCurrent := cfg.Current == name
	if wasCurrent {
		cfg.Current = ""
	}
	if err := s.Config.Save(cfg); err != nil {
		return fmt.Errorf("saving the configuration: %w", err)
	}
	s.Out.OK("forgot %s; its files are still at %s", name, entry.Root)
	if wasCurrent && len(cfg.Workspaces) > 0 {
		s.Out.Info("no current workspace: `multiversectl catalog use <name>`")
	}
	return nil
}

// WorkspaceNamed finds an installed workspace by name or by the catalog path
// it came from.
func (s *Service) WorkspaceNamed(key string) (WorkspaceInfo, error) {
	cfg, err := s.Config.Load()
	if err != nil {
		return WorkspaceInfo{}, err
	}
	name, e, err := cfg.Resolve(key)
	if err != nil {
		return WorkspaceInfo{}, err
	}
	return WorkspaceInfo{Name: name, Current: name == cfg.Current, WorkspaceEntry: e}, nil
}
