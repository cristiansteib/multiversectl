// Package schema carries the generated field reference for the two kinds of
// file, and reads it: what a manifest takes can then be answered without the
// repository, the website or a network. `make schema` writes the files from
// the Go types, and this package is where the shape they are written in — a
// CustomResourceDefinition — is known, so that a caller asks for a field by
// name and never for the envelope around it.
package schema

import (
	_ "embed"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed multiverse.v1alpha1.yaml
var multiverse string

//go:embed catalog.v1alpha1.yaml
var catalog string

// Kinds are the files there are, in the words a flag or a completion uses.
func Kinds() []string { return []string{"multiverse", "catalog"} }

// Root is the object a kind's fields hang off: the ceremony of the
// CustomResourceDefinition it is written as stops here.
func Root(kind string) (*yaml.Node, error) {
	var text string
	switch kind {
	case "multiverse":
		text = multiverse
	case "catalog":
		text = catalog
	default:
		return nil, fmt.Errorf("no schema for %q; there is %s", kind, strings.Join(Kinds(), " and "))
	}
	var doc struct {
		Spec struct {
			Versions []struct {
				Schema struct {
					Root yaml.Node `yaml:"openAPIV3Schema"`
				} `yaml:"schema"`
			} `yaml:"versions"`
		} `yaml:"spec"`
	}
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		return nil, fmt.Errorf("the built-in %s schema does not parse: %w", kind, err)
	}
	if len(doc.Spec.Versions) == 0 {
		return nil, fmt.Errorf("the built-in %s schema has no versions", kind)
	}
	return &doc.Spec.Versions[0].Schema.Root, nil
}

// At walks a dotted path. A list, or a map of objects, is stepped through as
// if it were a single one, so a path reads like the field it names
// (spec.apps.hosts.port) and not like the schema's own shape.
func At(node *yaml.Node, path string) (*yaml.Node, error) {
	for _, name := range strings.Split(path, ".") {
		if name == "" {
			continue
		}
		node = through(node)
		properties := field(node, "properties")
		if next := field(properties, name); next != nil {
			node = next
			continue
		}
		if names := keys(properties); len(names) > 0 {
			return nil, fmt.Errorf("no field %q there; it has: %s", name, strings.Join(names, ", "))
		}
		return nil, fmt.Errorf("no field %q there; nothing is nested under it", name)
	}
	return node, nil
}

// Fields are the names nested under a node, in the order they are written.
func Fields(node *yaml.Node) []string { return keys(field(through(node), "properties")) }

// through skips the wrapping a list or a map of objects adds, so that
// spec.apps.hosts means the hosts of an app.
func through(node *yaml.Node) *yaml.Node {
	for field(node, "properties") == nil {
		next := field(node, "items")
		if next == nil {
			next = field(node, "additionalProperties")
		}
		if next == nil || next.Kind != yaml.MappingNode {
			return node
		}
		node = next
	}
	return node
}

// field is the value of one key of a mapping node, or nil.
func field(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// keys are the field names of a properties mapping, which a mapping node
// holds as alternating keys and values.
func keys(properties *yaml.Node) []string {
	if properties == nil {
		return nil
	}
	var names []string
	for i := 0; i < len(properties.Content); i += 2 {
		names = append(names, properties.Content[i].Value)
	}
	return names
}
