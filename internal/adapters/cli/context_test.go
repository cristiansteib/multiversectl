package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/app"
)

func TestTheBriefViewKeepsWhatAnAgentNeedsAndDropsTheRest(t *testing.T) {
	ctx := app.ProjectContext{
		Name: "acme", Root: "/r", Manifest: "/r/m.yaml", UniversesDir: "/r/universes",
		Repos: []app.RepoContext{{Key: "api", Path: "/r/api", URL: "git@example.test:acme/api.git", Runtime: "compose"}},
	}
	list := []app.Summary{{
		Name: "3", Dir: "/r/universes/3", Running: 1,
		Branches:  map[string]string{"api": "u3"},
		Checkouts: []app.Checkout{{Repo: "api", Branch: "u3", Path: "/r/universes/3/api"}},
		Apps: []app.AppSummary{{
			Key: "api", Expected: 2,
			URLs: []app.HostURL{{Host: "api-3.example.test", URL: "http://api-3.example.test"}},
			Services: []app.ServiceStatus{
				{Name: "web", State: app.StateRunning},
				{Name: "db", State: app.StateNotUp},
			},
			Volumes: []string{"acme-u3-data"},
		}},
	}}

	b, err := json.Marshal(briefView(ctx, list))
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, want := range []string{`"/r/universes/3/api"`, `"http://api-3.example.test"`, `"state":"partial"`, `"up":"1/2"`, `"down":["api/db"]`, `"api":"/r/api"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
	// Nothing the agent can't act on: no volumes, no repository URLs, no times.
	for _, bad := range []string{"acme-u3-data", "git@example.test", "created", "lastStarted", "compose"} {
		if strings.Contains(got, bad) {
			t.Errorf("%q should not be in the brief view: %s", bad, got)
		}
	}
}

func TestHelpIsADocumentWithJSON(t *testing.T) {
	root, _ := New(Factory{})
	var out strings.Builder
	root.SetOut(&out)
	root.SetArgs([]string{"universe", "ls", "--help", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var d CommandDoc
	if err := json.Unmarshal([]byte(out.String()), &d); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	if d.Command != "multiversectl universe ls" || len(d.Examples) == 0 {
		t.Errorf("doc = %+v", d)
	}
	var hasOutput bool
	for _, f := range d.Flags {
		hasOutput = hasOutput || f.Name == "output"
	}
	if !hasOutput {
		t.Errorf("the -o flag is missing: %+v", d.Flags)
	}
}

func TestMessagesBecomeJSONLinesWithJSON(t *testing.T) {
	var buf strings.Builder
	o := NewOutput(&buf, false)
	o.events = true
	o.OK("universe %s ready", "3")
	o.Table([]string{"NAME", "UP"}, [][]string{{"3", "2/2"}})
	want := `{"level":"ok","message":"universe 3 ready"}` + "\n" + `{"level":"table","rows":[{"name":"3","up":"2/2"}]}` + "\n"
	if buf.String() != want {
		t.Errorf("got %q", buf.String())
	}
}
