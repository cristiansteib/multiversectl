package main

import (
	"os"
	"path/filepath"
	"testing"
)

// The committed schemas must be what the types produce, so nobody has to
// remember to run `make schema`.
func TestCommittedSchemasAreCurrent(t *testing.T) {
	tests := []struct{ kind, types, file string }{
		{"Workspace", filepath.Join("manifest", "v1alpha1", "types.go"), "workspace.v1alpha1.yaml"},
		{"Catalog", filepath.Join("catalog", "v1alpha1", "types.go"), "catalog.v1alpha1.yaml"},
	}
	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			structs, err := parse([]string{filepath.Join("..", "..", "adapters", tt.types)})
			if err != nil {
				t.Fatal(err)
			}
			want, err := render(structs, targets[tt.kind])
			if err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(filepath.Join("..", "..", "..", "schema", tt.file))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(want) {
				t.Errorf("schema/%s is stale — run: make schema", tt.file)
			}
		})
	}
}
