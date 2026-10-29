package app

import (
	"context"
	"fmt"
	"time"
)

// CloneChecks runs the doctor's look at the shared clones alone.
func (s *Service) CloneChecks(ctx context.Context) []Check {
	var out []Check
	s.checkClones(ctx, func(name string, st Status, f string, a ...any) {
		out = append(out, Check{Name: name, Status: st, Detail: fmt.Sprintf(f, a...)})
	})
	return out
}

// PollFast makes the waits of `up` poll every millisecond.
func PollFast() (restore func()) {
	old := readyEvery
	readyEvery = time.Millisecond
	return func() { readyEvery = old }
}
