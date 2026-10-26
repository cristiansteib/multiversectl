package app

import (
	"context"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

type idSystem struct{ System }

func (idSystem) UID() int { return 1000 }
func (idSystem) GID() int { return 1000 }

type recordingRuntime struct {
	Runtime
	services []string
	ran      string
}

func (r *recordingRuntime) Inspect(context.Context, RuntimeRequest) (ProjectConfig, error) {
	return ProjectConfig{Services: map[string]ServiceConfig{}}, nil
}

func (r *recordingRuntime) Exec(_ context.Context, _ RuntimeRequest, service string, _ []string) error {
	r.ran = service
	return nil
}

// With several services and none named, exec goes to the app's defaultService
// instead of failing; --service still wins.
func TestExecUsesTheAppsDefaultServiceUnlessOneIsNamed(t *testing.T) {
	st := domain.UniverseState{Name: "3", ID: "3", Slug: "3"}
	for _, tc := range []struct{ asked, want string }{{"", "web"}, {"db", "db"}} {
		rt := &recordingRuntime{}
		m := &domain.Manifest{
			Name: "acme", Repos: map[string]domain.Repo{"api": {}},
			Apps: map[string]domain.App{"api": {Repo: "api", DefaultService: "web"}},
		}
		svc := New(Deps{Store: &pruneStore{states: []domain.UniverseState{st}}, Runtime: rt, Files: everywhere{}, System: idSystem{}}, "/ws", m, "test")
		err := svc.Run(context.Background(), RunInput{Universe: "3", App: "api", Service: tc.asked, InRunning: true})
		if err != nil || rt.ran != tc.want {
			t.Errorf("asked %q: ran %q, err %v, want %q", tc.asked, rt.ran, err, tc.want)
		}
	}
}
