package app

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// Item is one thing multiversectl put on this machine, and the command that
// takes it away. The inventory only lists: removing is left to the person, who
// can read what each costs first.
type Item struct {
	Kind string // what sort of thing: server, proxy, universe, clone, branch, copy, state, completion, program
	What string
	Path string // where it is: a path, or a name in the container runtime
	// Size is the bytes it takes, -1 when that does not apply or is not known.
	Size int64
	Note string
	// Risk says removing it loses something that exists nowhere else: work not
	// committed, commits on no remote, data in a volume.
	Risk   bool
	Remove string
}

// ProjectFootprint lists what one installed project has made: its universes with
// their containers and volumes, the clones of its repositories, the copy of its
// manifest, and branches left behind by universes that are gone. name is how the
// project is called on this machine, which is what the removal commands name it by.
func (s *Service) ProjectFootprint(ctx context.Context, name string) ([]Item, error) {
	var items []Item

	states, err := s.Store.List()
	if err != nil {
		return nil, fmt.Errorf("listing universes: %w", err)
	}
	inUse := map[string]map[string]bool{} // repo key -> branches a universe is on
	for _, st := range states {
		u := s.Universe(st.Name)
		it := Item{Kind: "universe", What: fmt.Sprintf("universe %s of %s", st.Name, name), Path: u.Dir(), Size: -1,
			Remove: fmt.Sprintf("multiversectl -p %s universe rm %s", name, st.Name)}
		var notes []string
		if cs, err := s.Runtime.Containers(ctx, st.Project); err == nil && len(cs) > 0 {
			notes = append(notes, fmt.Sprintf("%s (%d up)", plural(len(cs), "container"), countRunning(cs)))
		}
		if vs, err := s.Runtime.Volumes(ctx, st.Project+"-"); err == nil && len(vs) > 0 {
			notes = append(notes, plural(len(vs), "volume")+": their data goes with it")
		}
		var dirty []string
		for key := range st.Branches {
			if dir := u.RepoDir(key); s.Files.Exists(dir) {
				if clean, err := s.Git.IsClean(ctx, dir); err != nil || !clean {
					dirty = append(dirty, key)
				}
			}
			if inUse[key] == nil {
				inUse[key] = map[string]bool{}
			}
			inUse[key][st.Branches[key]] = true
		}
		sort.Strings(dirty)
		if len(dirty) > 0 {
			notes = append(notes, "uncommitted work in "+strings.Join(dirty, ", "))
			it.Risk = true
		}
		it.Note = strings.Join(notes, "; ")
		items = append(items, it)
	}

	leftover := regexp.MustCompile("^" + strings.ReplaceAll(regexp.QuoteMeta(firstNonEmpty(s.Manifest.Branch, "u{{universe}}")), `\{\{universe\}\}`, "[0-9]+") + "$")
	for _, key := range s.repoKeys() {
		clone := s.Manifest.CloneDir(s.Workspace, key)
		if !s.Files.Exists(clone) {
			continue
		}
		it := Item{Kind: "clone", What: fmt.Sprintf("clone of %s (%s)", key, shortRepo(s.Manifest.Repos[key].URL)), Path: clone, Size: -1, Remove: "rm -rf " + quote(clone)}
		var notes []string
		if clean, err := s.Git.IsClean(ctx, clone); err == nil && !clean {
			notes = append(notes, "uncommitted changes")
			it.Risk = true
		}
		if n, err := s.Git.Unpushed(ctx, clone); err == nil && n > 0 {
			notes = append(notes, fmt.Sprintf("%s on no remote", plural(n, "commit")))
			it.Risk = true
		}
		it.Note = strings.Join(notes, "; ")
		items = append(items, it)

		if names, err := s.Git.Branches(ctx, clone); err == nil {
			for _, b := range names {
				if !leftover.MatchString(b) || inUse[key][b] {
					continue
				}
				if kind, _ := s.Git.RefExists(ctx, clone, b); kind != RefLocal {
					continue
				}
				items = append(items, Item{Kind: "branch", What: fmt.Sprintf("branch %s in %s, left by a universe that is gone", b, key),
					Path: clone, Size: -1, Note: "check it holds nothing you need", Remove: fmt.Sprintf("git -C %s branch -D %s", quote(clone), b)})
			}
		}
	}
	if dir := manifestClone(s.Workspace); s.Files.Exists(dir) {
		items = append(items, Item{Kind: "copy", What: "copy of " + name + "'s manifest", Path: dir, Size: -1, Remove: "rm -rf " + quote(filepath.Dir(dir))})
	}
	s.measure(items)
	return items, nil
}

// MachineFootprint lists what is kept for the machine as a whole: the
// configuration, the cache of catalogs, a root catalog that lives only here, the
// shared proxy's file, and the web server's state.
func (c *Catalogs) MachineFootprint() []Item {
	dir := filepath.Dir(c.Config.Path())
	var items []Item
	add := func(name, what, note string, risk bool) {
		p := filepath.Join(dir, name)
		if c.Files.Exists(p) {
			items = append(items, Item{Kind: "state", What: what, Path: p, Size: -1, Note: note, Risk: risk, Remove: "rm -rf " + quote(p)})
		}
	}
	add("config.yaml", "the configuration: your root catalog and which projects are installed", "", false)
	add("catalogs", "the cache of catalog repositories", "fetched again when needed", false)
	add("index", "a root catalog kept only on this machine", "if it has no remote it exists nowhere else", true)
	add("proxy", "the shared proxy's compose file", "", false)
	add("server.json", "the web server's state", "", false)
	add("server.log", "the web server's log", "", false)
	c.measure(items)
	return items
}

// Footprint lists the completion scripts this tool installed.
func (c *Completions) Footprint() []Item {
	var items []Item
	for _, s := range Shells {
		t, err := c.TargetFor(s)
		if err != nil || c.InstalledVersion(t) == "" {
			continue
		}
		note := ""
		if t.RC != "" {
			note = "and a block in " + t.RC
		}
		items = append(items, Item{Kind: "completion", What: string(s) + " completions", Path: t.File, Size: -1, Note: note,
			Remove: "multiversectl completion --uninstall --shell " + string(s)})
	}
	return items
}

// measure fills in the sizes, several at a time: a clone with its dependencies
// installed is a long walk.
func (s *Service) measure(items []Item)  { measureAll(s.Files, items) }
func (c *Catalogs) measure(items []Item) { measureAll(c.Files, items) }

func measureAll(files Files, items []Item) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i := range items {
		if items[i].Path == "" || !filepath.IsAbs(items[i].Path) {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(it *Item) {
			defer wg.Done()
			defer func() { <-sem }()
			if n, err := files.Size(it.Path); err == nil {
				it.Size = n
			}
		}(&items[i])
	}
	wg.Wait()
}

func quote(p string) string { return "'" + strings.ReplaceAll(p, "'", `'\''`) + "'" }

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	// A bare "s" turns repository into repositorys, which is what the clone
	// progress line used to say.
	if len(noun) > 1 && strings.HasSuffix(noun, "y") && !strings.ContainsRune("aeiou", rune(noun[len(noun)-2])) {
		return fmt.Sprintf("%d %sies", n, noun[:len(noun)-1])
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

var _ = domain.ErrNotFound
