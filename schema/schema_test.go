package schema

import (
	"strings"
	"testing"
)

// The package hides the envelope the file is written in: a caller asks for a
// field, never for spec.versions[0].schema.openAPIV3Schema.
func TestRootIsWhereTheFieldsHang(t *testing.T) {
	for _, kind := range Kinds() {
		root, err := Root(kind)
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if got := Fields(root); len(got) < 3 || got[0] != "apiVersion" {
			t.Errorf("%s: fields are %v", kind, got)
		}
	}
	if _, err := Root("nope"); err == nil || !strings.Contains(err.Error(), "multiverse and catalog") {
		t.Errorf("an unknown kind should say which there are: %v", err)
	}
}

// A path names fields, not the shape they are written in: spec.apps is a list
// and hosts is another, and a caller should not have to know.
func TestAtStepsThroughListsAndMaps(t *testing.T) {
	root, err := Root("multiverse")
	if err != nil {
		t.Fatal(err)
	}
	node, err := At(root, "spec.apps.hosts.port")
	if err != nil {
		t.Fatal(err)
	}
	if got := Fields(node); got != nil {
		t.Errorf("a leaf has no fields: %v", got)
	}
	if _, err := At(root, "spec.apps.nope"); err == nil || !strings.Contains(err.Error(), "hosts") {
		t.Errorf("a wrong field should answer with the fields there are: %v", err)
	}
	// An empty segment is nothing to walk, so a trailing dot is the node itself.
	if node, err = At(root, "spec.apps."); err != nil || len(Fields(node)) == 0 {
		t.Errorf("At(spec.apps.) = %v", err)
	}
}
