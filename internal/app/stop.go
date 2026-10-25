package app

import (
	"context"
	"fmt"
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
	apps, err := s.appsToAct(in.Apps)
	if err != nil {
		return err
	}

	u := s.Universe(st.Name)
	for _, appKey := range apps {
		if err := s.Runtime.Down(ctx, s.request(u, st, appKey, nil), false); err != nil {
			return fmt.Errorf("universe %s: %s: stop: %w", st.Name, appKey, err)
		}
		s.Out.OK("%s stopped", appKey)
	}
	return nil
}
