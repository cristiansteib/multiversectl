package app

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	multiverseNameRE = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	nonNameRE        = regexp.MustCompile(`[^a-z0-9]+`)
)

// Resolve finds a multiverse by the name it was recorded under or by the
// catalog path it was installed from, and returns its name either way.
func (c Config) Resolve(key string) (string, MultiverseEntry, error) {
	if e, ok := c.Multiverses[key]; ok {
		return key, e, nil
	}
	for name, e := range c.Multiverses {
		if e.From != "" && e.From == key {
			return name, e, nil
		}
	}
	return "", MultiverseEntry{}, fmt.Errorf("no multiverse %q; installed: %s (`multiversectl start %s` adds it)", key, c.names(), key)
}

// Select is the multiverse a command acts on, from what it was told and where
// it was run: the one named, else the one whose folder holds cwd, else the only
// one there is. The boolean is false when nothing is recorded yet, which is the
// normal state before the first start; several with nothing to choose between
// them is an error that names them, since guessing is how the wrong one is
// stopped.
func (c Config) Select(name, cwd string) (MultiverseEntry, bool, error) {
	if name != "" {
		_, e, err := c.Resolve(name)
		return e, err == nil, err
	}
	if len(c.Multiverses) == 0 {
		return MultiverseEntry{}, false, nil
	}
	best, found := "", false
	for n, e := range c.Multiverses {
		if cwd != "" && within(cwd, e.Root) && (!found || len(e.Root) > len(c.Multiverses[best].Root)) {
			best, found = n, true
		}
	}
	if found {
		return c.Multiverses[best], true, nil
	}
	if len(c.Multiverses) == 1 {
		for _, e := range c.Multiverses {
			return e, true, nil
		}
	}
	return MultiverseEntry{}, false, fmt.Errorf("several multiverses are started (%s), and this folder is in none of them: name one with -m", c.names())
}

// within says dir is root or somewhere under it.
func within(dir, root string) bool {
	if root == "" {
		return false
	}
	dir, root = filepath.Clean(dir), filepath.Clean(root)
	return dir == root || strings.HasPrefix(dir, root+string(filepath.Separator))
}

func (c Config) names() string {
	if len(c.Multiverses) == 0 {
		return "none"
	}
	names := make([]string, 0, len(c.Multiverses))
	for n := range c.Multiverses {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// MultiverseNameFor derives a name from a root directory when the user gave
// none, squashing whatever the directory is called into a valid name.
func MultiverseNameFor(root string) string {
	n := strings.Trim(nonNameRE.ReplaceAllString(strings.ToLower(filepath.Base(root)), "-"), "-")
	if n == "" || n[0] < 'a' || n[0] > 'z' {
		n = "ws-" + n
	}
	return strings.TrimSuffix(n, "-")
}

// MultiverseInfo is one row of the multiverses listing.
type MultiverseInfo struct {
	Name string
	MultiverseEntry
}

// Multiverses lists what is installed on this machine, by name.
func (s *Service) Multiverses() ([]MultiverseInfo, error) {
	cfg, err := s.Config.Load()
	if err != nil {
		return nil, err
	}
	out := make([]MultiverseInfo, 0, len(cfg.Multiverses))
	for name, e := range cfg.Multiverses {
		out = append(out, MultiverseInfo{Name: name, MultiverseEntry: e})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Forget removes a multiverse from this machine's list. Its files stay where
// they are; only the tool stops knowing about it.
func (s *Service) Forget(key string) error {
	cfg, err := s.Config.Load()
	if err != nil {
		return err
	}
	if key == "" {
		return fmt.Errorf("forget: a multiverse name is required")
	}
	name, entry, err := cfg.Resolve(key)
	if err != nil {
		return err
	}
	delete(cfg.Multiverses, name)
	if err := s.Config.Save(cfg); err != nil {
		return fmt.Errorf("saving the configuration: %w", err)
	}
	s.Out.OK("forgot %s; its files are still at %s", name, entry.Root)
	return nil
}

// MultiverseNamed finds an installed multiverse by name or by the catalog path
// it came from.
func (s *Service) MultiverseNamed(key string) (MultiverseInfo, error) {
	cfg, err := s.Config.Load()
	if err != nil {
		return MultiverseInfo{}, err
	}
	name, e, err := cfg.Resolve(key)
	if err != nil {
		return MultiverseInfo{}, err
	}
	return MultiverseInfo{Name: name, MultiverseEntry: e}, nil
}
