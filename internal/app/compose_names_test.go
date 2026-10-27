package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/adapters/fsx"
	"github.com/cristiansteib/multiversectl/internal/domain"
)

// A repository may call its compose file what compose accepts. With more than
// one, compose's own preference decides, and the others are not stacked on it.
func TestAnAppRunsFromTheComposeFileTheRepositoryHas(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files []string
		want  string
	}{
		{"compose.yaml", []string{"compose.yaml"}, "compose.yaml"},
		{"docker-compose.yml", []string{"docker-compose.yml"}, "docker-compose.yml"},
		{"both: the preferred one", []string{"docker-compose.yml", "compose.yaml"}, "compose.yaml"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			m := &domain.Manifest{Name: "acme", Repos: map[string]domain.Repo{"api": {}}, Apps: map[string]domain.App{"api": {Repo: "api", Derived: true}}}
			u := domain.Universe{Name: "1", Multiverse: root, Manifest: m}
			for _, f := range tc.files {
				p := filepath.Join(u.RepoDir("api"), f)
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte("services: {}\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			svc := New(Deps{Files: fsx.New()}, root, m, "test")
			got, err := svc.composeFiles(u, "api")
			if err != nil {
				t.Fatal(err)
			}
			if want := filepath.Join(u.RepoDir("api"), tc.want); len(got) != 1 || got[0] != want {
				t.Errorf("files = %v, want just %s", got, want)
			}
		})
	}
}
