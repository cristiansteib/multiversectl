package app

import (
	"context"
	"fmt"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// StopInput names the universe and the apps to stop (all runnable ones when
// empty).
type StopInput struct {
	Universe string
	Apps     []string
}

// Stop brings the containers of each app down but keeps their volumes.
func (s *Service) Stop(ctx context.Context, in StopInput) error {
	st, err := s.Resolve(in.Universe)
	if err != nil {
		return err
	}
	unlock, err := s.lockUniverse(ctx, st.ID, st.Name)
	if err != nil {
		return err
	}
	defer unlock()
	if st, err = s.Resolve(in.Universe); err != nil {
		return err
	}
	apps, err := s.appsToAct(in.Apps)
	if err != nil {
		return err
	}
	if len(in.Apps) == 0 {
		apps = s.withoutIdleOnDemand(ctx, st, apps)
	}

	u := s.Universe(st.Name)
	if st, err = s.fire(ctx, domain.PreDown, st, multiverseHooks, nil, nil); err != nil {
		return err
	}
	for _, appKey := range apps {
		if st, err = s.fire(ctx, domain.PreDown, st, appKey, nil, nil); err != nil {
			return err
		}
		if err := s.Runtime.Down(ctx, s.request(u, st, appKey, nil), false); err != nil {
			return fmt.Errorf("universe %s: %s: stop: %w", st.Name, appKey, err)
		}
		s.Out.OK("%s stopped", appKey)
		if st, err = s.fire(ctx, domain.PostDown, st, appKey, nil, nil); err != nil {
			return err
		}
	}
	_, err = s.fire(ctx, domain.PostDown, st, multiverseHooks, nil, nil)
	return err
}
