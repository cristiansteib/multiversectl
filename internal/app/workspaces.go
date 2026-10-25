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

// Select returns the workspace called name, or the current one when name is
// empty. The boolean is false when nothing is recorded yet, which is the
// normal state before the first bootstrap.
func (c Config) Select(name string) (WorkspaceEntry, bool, error) {
	if name == "" {
		name = c.Current
	}
	if name == "" {
		return WorkspaceEntry{}, false, nil
	}
	e, ok := c.Workspaces[name]
	if !ok {
		return WorkspaceEntry{}, false, fmt.Errorf("no workspace %q (known: %s)", name, c.names())
	}
	return e, true, nil
}

func (c Config) names() string {
	if len(c.Workspaces) == 0 {
		return "none, run bootstrap"
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

// Workspaces lists what this machine has bootstrapped, by name.
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

// Use makes name the workspace commands act on by default.
func (s *Service) Use(name string) error {
	cfg, err := s.Config.Load()
	if err != nil {
		return err
	}
	if name == "" {
		return fmt.Errorf("use: a workspace name is required")
	}
	if _, _, err := cfg.Select(name); err != nil {
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
func (s *Service) Forget(name string) error {
	cfg, err := s.Config.Load()
	if err != nil {
		return err
	}
	if _, _, err := cfg.Select(name); err != nil || name == "" {
		return firstErr(err, fmt.Errorf("uninstall: a workspace name is required"))
	}
	delete(cfg.Workspaces, name)
	if cfg.Current == name {
		cfg.Current = ""
	}
	if err := s.Config.Save(cfg); err != nil {
		return fmt.Errorf("saving the configuration: %w", err)
	}
	s.Out.OK("forgot %s; its files are still on disk", name)
	return nil
}
