package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/cristiansteib/multiversectl/schema"
)

// collapsedKey holds the names of the fields a node was cut off at. It is not
// `properties`, because a `properties` holding a list of names would make the
// output an invalid schema while still looking like one — to a validator, an
// editor, or anything else reading the JSON.
const collapsedKey = "fields"

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
	cmd.Flags().StringVar(&kind, "kind", "multiverse", "which file's fields: "+strings.Join(schema.Kinds(), " or "))
	_ = cmd.RegisterFlagCompletionFunc("kind", cobra.FixedCompletions(schema.Kinds(), cobra.ShellCompDirectiveNoFileComp))
	cmd.ValidArgsFunction = func(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return schemaPaths(kind, toComplete), cobra.ShellCompDirectiveNoFileComp
	}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		root, err := schema.Root(kind)
		if err != nil {
			return err
		}
		path := ""
		if len(args) > 0 {
			path = args[0]
		}
		node, err := schema.At(root, path)
		if err != nil {
			return err
		}
		header := kind
		if path != "" {
			header += " " + path
		}
		// Only when something was left out: on a leaf, saying so would be a riddle.
		if prune(node, depth) {
			header += fmt.Sprintf(" — a name under `%s` is printed by its own path; --depth for the schema itself", collapsedKey)
		}
		return writeSchema(cmd, node, header)
	}
	return cmd
}

// schemaPaths completes a field path, one level at a time: without it the
// shell would offer the files of the current directory, which no path is.
func schemaPaths(kind, toComplete string) []string {
	root, err := schema.Root(kind)
	if err != nil {
		return nil
	}
	prefix, path := "", ""
	if i := strings.LastIndex(toComplete, "."); i >= 0 {
		prefix, path = toComplete[:i+1], toComplete[:i]
	}
	node, err := schema.At(root, path)
	if err != nil {
		return nil
	}
	var out []string
	for _, name := range schema.Fields(node) {
		out = append(out, prefix+name)
	}
	return out
}

// prune cuts a node to depth levels of nested fields: deeper ones are left as
// a list of their names under collapsedKey, which is all it takes to ask for
// one by path. A negative depth keeps everything, and only then is the output
// the schema itself. It says whether anything was left out, and edits the
// node, which is freshly parsed.
func prune(node *yaml.Node, depth int) bool {
	if node == nil || node.Kind != yaml.MappingNode {
		return false
	}
	cut := false
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, value := node.Content[i].Value, node.Content[i+1]
		switch key {
		case "items", "additionalProperties":
			// An item of a list, or a value of a map, is the same level.
			cut = prune(value, depth) || cut
		case "properties":
			if depth == 0 {
				node.Content[i].Value = collapsedKey
				*value = namesOf(value)
				cut = true
				continue
			}
			for j := 1; j < len(value.Content); j += 2 {
				cut = prune(value.Content[j], depth-1) || cut
			}
		}
	}
	return cut
}

// namesOf is a properties mapping as the list of its field names.
func namesOf(properties *yaml.Node) yaml.Node {
	list := yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
	for i := 0; i < len(properties.Content); i += 2 {
		list.Content = append(list.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: properties.Content[i].Value})
	}
	return list
}

// writeSchema prints the node: YAML, as the file is written, or JSON for
// anything that parses it.
func writeSchema(cmd *cobra.Command, node *yaml.Node, header string) error {
	out := cmd.OutOrStdout()
	if wantsJSON(cmd) {
		var value any
		if err := node.Decode(&value); err != nil {
			return err
		}
		return writeCompactJSON(out, value)
	}
	if _, err := fmt.Fprintf(out, "# %s\n", header); err != nil {
		return err
	}
	encoder := yaml.NewEncoder(out)
	encoder.SetIndent(2)
	if err := encoder.Encode(node); err != nil {
		return err
	}
	return encoder.Close()
}
