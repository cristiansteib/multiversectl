// Package v1alpha1 is the catalog's file format, shaped like the project
// manifest's so both read as the same family of objects.
//
// Markers follow the manifest types: +pattern=, +required, +default=.
package v1alpha1

const (
	APIVersion = "multiverse/v1alpha1"
	Kind       = "Catalog"
	// KindRoot is the catalog a machine starts from. It is a Catalog in every
	// other respect; the kind is what lets a new machine tell the index a person
	// begins with from the catalogs of the projects hanging under it.
	KindRoot = "CatalogRoot"
)

// Catalog is an index of projects and of other catalogs. It holds pointers
// only: each project's repository owns its multiverse.yaml, and each nested
// catalog is owned by whoever owns the repository it lives in.
type Catalog struct {
	TypeMeta `yaml:",inline"`

	// +required
	ObjectMeta ObjectMeta `yaml:"metadata"`

	Spec Spec `yaml:"spec"`
}

type TypeMeta struct {
	// The only version this build understands.
	// +enum=multiverse/v1alpha1
	// +required
	APIVersion string `yaml:"apiVersion"`

	// +enum=Catalog
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

	// ProjectRefs are the leaves: a repository with a multiverse.yaml.
	Projects []ProjectRef `yaml:"projects"`

	// Projects is the old name of Projects, still read so a catalog written
	// before they were called projects keeps working, and never written to a
	// file that does not already use it.
	Workspaces []ProjectRef `yaml:"workspaces"`
}

type CatalogRef struct {
	// Unique among the siblings of both lists.
	// +pattern=^[a-z][a-z0-9-]*$
	// +required
	Name string `yaml:"name"`

	// +required
	Source Source `yaml:"source"`
}

type ProjectRef struct {
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

// CatalogRoot is a Catalog that is the starting point of a machine: the one
// `multiversectl init` is pointed at. Its spec is a Catalog's.
type CatalogRoot struct {
	TypeMeta `yaml:",inline"`

	// +required
	ObjectMeta ObjectMeta `yaml:"metadata"`

	// +required
	Spec Spec `yaml:"spec"`
}
