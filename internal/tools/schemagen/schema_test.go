package main

import (
	"os"
	"path/filepath"
	"testing"
)

// The committed schema must be what the types produce, so nobody has to
// remember to run `make schema`.
func TestCommittedSchemaIsCurrent(t *testing.T) {
	types := filepath.Join("..", "..", "adapters", "manifest", "v1alpha1", "types.go")
	structs, err := parse([]string{types})
	if err != nil {
		t.Fatal(err)
	}
	want, err := render(structs)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join("..", "..", "..", "schema", "workspace.v1alpha1.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Error("schema/workspace.v1alpha1.yaml is stale — run: make schema")
	}
}
