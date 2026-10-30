package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func runSchema(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root, _ := New(Factory{})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs(append([]string{"schema"}, args...))
	err := root.Execute()
	return out.String(), err
}

// The point of the command is that an agent can read one field without paying
// for the whole 130 KB, so the default must stay a sketch.
func TestSchemaNamesNestedFieldsInsteadOfPrintingThem(t *testing.T) {
	out, err := runSchema(t)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"properties: [root, expose, domain, branch, proxy, repos, apps, hooks, env]", "multiverse/v1alpha1"} {
		if !strings.Contains(out, want) {
			t.Errorf("the top-level view lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Port inside the container") {
		t.Error("a field three levels down was spelled out")
	}
	if len(out) > 2000 {
		t.Errorf("the top-level view is %d bytes; it is meant to be cheap to read", len(out))
	}
}

// A path reads like the field it names: spec.apps is a list and hosts is
// another, and neither should have to be spelled as such.
func TestSchemaPathStepsThroughLists(t *testing.T) {
	out, err := runSchema(t, "spec.apps.hosts.port")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "type: integer") || !strings.Contains(out, "Port inside the container") {
		t.Errorf("not the port field:\n%s", out)
	}
}

func TestSchemaUnknownFieldSaysWhatIsThere(t *testing.T) {
	_, err := runSchema(t, "spec.nope")
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{`no field "nope"`, "apps"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error lacks %q: %v", want, err)
		}
	}
}

func TestSchemaJSONIsOneObject(t *testing.T) {
	out, err := runSchema(t, "spec.branch", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var field struct {
		Type        string `json:"type"`
		Description string `json:"description"`
		Default     string `json:"default"`
	}
	if err := json.Unmarshal([]byte(out), &field); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if field.Type != "string" || field.Default != "u{{universe}}" {
		t.Errorf("got %+v", field)
	}
}

func TestSchemaCatalogIsTheOtherKind(t *testing.T) {
	out, err := runSchema(t, "--kind", "catalog")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `enum: ["Catalog"]`) {
		t.Errorf("not the catalog schema:\n%s", out)
	}
	if _, err := runSchema(t, "--kind", "nope"); err == nil {
		t.Error("an unknown kind is an error")
	}
}

// --depth -1 is the whole subtree, for a person reading or a file.
func TestSchemaDepthExpandsEverything(t *testing.T) {
	out, err := runSchema(t, "spec.repos", "--depth=-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Clone directory name") {
		t.Errorf("a nested field was still collapsed:\n%s", out)
	}
}
