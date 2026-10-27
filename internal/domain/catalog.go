package domain

import (
	"fmt"
	"regexp"
	"strings"
)

// Source says where a file lives. An empty Repo means the repository of the
// catalog that holds the reference, so one repository can carry a whole
// subtree and a new repository is only needed to hand a layer to someone else.
type Source struct {
	Repo string
	Ref  string
	Path string
}

// CatalogRef points at another Catalog; MultiverseRef at a file that defines a
// Multiverse. Names are unique among siblings of either kind.
type CatalogRef struct {
	Name   string
	Source Source
}

type MultiverseRef struct {
	Name   string
	Source Source
}

// Catalog is one index file: pointers only, never definitions.
type Catalog struct {
	Name        string
	Catalogs    []CatalogRef
	Multiverses []MultiverseRef
}

type NodeKind string

const (
	CatalogNode    NodeKind = "catalog"
	MultiverseNode NodeKind = "multiverse"
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
	// File and Dir are where a loaded catalog node lives on this machine: its
	// file, and the root of the checkout or directory its paths are relative
	// to. They are what a write goes to; multiverses and unavailable nodes have
	// neither.
	File, Dir string

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

// Leaves are the multiverses under n, in tree order; n itself if it is one.
func (n Node) Leaves() []Node {
	if n.Kind == MultiverseNode {
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

var catalogNameRE = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// ValidateCatalogName checks a name that will become a path segment, a file
// name and a metadata.name.
func ValidateCatalogName(name string) error {
	if !catalogNameRE.MatchString(name) {
		return fmt.Errorf("name %q must start with a letter and use lowercase letters, digits and dashes", name)
	}
	return nil
}

// TreeRow is one line of a tree drawing: a node and the glyphs that put it in
// place, so every view of the tree (the command, the pickers) draws the same.
type TreeRow struct {
	Node   Node
	Prefix string
}

// Rows lists the root and everything under it, depth first.
func (n Node) Rows() []TreeRow {
	rows := []TreeRow{{Node: n}}
	var walk func(parent Node, indent string)
	walk = func(parent Node, indent string) {
		for i, c := range parent.Children {
			last := i == len(parent.Children)-1
			connector, next := "├─ ", indent+"│  "
			if last {
				connector, next = "└─ ", indent+"   "
			}
			rows = append(rows, TreeRow{Node: c, Prefix: indent + connector})
			walk(c, next)
		}
	}
	walk(n, "")
	return rows
}
