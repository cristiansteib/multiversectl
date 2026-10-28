package app

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// Ignore leaves paths of the catalog tree out on this machine, or brings them
// back. An ignored catalog is not read, so one that is unreachable (behind a VPN,
// say) costs no wait; an ignored multiverse is not offered for installing. It is
// a setting of this machine: nothing in any catalog repository changes.
func (c *Catalogs) Ignore(ctx context.Context, paths []string, ignore bool) error {
	cfg, err := c.Config.Load()
	if err != nil {
		return err
	}
	// What is in the tree now, from what was last read, so a typo is said and not
	// silently saved. A tree that cannot be read does not stop a path being left out.
	var known func(string) bool
	if tree, err := c.tree(ctx, "", false, true); err == nil {
		known = func(p string) bool {
			if _, ok := tree.Find(p); ok {
				return true
			}
			// Under a catalog that could not be read here, what is in it is not known:
			// the path may well be right.
			for at := p; strings.Contains(at, "/"); {
				at = at[:strings.LastIndex(at, "/")]
				if n, ok := tree.Find(at); ok && (n.Unavailable != "" || n.Ignored) {
					return true
				}
			}
			return false
		}
	}
	for _, raw := range paths {
		p := strings.Trim(raw, "/")
		if p == "" {
			return fmt.Errorf("the root catalog cannot be ignored: it is the one this machine points at")
		}
		if !ignore {
			if !slices.Contains(cfg.Ignored, p) {
				return fmt.Errorf("%q is not ignored", p)
			}
			cfg.Ignored = slices.DeleteFunc(cfg.Ignored, func(x string) bool { return x == p })
			continue
		}
		if known != nil && !known(p) {
			return fmt.Errorf("there is no %q in the catalog; `multiversectl catalog list` shows the paths", p)
		}
		if slices.Contains(cfg.Ignored, p) {
			continue
		}
		// Whatever is under it is left out with it, so it needs no entry of its own.
		cfg.Ignored = slices.DeleteFunc(cfg.Ignored, func(x string) bool { return strings.HasPrefix(x, p+"/") })
		cfg.Ignored = append(cfg.Ignored, p)
	}
	slices.Sort(cfg.Ignored)
	return c.Config.Save(cfg)
}
