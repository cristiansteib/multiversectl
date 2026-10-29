package app

import (
	"context"
	"fmt"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// ForeignProject is a compose project whose containers were started from
// somewhere other than the universe that is about to act on it. Two multiverse
// roots can end up with the same project name; acting on it would recreate, or
// delete the volumes of, the other one's containers.
type ForeignProject struct {
	Project  string
	Universe string
	Dir      string // where the existing containers say they were started from
	Own      string // the directory this universe lives in
}

func (e *ForeignProject) Error() string {
	return fmt.Sprintf("compose project %s already has containers started from %s, which is not universe %s (%s): they belong to another multiverse root that uses the same project name, so they are left alone. Install this multiverse under a different name (`multiversectl start <source> --as <name>`) or remove the other one first",
		e.Project, e.Dir, e.Universe, e.Own)
}

// guardProject refuses to act on a compose project that has containers from
// another multiverse root. A container that carries no compose labels, or none
// that says where it was started, is given the benefit of the doubt: the check
// exists to stop two roots from trampling each other, not to second-guess
// containers it cannot attribute.
func (s *Service) guardProject(ctx context.Context, u domain.Universe, project string) error {
	containers, err := s.Runtime.Containers(ctx, project)
	if err != nil {
		return nil // listing is not what is being asked; the action reports its own failure
	}
	own := u.Dir()
	for _, c := range containers {
		if c.Project != project {
			continue
		}
		for _, where := range []string{c.WorkingDir, c.ConfigFile} {
			if where == "" {
				continue
			}
			// Compose files a manifest names itself sit beside the manifest,
			// outside the universe, and are shared by every one of its roots.
			if !within(where, own) && (s.Manifest.Dir == "" || !within(where, s.Manifest.Dir)) {
				return &ForeignProject{Project: project, Universe: u.Name, Dir: where, Own: own}
			}
			break
		}
	}
	return nil
}
