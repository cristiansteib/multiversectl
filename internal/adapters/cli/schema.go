package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/cristiansteib/multiversectl/schema"
)

// schemaCmd prints the field reference the binary carries, so what a manifest
// takes can be read offline, by a person or by an agent, instead of being
// guessed at or looked up on the website. The whole Multiverse schema is some
// 130 KB: a path narrows it to one field, and only one level below it is
// spelled out, deeper fields being listed by name so the next path is obvious.
func (r runner) schemaCmd() *cobra.Command {
	var depth int
	var kind string
	cmd := &cobra.Command{
		Use:   "schema [field path]",
		Short: "Print what a multiverse.yaml (or catalog.yaml) takes, field by field",
		Long: "The fields of a manifest, each with its meaning, default and allowed values,\n" +
			"from the schema this binary was built with. A path is a field's dotted name,\n" +
			"through a list as if it were one item: `spec.apps.hosts.port`.",
		Example: "  multiversectl schema                        # the top-level fields\n" +
			"  multiversectl schema spec.apps              # one field, and what it takes\n" +
			"  multiversectl schema spec.apps.hosts --depth 2\n" +
			"  multiversectl schema --kind catalog",
		Args: cobra.MaximumNArgs(1),
	}
	cmd.Flags().IntVar(&depth, "depth", 1, "levels of nested fields to spell out; 0 only names them, -1 prints everything")
	cmd.Flags().StringVar(&kind, "kind", "multiverse", "which file's fields: multiverse or catalog")
	_ = cmd.RegisterFlagCompletionFunc("kind", cobra.FixedCompletions([]string{"multiverse", "catalog"}, cobra.ShellCompDirectiveNoFileComp))
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		root, err := schemaDoc(kind)
		if err != nil {
			return err
		}
		path := ""
		if len(args) > 0 {
			path = args[0]
		}
		node, err := schemaAt(root, path)
		if err != nil {
			return err
		}
		prune(node, depth)
		return writeSchema(cmd, node, kind, path, depth)
	}
	return cmd
}

// schemaDoc is the schema of one kind, down to the object the fields hang off:
// the file is written as a CustomResourceDefinition, which is a familiar shape
// to read but two levels of ceremony deeper than anybody asking for a field.
func schemaDoc(kind string) (*yaml.Node, error) {
	var text string
	switch kind {
	case "multiverse":
		text = schema.Multiverse
	case "catalog":
		text = schema.Catalog
	default:
		return nil, fmt.Errorf("--kind %q: want multiverse or catalog", kind)
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

// schemaAt walks a dotted path. A list or a map of objects is stepped through
// as if it were a single one, so a path reads like the field it names rather
// than like the schema's own shape.
func schemaAt(root *yaml.Node, path string) (*yaml.Node, error) {
	node := root
	for _, name := range strings.Split(path, ".") {
		if name == "" {
			continue
		}
		node = through(node)
		if next := field(field(node, "properties"), name); next != nil {
			node = next
			continue
		}
		if names := fieldNames(node); len(names) > 0 {
			return nil, fmt.Errorf("no field %q there; it has: %s", name, strings.Join(names, ", "))
		}
		return nil, fmt.Errorf("no field %q there; nothing is nested under it", name)
	}
	return node, nil
}

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

// prune cuts a node to depth levels of nested fields: deeper ones are left as
// a list of their names, which is all it takes to ask for one by path. A
// negative depth keeps everything. It edits the node, which is freshly parsed.
func prune(node *yaml.Node, depth int) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, value := node.Content[i].Value, node.Content[i+1]
		switch key {
		case "items", "additionalProperties":
			// An item of a list, or a value of a map, is the same level.
			prune(value, depth)
		case "properties":
			if depth == 0 {
				*value = namesOf(value)
				continue
			}
			for j := 1; j < len(value.Content); j += 2 {
				prune(value.Content[j], depth-1)
			}
		}
	}
}

// namesOf is a properties mapping as the list of its field names.
func namesOf(properties *yaml.Node) yaml.Node {
	list := yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
	for i := 0; i < len(properties.Content); i += 2 {
		list.Content = append(list.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: properties.Content[i].Value})
	}
	return list
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

// fieldNames are the fields nested under a node, for an error that says where
// the path went wrong.
func fieldNames(node *yaml.Node) []string {
	properties := field(through(node), "properties")
	if properties == nil {
		return nil
	}
	var names []string
	for i := 0; i < len(properties.Content); i += 2 {
		names = append(names, properties.Content[i].Value)
	}
	return names
}

// writeSchema prints the node: YAML, as the file is written, or JSON for
// anything that parses it.
func writeSchema(cmd *cobra.Command, node *yaml.Node, kind, path string, depth int) error {
	out := cmd.OutOrStdout()
	if wantsJSON(cmd) {
		var value any
		if err := node.Decode(&value); err != nil {
			return err
		}
		body, err := json.Marshal(value)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(out, "%s\n", body)
		return err
	}
	where := kind
	if path != "" {
		where += " " + path
	}
	if _, err := fmt.Fprintf(out, "# %s", where); err != nil {
		return err
	}
	if depth >= 0 {
		if _, err := fmt.Fprint(out, " — a field listed by name only is printed by its own path; --depth for more"); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(out); err != nil {
		return err
	}
	encoder := yaml.NewEncoder(out)
	encoder.SetIndent(2)
	if err := encoder.Encode(node); err != nil {
		return err
	}
	return encoder.Close()
}
