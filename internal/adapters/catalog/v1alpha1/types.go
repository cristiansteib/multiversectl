// Package v1alpha1 is the catalog's file format, shaped like the workspace
// manifest's so both read as the same family of objects.
//
// Markers follow the manifest types: +pattern=, +required, +default=.
package v1alpha1

const (
	APIVersion = "multiverse/v1alpha1"
	Kind       = "Catalog"
)

// Catalog is an index of workspaces and of other catalogs. It holds pointers
// only: each workspace's repository owns its multiverse.yaml, and each nested
// catalog is owned by whoever owns the repository it lives in.
type Catalog struct {
	TypeMeta `yaml:",inline"`

	// +required
	ObjectMeta ObjectMeta `yaml:"metadata"`

	Spec Spec `yaml:"spec"`
}

type TypeMeta struct {
	// +required
	APIVersion string `yaml:"apiVersion"`

	// +required
	Kind string `yaml:"kind"`
}

type ObjectMeta struct {
	// +pattern=^[a-z][a-z0-9-]*$
	// +required
	Name string `yaml:"name"`
}

type Spec struct {
	// CatalogRefs descend into another Catalog; a node's address is the names
	// of the refs leading to it, e.g. acme/backend.
	Catalogs []CatalogRef `yaml:"catalogs"`

	// WorkspaceRefs are the leaves: a repository with a multiverse.yaml.
	Workspaces []WorkspaceRef `yaml:"workspaces"`
}

type CatalogRef struct {
	// Unique among the siblings of both lists.
	// +pattern=^[a-z][a-z0-9-]*$
	// +required
	Name string `yaml:"name"`

	// +required
	Source Source `yaml:"source"`
}

type WorkspaceRef struct {
	// +pattern=^[a-z][a-z0-9-]*$
	// +required
	Name string `yaml:"name"`

	// +required
	Source Source `yaml:"source"`
}

// Source locates a file. Leave repo out to mean the repository the reference
// is written in.
type Source struct {
	// A git URL.
	Repo string `yaml:"repo"`

	// Branch, tag or commit. Defaults to the repository's default branch.
	Ref string `yaml:"ref"`

	// Relative to the repository root. Defaults to catalog.yaml for a
	// CatalogRef and multiverse.yaml for a WorkspaceRef.
	Path string `yaml:"path"`
}
