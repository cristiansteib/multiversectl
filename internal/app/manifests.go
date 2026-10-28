package app

import (
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// A file is a multiverse when it says so: `kind: Multiverse` at the start of a
// line. It is looked for before the file is parsed, so the many YAML files of a
// repository (compose files, workflows, charts) cost a read and nothing more.
var kindMultiverseRE = regexp.MustCompile(`(?m)^kind:[ \t]*["']?Multiverse["']?[ \t]*(?:#.*)?$`)

// What a scan of a repository does not go into, and how far it goes: what is
// installed or built there is not a definition, and a repository of a monorepo's
// size must not make installing it slow.
var skipScan = map[string]bool{"node_modules": true, "vendor": true, "dist": true, "build": true, "target": true, "__pycache__": true, "venv": true, "site-packages": true}

const (
	maxScanDepth     = 6
	maxScanFiles     = 4000
	maxManifestBytes = 256 << 10
)

func isYAML(name string) bool { ext := filepath.Ext(name); return ext == ".yaml" || ext == ".yml" }

// manifestsIn lists the multiverse definitions a directory holds. A repository
// may keep any number of them, in files of any name, anywhere in it: a file is
// one when it says `kind: Multiverse`. multiverse.yaml at the root and the files
// of a multiverse/ folder come first, and are taken as definitions whatever
// they say, so that one that is wrong is reported and not skipped. Folders that
// start with a dot, and the ones dependencies and builds fill, are not entered.
func manifestsIn(files Files, dir string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(rel string) {
		if !seen[rel] {
			seen[rel] = true
			out = append(out, rel)
		}
	}
	for _, name := range []string{"multiverse.yaml", "multiverse.yml"} {
		if files.Exists(filepath.Join(dir, name)) {
			add(name)
		}
	}
	if names, err := files.ListFiles(filepath.Join(dir, "multiverse")); err == nil {
		for _, n := range names {
			if isYAML(n) {
				add(path.Join("multiverse", n))
			}
		}
	}

	budget := maxScanFiles
	var walk func(rel string, depth int)
	walk = func(rel string, depth int) {
		here := filepath.Join(dir, filepath.FromSlash(rel))
		if names, err := files.ListFiles(here); err == nil {
			for _, n := range names {
				if !isYAML(n) || seen[path.Join(rel, n)] {
					continue
				}
				if budget--; budget < 0 {
					return
				}
				full := filepath.Join(here, n)
				if size, err := files.Size(full); err != nil || size > maxManifestBytes {
					continue
				}
				if data, err := files.ReadFile(full); err == nil && kindMultiverseRE.Match(data) {
					add(path.Join(rel, n))
				}
			}
		}
		if depth >= maxScanDepth {
			return
		}
		if dirs, err := files.ListDirs(here); err == nil {
			for _, d := range dirs {
				if strings.HasPrefix(d, ".") || skipScan[d] {
					continue
				}
				walk(path.Join(rel, d), depth+1)
			}
		}
	}
	walk("", 0)
	return out
}

// definedIn reads the name of each definition a directory holds. repo is where
// the directory came from, empty for a folder on this machine, in which case
// the file itself is the source.
func (c *Catalogs) definedIn(dir, repo string) ([]domain.Node, error) {
	var out []domain.Node
	for _, rel := range manifestsIn(c.Files, dir) {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		data, err := c.Files.ReadFile(full)
		if err != nil {
			return nil, err
		}
		name, err := c.Editor.MultiverseName(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", rel, err)
		}
		src := domain.Source{Repo: repo, Path: rel}
		if repo == "" {
			src.Path = full
		}
		for _, other := range out {
			if other.Name == name {
				return nil, fmt.Errorf("%s and %s both define a multiverse named %q: names are what picks one, so each needs its own", other.Source.Path, rel, name)
			}
		}
		out = append(out, domain.Node{Name: name, Path: name, Kind: domain.MultiverseNode, Source: src})
	}
	return out, nil
}

var addressedRE = regexp.MustCompile(`^(.+):([A-Za-z0-9][A-Za-z0-9_-]*)$`)

// splitAddress separates owner/repo:name, which picks one multiverse of a
// repository that has several. A URL, git@host:owner/repo and a Windows-style
// path have a colon too, but what follows it is never a bare name.
func splitAddress(arg string) (source, name string) {
	m := addressedRE.FindStringSubmatch(arg)
	if m == nil || strings.HasSuffix(m[1], "/") || strings.HasSuffix(m[1], ":") {
		return arg, ""
	}
	return m[1], m[2]
}

// only keeps the multiverse called name, and says what there is when none is.
func only(leaves []domain.Node, name, source string) ([]domain.Node, error) {
	for _, l := range leaves {
		if l.Name == name {
			return []domain.Node{l}, nil
		}
	}
	return nil, fmt.Errorf("%s has no multiverse named %q (it has: %s)", source, name, leafNames(leaves))
}

// hasCompose reports a compose file at the root of dir.
func hasCompose(files Files, dir string) bool {
	for _, n := range composeFiles {
		if files.Exists(filepath.Join(dir, n)) {
			return true
		}
	}
	return false
}

// impliedFrom is the multiverse a repository is when it defines none but has
// compose files: itself, run on its own. Its name is the repository's.
func impliedFrom(url string) domain.Node {
	name := nameParts(url).repo
	return domain.Node{Name: name, Path: name, Kind: domain.MultiverseNode, Implied: true, Source: domain.Source{Repo: url}}
}

// parts are the pieces of a repository's address a name is made from.
type parts struct {
	repo string   // the repository's own name
	path []string // what leads to it, nearest first: owner, then the group above it
	host string
}

func nameParts(url string) parts {
	l := domain.Locate(url, "")
	segs := strings.FieldsFunc(strings.TrimSuffix(l.Path, ".git"), func(r rune) bool { return r == '/' || r == '\\' })
	var p parts
	p.host = nameSegment(l.Host)
	for i := len(segs) - 1; i >= 0; i-- {
		seg := nameSegment(segs[i])
		switch {
		case seg == "":
		case p.repo == "":
			p.repo = seg
		default:
			p.path = append(p.path, seg)
		}
	}
	if p.repo == "" {
		p.repo = "app"
	}
	return p
}

var notNameRE = regexp.MustCompile(`[^a-z0-9]+`)

// nameSegment turns a piece of an address into something a name may hold.
func nameSegment(s string) string {
	return strings.Trim(notNameRE.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

// nameCandidates are the names a repository can take, shortest first: its own,
// then with its owner in front, then each level above, then the host. A name is
// only worth a level more when the shorter one is already someone else's.
func nameCandidates(url string) []string {
	p := nameParts(url)
	var out []string
	name := p.repo
	out = append(out, name)
	for _, seg := range p.path {
		name = seg + "-" + name
		out = append(out, name)
	}
	if p.host != "" {
		out = append(out, p.host+"-"+name)
	}
	var valid []string
	for _, n := range out {
		if multiverseNameRE.MatchString(n) {
			valid = append(valid, n)
		}
	}
	return valid
}
