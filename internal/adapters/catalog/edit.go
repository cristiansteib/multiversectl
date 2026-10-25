package catalog

import (
	"bytes"
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/cristiansteib/multiversectl/internal/adapters/catalog/v1alpha1"
	"github.com/cristiansteib/multiversectl/internal/app"
	"github.com/cristiansteib/multiversectl/internal/domain"
)

// Editor edits catalog files through the YAML node tree, so a catalog someone
// has commented and arranged by hand comes back with that intact.
type Editor struct{}

var _ app.CatalogEditor = Editor{}

func (Editor) Scaffold(name string) []byte {
	return []byte("apiVersion: " + v1alpha1.APIVersion + "\nkind: " + v1alpha1.Kind +
		"\nmetadata:\n  name: " + name + "\nspec: {}\n")
}

func (Editor) AddCatalogRef(data []byte, ref domain.CatalogRef) ([]byte, error) {
	return addRef(data, "catalogs", ref.Name, ref.Source, DefaultCatalogPath)
}

func (Editor) AddWorkspaceRef(data []byte, ref domain.WorkspaceRef) ([]byte, error) {
	return addRef(data, "workspaces", ref.Name, ref.Source, DefaultWorkspacePath)
}

func (Editor) RemoveRef(data []byte, name string) ([]byte, error) {
	doc, spec, err := parse(data)
	if err != nil {
		return nil, err
	}
	removed := false
	for i := 0; i+1 < len(spec.Content); i += 2 {
		list := spec.Content[i+1]
		if list.Kind != yaml.SequenceNode {
			continue
		}
		kept := list.Content[:0]
		for _, item := range list.Content {
			if value(item, "name") == name {
				removed = true
				continue
			}
			kept = append(kept, item)
		}
		list.Content = kept
	}
	if !removed {
		return nil, fmt.Errorf("no entry named %q in this catalog", name)
	}
	return encode(doc)
}

// WorkspaceName only needs metadata.name, so it does not validate the rest:
// the manifest decoder owns that, and a catalog entry should not be refused
// because of a field this tool does not read.
func (Editor) WorkspaceName(data []byte) (string, error) {
	var w struct {
		Kind     string `yaml:"kind"`
		Metadata struct {
			Name string `yaml:"name"`
		} `yaml:"metadata"`
	}
	if err := yaml.Unmarshal(data, &w); err != nil {
		return "", fmt.Errorf("not valid YAML: %w", err)
	}
	if w.Kind != "Workspace" || w.Metadata.Name == "" {
		return "", errors.New("not a Workspace manifest with a metadata.name")
	}
	return w.Metadata.Name, nil
}

func addRef(data []byte, list, name string, src domain.Source, defaultPath string) ([]byte, error) {
	doc, spec, err := parse(data)
	if err != nil {
		return nil, err
	}
	for i := 0; i+1 < len(spec.Content); i += 2 {
		if seq := spec.Content[i+1]; seq.Kind == yaml.SequenceNode {
			for _, item := range seq.Content {
				if value(item, "name") == name {
					return nil, fmt.Errorf("%q is already in this catalog", name)
				}
			}
		}
	}
	seq := mapValue(spec, list)
	if seq == nil {
		spec.Content = append(spec.Content, scalar(list), &yaml.Node{Kind: yaml.SequenceNode})
		seq = spec.Content[len(spec.Content)-1]
	}
	// A scaffolded `[]` or `{}` is flow style; leaving it would write the
	// first entry inline.
	seq.Style = 0
	source := &yaml.Node{Kind: yaml.MappingNode}
	for _, kv := range [][2]string{{"repo", src.Repo}, {"ref", src.Ref}, {"path", pathUnlessDefault(src.Path, defaultPath)}} {
		if kv[1] != "" {
			source.Content = append(source.Content, scalar(kv[0]), scalar(kv[1]))
		}
	}
	seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		scalar("name"), scalar(name), scalar("source"), source,
	}})
	return encode(doc)
}

func pathUnlessDefault(p, def string) string {
	if p == def {
		return ""
	}
	return p
}

func parse(data []byte) (doc, spec *yaml.Node, err error) {
	doc = &yaml.Node{}
	if err := yaml.Unmarshal(data, doc); err != nil {
		return nil, nil, fmt.Errorf("catalog is not valid YAML: %w", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, nil, errors.New("catalog is not a YAML mapping")
	}
	spec = mapValue(doc.Content[0], "spec")
	if spec == nil {
		doc.Content[0].Content = append(doc.Content[0].Content, scalar("spec"), &yaml.Node{Kind: yaml.MappingNode})
		spec = doc.Content[0].Content[len(doc.Content[0].Content)-1]
	}
	if spec.Kind != yaml.MappingNode {
		spec.Kind, spec.Tag, spec.Value = yaml.MappingNode, "", ""
	}
	spec.Style = 0
	return doc, spec, nil
}

func mapValue(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func value(m *yaml.Node, key string) string {
	if m.Kind != yaml.MappingNode {
		return ""
	}
	if v := mapValue(m, key); v != nil {
		return v.Value
	}
	return ""
}

func scalar(s string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Value: s} }

func encode(doc *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), enc.Close()
}
