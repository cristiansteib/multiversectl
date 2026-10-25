package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// maxCatalogDepth bounds how far references are followed, as a second guard
// next to cycle detection against a very long chain of repositories.
const maxCatalogDepth = 8

const defaultCatalogFile = "catalog.yaml"

// CatalogDeps are the ports the catalog service needs. It is deliberately not
// part of Service: browsing what exists never touches a workspace or a
// universe, and the two should not be able to reach into each other.
type CatalogDeps struct {
	Git      Git
	Files    Files
	Decoder  CatalogDecoder
	Config   ConfigStore
	Out      Output
	CacheDir string // where remote repositories are cloned
}

// Catalogs resolves and browses the tree of catalogs.
type Catalogs struct{ CatalogDeps }

func NewCatalogs(d CatalogDeps) *Catalogs { return &Catalogs{d} }

// Use records the root catalog for this machine.
func (c *Catalogs) Use(source string) error {
	if source == "" {
		return errors.New("catalog use: a git URL or a file path is required")
	}
	cfg, err := c.Config.Load()
	if err != nil {
		return err
	}
	cfg.Catalog = source
	if err := c.Config.Save(cfg); err != nil {
		return fmt.Errorf("saving the configuration: %w", err)
	}
	c.Out.OK("root catalog is %s", source)
	return nil
}

// Tree resolves the catalog at source, or the saved root when source is empty.
// With refresh, every remote repository is fetched first. A layer that cannot
// be read is returned as an unavailable node; only the root failing is an error.
func (c *Catalogs) Tree(ctx context.Context, source string, refresh bool) (domain.Node, error) {
	if source == "" {
		cfg, err := c.Config.Load()
		if err != nil {
			return domain.Node{}, err
		}
		if source = cfg.Catalog; source == "" {
			return domain.Node{}, errors.New("no root catalog: pass one, or record it with `catalog use <git-url-or-file>`")
		}
	}
	r := &resolver{Catalogs: c, ctx: ctx, refresh: refresh, fetched: map[string]bool{}}
	root := location{}
	if isGitURL(source) {
		root.repo = source
		dir, err := r.checkout(source, "")
		if err != nil {
			return domain.Node{}, err
		}
		root.dir, root.file = dir, defaultCatalogFile
	} else {
		abs, err := filepath.Abs(source)
		if err != nil {
			return domain.Node{}, err
		}
		root.dir, root.file = filepath.Dir(abs), filepath.Base(abs)
	}
	node, err := r.load(root, "", "", nil)
	if err != nil {
		return domain.Node{}, err
	}
	return node, nil
}

// location is one catalog file: the checkout it sits in and which repository
// that is (empty for a local file), so same-repo references can reuse it.
type location struct {
	repo, ref string
	dir, file string
}

func (l location) key() string { return l.repo + "@" + l.ref + ":" + filepath.Join(l.dir, l.file) }

type resolver struct {
	*Catalogs
	ctx     context.Context
	refresh bool
	fetched map[string]bool
}

func (r *resolver) load(at location, name, nodePath string, stack []string) (domain.Node, error) {
	k := at.key()
	for _, seen := range stack {
		if seen == k {
			return domain.Node{}, fmt.Errorf("catalog %s refers back to itself", at.file)
		}
	}
	if len(stack) >= maxCatalogDepth {
		return domain.Node{}, fmt.Errorf("catalogs nest deeper than %d levels", maxCatalogDepth)
	}
	data, err := r.Files.ReadFile(filepath.Join(at.dir, at.file))
	if err != nil {
		return domain.Node{}, fmt.Errorf("read catalog: %w", err)
	}
	cat, err := r.Decoder.Decode(data)
	if err != nil {
		return domain.Node{}, fmt.Errorf("%s: %w", at.file, err)
	}
	if name == "" {
		name = cat.Name
	}
	node := domain.Node{
		Name: name, Path: nodePath, Kind: domain.CatalogNode,
		Source: domain.Source{Repo: at.repo, Ref: at.ref, Path: filepath.Join(at.dir, at.file)},
	}
	stack = append(stack, k)

	for _, ref := range cat.Catalogs {
		child := domain.Node{Name: ref.Name, Path: domain.ChildPath(nodePath, ref.Name), Kind: domain.CatalogNode}
		loc, err := r.locate(at, ref.Source)
		if err == nil {
			var sub domain.Node
			if sub, err = r.load(loc, ref.Name, child.Path, stack); err == nil {
				child = sub
			}
		}
		if err != nil {
			child.Source = domain.Source{Repo: firstOf(ref.Source.Repo, at.repo), Ref: ref.Source.Ref, Path: ref.Source.Path}
			child.Unavailable = reason(err)
		}
		node.Children = append(node.Children, child)
	}
	for _, ref := range cat.Workspaces {
		node.Children = append(node.Children, domain.Node{
			Name: ref.Name, Path: domain.ChildPath(nodePath, ref.Name), Kind: domain.WorkspaceNode,
			Source: r.absolute(at, ref.Source),
		})
	}
	return node, nil
}

