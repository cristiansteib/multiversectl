package app

import (
	"context"
	"fmt"
	"io"
	"sort"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// ContainerRow is one container of a universe, filed under the app and service
// it belongs to.
type ContainerRow struct {
	App     string
	Service string
	Name    string
	Status  string
	Running bool
	Ports   string
}

// MaxLogTail bounds how much history one request can pull.
const MaxLogTail = 2000

// UniverseContainers lists the containers of every app of a universe, running or
// not: what is stopped is as much of an answer as what is up.
func (s *Service) UniverseContainers(ctx context.Context, universe string) ([]ContainerRow, error) {
	st, err := s.Resolve(universe)
	if err != nil {
		return nil, err
	}
	return s.containersOf(ctx, st)
}

func (s *Service) containersOf(ctx context.Context, st domain.UniverseState) ([]ContainerRow, error) {
	containers, err := s.Runtime.Containers(ctx, st.Project)
	if err != nil {
		return nil, err
	}
	u := s.Universe(st.Name)
	appOf := map[string]string{}
	for key := range s.Manifest.Apps {
		appOf[u.ProjectFor(key)] = key
	}
	rows := make([]ContainerRow, 0, len(containers))
	for _, c := range containers {
		key, ok := appOf[c.Project]
		if !ok {
			continue
		}
		rows = append(rows, ContainerRow{App: key, Service: c.Service, Name: c.Name, Status: c.Status, Running: isRunning(c), Ports: c.Ports})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].App != rows[j].App {
			return rows[i].App < rows[j].App
		}
		return rows[i].Service < rows[j].Service
	})
	return rows, nil
}

// LogsInput names a container by where it belongs, never by its docker name.
type LogsInput struct {
	Universe string
	App      string
	Service  string
	Tail     int
	Follow   bool
}

// Logs writes a service's log to w. The container is looked up among the
// universe's own, so a caller cannot read a container of anything else.
func (s *Service) Logs(ctx context.Context, in LogsInput, w io.Writer) error {
	st, err := s.Resolve(in.Universe)
	if err != nil {
		return err
	}
	rows, err := s.containersOf(ctx, st)
	if err != nil {
		return err
	}
	for _, r := range rows {
		if r.App == in.App && r.Service == in.Service {
			tail := in.Tail
			if tail <= 0 {
				tail = 200
			}
			tail = min(tail, MaxLogTail)
			return s.Runtime.Logs(ctx, r.Name, tail, in.Follow, w)
		}
	}
	return fmt.Errorf("universe %s has no container for %s/%s: %w", st.Name, in.App, in.Service, domain.ErrNotFound)
}

// Stats takes one reading of every running container of a universe.
func (s *Service) Stats(ctx context.Context, universe string) (map[string]ContainerStats, error) {
	st, err := s.Resolve(universe)
	if err != nil {
		return nil, err
	}
	rows, err := s.containersOf(ctx, st)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, r := range rows {
		if r.Running {
			names = append(names, r.Name)
		}
	}
	stats, err := s.Runtime.Stats(ctx, names)
	if err != nil {
		return nil, err
	}
	out := make(map[string]ContainerStats, len(stats))
	for _, c := range stats {
		out[c.Name] = c
	}
	return out, nil
}

// RestartInput is one service of one universe.
type RestartInput struct {
	Universe string
	App      string
	Service  string
}

// Restart restarts one service of a running universe in place. It does not
// recreate the container, so a changed image or compose file is not picked up:
// that is `up`.
func (s *Service) Restart(ctx context.Context, in RestartInput) error {
	st, err := s.Resolve(in.Universe)
	if err != nil {
		return err
	}
	if _, ok := s.Manifest.Apps[in.App]; !ok {
		return fmt.Errorf("app %q: %w in the manifest", in.App, domain.ErrNotFound)
	}
	rows, err := s.containersOf(ctx, st)
	if err != nil {
		return err
	}
	for _, r := range rows {
		if r.App == in.App && r.Service == in.Service {
			s.Out.Info("restarting %s/%s in universe %s", in.App, in.Service, st.Name)
			return s.Runtime.Restart(ctx, s.request(s.Universe(st.Name), st, in.App, nil), in.Service)
		}
	}
	return fmt.Errorf("universe %s has no container for %s/%s: %w", st.Name, in.App, in.Service, domain.ErrNotFound)
}
