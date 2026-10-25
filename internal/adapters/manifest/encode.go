package manifest

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/cristiansteib/multiversectl/internal/adapters/manifest/v1alpha1"
	"github.com/cristiansteib/multiversectl/internal/app"
)

// Writer writes a new project's manifest. The result is text rather than a
// marshalled struct: the types carry no omitempty, and a starting point full of
// empty fields is not one to edit by hand.
type Writer struct{}

var _ app.ManifestWriter = Writer{}

func (Writer) Workspace(d app.ProjectDraft) ([]byte, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "apiVersion: %s\nkind: %s\nmetadata:\n  name: %s\nspec:\n", v1alpha1.APIVersion, v1alpha1.Kind, scalar(d.Name))
	b.WriteString("  # Hostnames are derived from the machine's address, so nothing has to be\n  # set up for a universe to be reachable.\n  domain: auto\n\n")
	b.WriteString("  # Each repository with a docker-compose.yml becomes an app of the same name;\n  # add an apps: section only to say more than that.\n  repos:\n")
	for _, r := range d.Repos {
		fmt.Fprintf(&b, "    - name: %s\n      url: %s\n", scalar(r.Key), scalar(r.URL))
		if r.Dir != "" && r.Dir != r.Key {
			fmt.Fprintf(&b, "      dir: %s\n", scalar(r.Dir))
		}
		if r.NoRuntime {
			b.WriteString("      runtime: none\n")
		}
	}
	return []byte(b.String()), nil
}

// scalar quotes a value only when YAML needs it.
func scalar(s string) string {
	out, err := yaml.Marshal(s)
	if err != nil {
		return fmt.Sprintf("%q", s)
	}
	return strings.TrimSpace(string(out))
}