// locate finds the checkout a CatalogRef's file is in: this one when the ref
// stays in the repository, otherwise a cached clone at the wanted ref.
func (r *resolver) locate(from location, s domain.Source) (location, error) {
	rel := s.Path
	if rel == "" {
		rel = defaultCatalogFile
	}
	if s.Repo == "" || (s.Repo == from.repo && s.Ref == "") {
		return location{repo: from.repo, ref: from.ref, dir: from.dir, file: filepath.FromSlash(rel)}, nil
	}
	dir, err := r.checkout(s.Repo, s.Ref)
	if err != nil {
		return location{}, err
	}
	return location{repo: s.Repo, ref: s.Ref, dir: dir, file: rel}, nil
}

// absolute fills in what a workspace ref left implicit. In a local catalog
// there is no repository, so the path becomes the absolute manifest file.
func (r *resolver) absolute(from location, s domain.Source) domain.Source {
	if s.Repo == "" {
		s.Repo, s.Ref = from.repo, from.ref
	}
	if s.Repo == "" {
		s.Path = filepath.Join(from.dir, s.Path)
	}
	return s
}

var unsafeRE = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// checkout makes sure repo is cloned (and fetched, once per run, on refresh)
// at ref. Each repo and ref pair gets its own directory: two references to one
// repository at different refs must not fight over a single working tree.
func (r *resolver) checkout(repo, ref string) (string, error) {
	sum := sha256.Sum256([]byte(repo))
	dir := filepath.Join(r.CacheDir, hex.EncodeToString(sum[:])[:12], unsafeRE.ReplaceAllString(firstOf(ref, "HEAD"), "_"))
	switch {
	case !r.Files.Exists(dir):
		if err := r.Files.MkdirAll(filepath.Dir(dir)); err != nil {
			return "", err
		}
		if err := r.Git.Clone(r.ctx, repo, dir); err != nil {
			return "", fmt.Errorf("clone %s: %w", repo, err)
		}
	case r.refresh && !r.fetched[dir]:
		if err := r.Git.Fetch(r.ctx, dir); err != nil {
			return "", fmt.Errorf("fetch %s: %w", repo, err)
		}
	}
	r.fetched[dir] = true
	if ref != "" {
		if err := r.Git.Checkout(r.ctx, dir, ref); err != nil {
			return "", fmt.Errorf("%s at %s: %w", repo, ref, err)
		}
	} else if r.refresh {
		// A refresh must move to the new tip of the default branch, not stay
		// on the commit the first clone happened to land on.
		branch, err := r.Git.DefaultBranch(r.ctx, dir)
		if err != nil {
			return "", err
		}
		if err := r.Git.Checkout(r.ctx, dir, branch); err != nil {
			return "", err
		}
	}
	return dir, nil
}

func firstOf(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// reason keeps what a person needs from an error: git prints progress lines
// before the "fatal:" one that says what went wrong.
func reason(err error) string {
	lines := strings.Split(strings.TrimSpace(err.Error()), "\n")
	for i := range lines {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "fatal:") {
			return strings.TrimSpace(lines[i])
		}
	}
	return strings.TrimSpace(lines[0])
}
