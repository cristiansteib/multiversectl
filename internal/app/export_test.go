package app

import (
	"context"
	"fmt"
)

// CloneChecks runs the doctor's look at the shared clones alone.
func (s *Service) CloneChecks(ctx context.Context) []Check {
	var out []Check
	s.checkClones(ctx, func(name string, st Status, f string, a ...any) {
		out = append(out, Check{Name: name, Status: st, Detail: fmt.Sprintf(f, a...)})
	})
	return out
}
