package app

import "context"

// Env returns the variables an overlay, or a human shell, needs to address one
// universe.
func (s *Service) Env(ctx context.Context, universe string) (map[string]string, error) {
	st, err := s.Resolve(universe)
	if err != nil {
		return nil, err
	}
	return s.runtimeEnv(s.Universe(st.Name), st), nil
}
