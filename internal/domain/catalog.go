package domain

import "strings"

// Source says where a file lives. An empty Repo means the repository of the
// catalog that holds the reference, so one repository can carry a whole
// subtree and a new repository is only needed to hand a layer to someone else.
type Source struct {
	Repo string
	Ref  string
	Path string
}

// CatalogRef points at another Catalog; WorkspaceRef at a repository's
// multiverse.yaml. Names are unique among siblings of either kind.
type CatalogRef struct {
	Name   string
	Source Source
}

type WorkspaceRef struct {
	Name   string
	Source Source
}

// Catalog is one index file: pointers only, never definitions.
type Catalog struct {
	Name       string
	Catalogs   []CatalogRef
	Workspaces []WorkspaceRef
}

type NodeKind string

const (
	CatalogNode   NodeKind = "catalog"
	WorkspaceNode NodeKind = "workspace"
)

// Node is a catalog resolved into a tree. Path is the slash-joined names from
// the root, which is how users address a node. Source is absolute: Repo is
// never inherited-empty, except for a catalog read from a local directory,
// where Path is the absolute file.
type Node struct {
	Name     string
	Path     string
	Kind     NodeKind
	Source   Source
	Children []Node

	// Unavailable says why a subtree could not be read (no access, a bad file),
	// so one unreachable layer does not hide the rest of the tree.
	Unavailable string
}

// Find returns the node at its full path; the empty path is the root.
func (n Node) Find(path string) (Node, bool) {
	path = strings.Trim(path, "/")
	if path == n.Path {
		return n, true
	}
	for _, c := range n.Children {
		if path == c.Path || strings.HasPrefix(path, c.Path+"/") {
			return c.Find(path)
		}
	}
	return Node{}, false
}

// Leaves are the workspaces under n, in tree order; n itself if it is one.
func (n Node) Leaves() []Node {
	if n.Kind == WorkspaceNode {
		return []Node{n}
	}
	var out []Node
	for _, c := range n.Children {
		out = append(out, c.Leaves()...)
	}
	return out
}

// ChildPath is the address of a child of the node at parent.
func ChildPath(parent, name string) string { return join(parent, name) }

func join(parent, name string) string {
	if parent == "" {
		return name
	}
	return parent + "/" + name
}
