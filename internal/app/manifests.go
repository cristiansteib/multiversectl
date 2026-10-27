package app

import (
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// manifestsIn lists the multiverse definitions a directory holds, by
// convention: multiverse.yaml at its root, and every .yaml or .yml under
// multiverse/. A repository that serves several multiverses keeps one file
// each, so nothing has to list them for them to be found.
func manifestsIn(files Files, dir string) []string {
	var out []string
	for _, name := range []string{"multiverse.yaml", "multiverse.yml"} {
		if files.Exists(filepath.Join(dir, name)) {
			out = append(out, name)
		}
	}
	names, err := files.ListFiles(filepath.Join(dir, "multiverse"))
	if err != nil {
		return out
	}
	for _, n := range names {
		if ext := filepath.Ext(n); ext == ".yaml" || ext == ".yml" {
			out = append(out, path.Join("multiverse", n))
		}
	}
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
