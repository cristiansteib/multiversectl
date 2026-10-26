package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

type detailRuntime struct {
	Runtime
	containers []Container
	logged     string
	tail       int
	asked      []string
	stats      []ContainerStats
}

func (d *detailRuntime) Containers(context.Context, string) ([]Container, error) {
	return d.containers, nil
}
func (d *detailRuntime) Logs(_ context.Context, name string, tail int, _ bool, _ io.Writer) error {
	d.logged, d.tail = name, tail
	return nil
}
func (d *detailRuntime) Stats(_ context.Context, names []string) ([]ContainerStats, error) {
	d.asked = names
	return d.stats, nil
}

func detailService(rt *detailRuntime) *Service {
	st := domain.UniverseState{Name: "3", ID: "3", Slug: "3", Project: "acme-u3"}
	m := &domain.Manifest{Name: "acme", Repos: map[string]domain.Repo{"api": {}}, Apps: map[string]domain.App{"api": {Repo: "api"}}}
	svc := New(Deps{Store: &pruneStore{states: []domain.UniverseState{st}}, Runtime: rt, Files: everywhere{}, Out: discardOut{}, System: idSystem{}}, "/ws", m, "test")
	return svc
}

// A page names a service, never a container: the name docker is asked about
// comes from the universe's own containers, so nothing else can be read.
func TestLogsOnlyReachContainersOfTheUniverse(t *testing.T) {
	svc := detailService(&detailRuntime{})
	project := svc.Universe("3").ProjectFor("api")
	rt := &detailRuntime{containers: []Container{
		{Name: project + "-web-1", Service: "web", Project: project, Status: "Up 1 hour"},
		{Name: "other-secret-1", Service: "secret", Project: "other", Status: "Up 1 hour"},
	}}
	svc = detailService(rt)

	if err := svc.Logs(context.Background(), LogsInput{Universe: "3", App: "api", Service: "web", Tail: 999999}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if rt.logged != project+"-web-1" || rt.tail != MaxLogTail {
		t.Errorf("logged %q tail %d, want the web container and a tail of %d", rt.logged, rt.tail, MaxLogTail)
	}

	err := svc.Logs(context.Background(), LogsInput{Universe: "3", App: "api", Service: "secret"}, &bytes.Buffer{})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("a service of another project: err = %v, want not found", err)
	}
}

// The stats view reads a whole project at once, and what it reads is filed
// under the universe and service it belongs to: a docker name means nothing on
// a page. A container of another project shares the machine, not the reading.
func TestProjectStatsAreFiledUnderTheirUniverseAndService(t *testing.T) {
	st3 := domain.UniverseState{Name: "3", ID: "3", Slug: "3", Project: "acme-u3"}
	st4 := domain.UniverseState{Name: "4", ID: "4", Slug: "4", Project: "acme-u4"}
	rt := &detailRuntime{containers: []Container{
		{Name: "acme-u3-api-web-1", Service: "web", Project: "acme-u3-api", Status: "Up 1 hour"},
		{Name: "acme-u3-api-db-1", Service: "db", Project: "acme-u3-api", Status: "Exited (0) 2 minutes ago"},
		{Name: "acme-u4-api-web-1", Service: "web", Project: "acme-u4-api", Status: "Up 3 minutes"},
		{Name: "other-u1-api-web-1", Service: "web", Project: "other-u1-api", Status: "Up 1 hour"},
	}, stats: []ContainerStats{
		{Name: "acme-u4-api-web-1", CPU: 2, MemBytes: 2 << 20},
		{Name: "acme-u3-api-web-1", CPU: 9, MemBytes: 1 << 20},
	}}
	m := &domain.Manifest{Name: "acme", Repos: map[string]domain.Repo{"api": {}}, Apps: map[string]domain.App{"api": {Repo: "api"}}}
	svc := New(Deps{Store: &pruneStore{states: []domain.UniverseState{st3, st4}}, Runtime: rt, Files: everywhere{}, Out: discardOut{}, System: idSystem{}}, "/ws", m, "test")

	rows, err := svc.ProjectStats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rt.asked) != 2 {
		t.Fatalf("asked for %v, want only the running containers of this project", rt.asked)
	}
	if len(rows) != 2 || rows[0].Universe != "3" || rows[0].App != "api" || rows[0].Service != "web" || rows[0].CPU != 9 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[1].Universe != "4" {
		t.Errorf("the second row is %+v, want universe 4", rows[1])
	}
}
