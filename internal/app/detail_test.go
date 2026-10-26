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
}

func (d *detailRuntime) Containers(context.Context, string) ([]Container, error) {
	return d.containers, nil
}
func (d *detailRuntime) Logs(_ context.Context, name string, tail int, _ bool, _ io.Writer) error {
	d.logged, d.tail = name, tail
	return nil
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
