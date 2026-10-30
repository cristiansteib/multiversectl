// Package schema carries the generated field reference for the two kinds of
// file, so the binary can print what a manifest takes without the repository,
// the website or a network. `multiversectl schema` is what reads it.
package schema

import _ "embed"

// Multiverse is schema/multiverse.v1alpha1.yaml, the Multiverse fields.
//
//go:embed multiverse.v1alpha1.yaml
var Multiverse string

// Catalog is schema/catalog.v1alpha1.yaml, the Catalog fields.
//
//go:embed catalog.v1alpha1.yaml
var Catalog string
