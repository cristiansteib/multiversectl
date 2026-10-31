package app

import (
	"context"
	"path/filepath"
	"time"

	"github.com/cristiansteib/multiversectl/internal/domain"
)

// Two processes, one multiverse
//
// The command line and the web view are separate programs, and an agent is a
// third. Nothing in one can see that another is halfway through creating the
// universe it is about to create, or is starting the containers it is about
// to remove. The server keeps its own operations apart, but only from each
// other.
//
// So the use cases that change something take a lock first, in the multiverse's
// own directory, and nothing that changes a universe runs twice at once:
//
//   - the multiverse lock, for the set of universes itself — creating one takes
//     the next free number and new branches, removing one gives them back;
//   - a universe's own lock, for working inside it, so different universes
//     still run side by side, which is the whole point of them.
//
// Both are taken in that order, never the other way, so two callers cannot
// each hold what the other is waiting for.
//
// Waiting is short on purpose. An operation can take minutes — `up` builds
// images — and sitting in silence behind one is worse than being told what is
// happening, so after a moment the answer is "something else is doing this".
const lockWait = 2 * time.Second

// lockProject guards the set of universes: their names, numbers and branches.
func (s *Service) lockProject(ctx context.Context) (Unlock, error) {
	return s.lock(ctx, "multiverse", "this multiverse")
}

// lockUniverse guards one universe, so another may be worked on at the same time.
func (s *Service) lockUniverse(ctx context.Context, id, name string) (Unlock, error) {
	return s.lock(ctx, "universe-"+id, "universe "+name)
}

// withUniverse is how every use case that changes one universe begins: resolve
// it, take its lock, and resolve it again — what it was before the wait is not
// what whoever held the lock left behind, and a universe can be removed while
// another command queues for it.
func (s *Service) withUniverse(ctx context.Context, name string, fn func(domain.UniverseState) error) error {
	st, err := s.Resolve(name)
	if err != nil {
		return err
	}
	unlock, err := s.lockUniverse(ctx, st.ID, st.Name)
	if err != nil {
		return err
	}
	defer unlock()
	if st, err = s.Resolve(name); err != nil {
		return err
	}
	return fn(st)
}

// lock takes one, and does nothing when no Locks port is wired — which is how
// the tests run, and is why the wiring in internal/wire always sets one.
func (s *Service) lock(ctx context.Context, name, what string) (Unlock, error) {
	if s.Locks == nil {
		return func() {}, nil
	}
	unlock, err := s.Locks.Acquire(ctx, filepath.Join(s.Multiverse, ".multiverse", "locks", name+".lock"), lockWait)
	if busy, ok := err.(*Busy); ok {
		busy.What = what
		return nil, busy
	}
	return unlock, err
}
