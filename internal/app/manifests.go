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
